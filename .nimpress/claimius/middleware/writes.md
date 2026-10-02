---
layout: doc
title: Writes in Go
description: The write builders NewInsert, NewUpdate and NewDelete, how a write to a shared type is routed to the prophet and upserted locally, how the actor is stamped, the feed position a routed write answers with, audit in the write's transaction, and debug mode.
tags: NewInsert, NewUpdate, NewDelete, Access, routed write, EndpointFor, Seq, WaitForSeq, WriteAudit, debug mode, sa_created_by
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

| Table | Deployed disciple | Prophet, and debug mode |
|---|---|---|
| `claim`, `claim_object`, `user_claim` | Sent to the prophet, the answer upserted locally | The local claimius functions |
| `organization`, `location`, `user_relation`, `samna_secret`, `samna_user` | Sent to the prophet, the answer upserted locally | Local, through the triggers |
| The implementing side's own registered tables | Local | Local |
| Claimius tables the prophet serves no route for, such as `notification` | Local | Local |

There is one builder, `shared`'s. Each role's `Init` sets where it sends a write once: the prophet's leaves `RouteToProphet` off, and the disciple's turns it on unless `SAUTH_DEBUG_USER_ID` or `SAUTH_DEBUG_CLIENTS` is set. The builder routes a model exactly when that is on and `shared.EndpointFor` knows its type, and otherwise runs the local executor, the claimius function for that type. No other code decides it.

The write helpers write through that builder only, so each does the same on both roles: `CreateClaim`, `Claim.AssignObject`, `Claim.AssignUser`, `CreateVirtualClaim` as those three, `InsertRootOrganization`, `UpdateMe` and `DeleteUser`. `IssueUserToken` and `UpsertUserExternal` insert their payload, `SamnaUserToken` or `SamnaUserExternal`: routed, it goes to `user/token` or `user/external` and the user the prophet answers with is upserted; local, it runs the token transaction. Each reads the existing token first to answer whether the user was created. `CreateOrganizationUser` runs `create_organization_user` directly and has no prophet route.

1. `.Access(ac)` stamps `sa_created_by` with `ac.UserClaimID` and `sa_owner_id` from the grant, pointer fields included. Nothing else stamps an actor.

2. A routed write resolves its path from the model with `shared.EndpointFor`, and an update or delete without an explicit `ID` takes the record id from the model, so `.Model(m).Access(ac)` reaches the right prophet path for every routed type. It sends the model to the prophet's `/sam` group over the app key, `POST` for an insert, `PATCH` for an update and `DELETE` for a delete, and upserts the row the prophet answers with inside the same call.

3. The disciple resolved the actor's grant with `MutationAccess` before it wrote. On the prophet the write runs for the calling app only: the app comes from the app key, a shared hierarchy row must be linked to it through `app_object`, and `smw.AppHome` refuses the write with `409` and the deciding prophet in `SAuth-Home` when another prophet decides the app, which the client returns as `client.ErrNotHome`. The prophet's triggers enforce the grant invariants against the stamped actor: a binding may not hand out more than the actor holds on the object, and a composition link needs read on the linked claim.

4. The prophet answers every successful write with the app's feed position in `SAuth-Seq`, written `<txid>.<seq>`. The builder keeps it, and `Seq()` returns it as a `client.SyncCursor`. A later read on the disciple that must see the write calls `sam.WaitForSeq(ctx, q.Seq(), timeout)`, which waits until the disciple's cursor reaches that position; see [Sync](../sync.md).

5. The write settles the actor's own pairs on the prophet and, through the local upsert and the triggers, on the disciple, so the handler reads its own write back. Every other user's pairs settle on that user's next check or listing; see [Materialization](../materialization.md).

## Audit in the write's transaction

`shared.WriteAudit(ctx, idb, appID, operation, type, objectType, objectID, ownerID, createdBy, message)` calls `claimius.write_audit` on `idb`, the transaction of the change it records, so the row commits with the change or not at all. The role wrappers pass it on: `sdb.Write(ctx, idb, appID, ...)` on the prophet, and `sdb.Write(ctx, idb, ...)` on the disciple, which mirrors one app and passes no app id. The pull loop writes its reconcile row this way, in the transaction of the snapshot it applies. See [Audit](../audit.md).

## Debug mode

`SAUTH_DEBUG_USER_ID` or `SAUTH_DEBUG_CLIENTS` turns a disciple into a self contained service: authentication is bypassed, the pull loop does not start, and every write runs through the local claimius functions. `SAUTH_DEBUG_PRIVATE_SEED` gives a debug disciple the app seed it encrypts local secrets with. It exists for local development and test runs, proves nothing about the prophet path, and is never set in a deployed environment.
