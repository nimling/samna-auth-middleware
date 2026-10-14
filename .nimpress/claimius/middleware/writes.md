---
gate: auth-middleware
layout: doc
title: Writes in Go
description: The write builders NewInsert, NewUpdate and NewDelete, how a write to a shared type is routed to the prophet and upserted locally, how the actor is stamped, the feed position a routed write answers with, audit in the write's transaction, and debug mode.
tags: NewInsert, NewUpdate, NewDelete, Access, Conn, routed write, EndpointFor, Seq, WaitForSeq, WriteAudit, debug mode, sa_created_by
lastUpdated: true
---

# Writes in Go

`sdb.Instance().NewInsert()`, `NewUpdate()` and `NewDelete()` take a model and the `AccessClaim` the actor holds.

```go
ac, err := sdb.Instance().MutationAccess(ctx, actor, orgID, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
if err != nil {
	return err
}
q := sdb.Instance().NewInsert().Model(&location).Access(ac)
err = q.Scan(ctx, &location)
```

## Where a write lands

| Table | Deployed disciple | Disciple in debug mode |
|---|---|---|
| `claim`, `claim_object`, `user_claim` | Sent to the prophet, the answer upserted locally | The local claimius functions |
| `organization`, `location`, `user_relation`, `samna_secret`, `samna_user` | Sent to the prophet, the answer upserted locally | Local, through the triggers |
| The implementing side's own registered tables | Local | Local |
| Claimius tables the prophet serves no route for, such as `notification` | Local | Local |

There is one builder, `shared`'s. The disciple's `Init` turns `RouteToProphet` on unless `SAUTH_DEBUG_USER_ID` or `SAUTH_DEBUG_CLIENTS` is set. The builder routes a model exactly when that is on and `shared.EndpointFor` knows its type, and otherwise runs the local executor, the function for that type in the disciple set. No other code decides it. The prophet's writes are its `/sam` handlers, which call the prophet set's functions with the calling app.

The write helpers write through that builder only: `CreateClaim`, `Claim.AssignObject`, `Claim.AssignUser`, `CreateVirtualClaim` as those three, `InsertRootOrganization`, `UpdateMe`, `DeleteUser`, `CreateOrganizationUser`, `IssueUserToken` and `UpsertUserExternal`.

The last three are one operation, `insert_organization_user`: the user, the grant of the claim it is created with, its virtual claim bound to it and the actor's grant of that, and with an identity the `user_token` naming it. `CreateOrganizationUser` inserts a `SamnaUser` carrying `OwnerID` and `ClaimID`, routed to `user`. `IssueUserToken` and `UpsertUserExternal` insert their payload, `SamnaUserToken` or `SamnaUserExternal`, routed to `user/token` or `user/external`. Locally each runs the disciple set's function; on the prophet each endpoint runs the prophet set's with the calling app and the payload's `created_by` as the actor. `Created()` on the insert answers whether the user was new: routed, from the prophet's `201`; local, from the function. A `SamnaUser` without `OwnerID` is an app user with no organization and runs `upsert_app_user`.

1. `.Access(ac)` stamps `sa_created_by` with `ac.UserClaimID` and `sa_owner_id` from the grant, pointer fields included. Nothing else stamps an actor.

2. A routed write resolves its path from the model with `shared.EndpointFor`, and an update or delete without an explicit `ID` takes the record from the model: `id`, `user_id` on a `samna_user` or `user_claim`, `object_id` on a `claim_object`. A `Where` expression never names the record; a routed update or delete whose model carries none is refused with `ErrRoutedNoRecord`, and a routed update carrying a `Set` expression is refused with `ErrRoutedSet`, because the prophet takes fields, not SQL. Set the field on the model, and soft delete with `NewDelete`. It sends the model to the prophet's `/sam` group over the app key, `POST` for an insert, `PATCH` for an update and `DELETE` for a delete, and upserts the row the prophet answers with inside the same call.

3. The disciple resolved the actor's grant with `MutationAccess` before it wrote. On the prophet the write runs for the calling app only: the app comes from the app key, a shared hierarchy row must be linked to it through `app_object`, and `smw.AppHome` refuses the write with `409` and the deciding prophet in `SAuth-Home` when another prophet decides the app, which the client returns as `client.ErrNotHome`. The prophet's triggers enforce the grant invariants against the stamped actor: a binding may not hand out more than the actor holds on the object, or, on a row of the disciple's own table that the prophet does not hold, on the binding's owning organization, and a composition link needs read on the linked claim. A consumer binds a claim on its own row with `NewInsert().Model(&ClaimObject{...}).Access(ac)`, never with a local `claimius.insert_claim_object`, which lands on the disciple alone: the prophet, its feed and every other disciple of the app never hold it.

4. The prophet answers every successful write with the app's feed position in `SAuth-Seq`, written `<txid>.<seq>`. The builder keeps it, and `Seq()` returns it as a `client.SyncCursor`. A later read on the disciple that must see the write calls `sam.WaitForSeq(ctx, q.Seq(), timeout)`, which waits until the disciple's cursor reaches that position; see [Sync](../sync.md).

5. The write settles the actor's own pairs on the prophet and, through the local upsert and the triggers, on the disciple, so the handler reads its own write back. Every other user's pairs settle on that user's next check or listing; see [Materialization](../materialization.md).

## Inside a caller's transaction

`.Conn(tx)` on `NewInsert()`, `NewUpdate()` or `NewDelete()` names the connection the builder writes on locally, bun's `bun.IConn` convention, a `bun.Tx` in practice. Every local statement of the write runs on it: the bun query, the claimius function a typed write calls, the statements a write such as `insertOrganization` would otherwise group in a transaction of its own, and the upsert of a routed write's answer. A binding on a row the same transaction inserted therefore sees that row, and the write commits or rolls back with the caller.

```go
err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
	if _, err := tx.NewInsert().Model(&booking).Exec(ctx); err != nil {
		return err
	}
	return db.NewInsert().Model(&sdb.ClaimObject{ClaimID: claimID, ObjectID: &booking.ID, ObjectType: &bookingType}).Access(ac).Conn(tx).Scan(ctx)
})
```

A routed write still goes to the prophet over HTTP, which judges a binding on a consumer's own row by the actor's hold on the binding's owning organization and needs no sight of the row; only the local copy of its answer joins the transaction. Without `.Conn` every statement runs on the pool, outside any transaction of the caller's.

## Audit in the write's transaction

`shared.WriteAudit(ctx, idb, appID, operation, type, objectType, objectID, ownerID, createdBy, message)` calls `claimius.insert_audit` on `idb`, the transaction of the change it records, so the row commits with the change or not at all. The role wrappers pass it on: `sdb.Write(ctx, idb, appID, ...)` on the prophet, and `sdb.Write(ctx, idb, ...)` on the disciple, which mirrors one app and passes no app id. The pull loop writes its reconcile row this way, in the transaction of the snapshot it applies. See [Audit](../audit.md).

## Debug mode

`SAUTH_DEBUG_USER_ID` or `SAUTH_DEBUG_CLIENTS` turns a disciple into a self contained service: authentication is bypassed, the pull loop does not start, and every write runs through the local claimius functions. `SAUTH_DEBUG_PRIVATE_SEED` gives a debug disciple the app seed it encrypts local secrets with. It exists for local development and test runs, proves nothing about the prophet path, and is never set in a deployed environment.
