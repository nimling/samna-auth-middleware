---
gate: auth-middleware
layout: doc
title: Disciple
description: How a disciple writes through to the prophet, what it owns itself, and how the pull loop keeps its copy current.
tags: disciple, prophet, sync, write through, sync_event, access_diff, replay mode
lastUpdated: true
---

# Disciple

A disciple is a product service that mirrors one app from the prophet. It answers every access question from its own database, and it carries application tables the prophet has never seen.

## Wiring

```go
if _, err := sdb.Init(e.Logger); err != nil {
	e.Logger.Fatal(err)
}
api := e.Group("/api", smw.UserAuthMiddleware(nil))
sam.RegisterEndpoints(api)
sam.SyncStart(context.Background())
```

`SyncStart` returns immediately and owns its goroutines, backoff and logging, on every pod. In debug mode it does nothing.

`UserAuthMiddleware` also holds the answer of a request that made routed writes until the pull has applied them, so the client's next request reads them back. A route outside it gets the same with `smw.WaitForSeq(timeout)`.

`RegisterEndpoints` mounts `login`, `callback` and `logout` under the group. `login?next=<path>` returns the visitor to that same origin path after the callback. The path travels in the OAuth `state` as well as in a cookie, so a callback that reaches a browser without the flow cookies, a login link opened in another browser, starts the login again with the same `next` and still lands on it, a cli approval page included.

## Writes

The builder is the same in both modes. `sdb.Instance().NewInsert()`, `NewUpdate()` and `NewDelete()` take a model and the `AccessClaim` the actor holds.

```go
ac, err := sdb.Instance().MutationAccess(ctx, actor, orgID, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
if err != nil {
	return err
}
err = sdb.Instance().NewInsert().Model(&location).Access(ac).Scan(ctx, &location)
```

Where the write lands depends on the table.

| Table | Deployed | Debug |
|---|---|---|
| `claim`, `claim_object`, `user_claim` | Sent to the prophet, the answer is upserted locally | Local claimius functions |
| `organization`, `location`, `user_relation`, `samna_secret`, `samna_client`, `samna_user`, `object_asset` | Sent to the prophet, the answer is upserted locally | Local claimius functions |
| The service's own registered tables | Local, through bun | Local, through bun |
| Claimius tables the prophet serves no route for, such as `notification` | Local | Local |

The helpers on `sdb.Instance()`, `CreateClaim`, `AssignObject`, `AssignUser`, `CreateVirtualClaim`, `InsertRootOrganization`, `UpdateMe`, `DeleteUser`, `CreateOrganizationUser`, `PostUserFromToken` and `PostUserFromExternal`, write through the same builder, so they are routed exactly like a direct `NewInsert`.

A routed write stamps `sa_created_by` and `sa_owner_id` from the `AccessClaim`, posts the model to the prophet over the app key, and upserts the row the prophet answers with. The write is visible on the disciple as soon as the call returns, and every other disciple of the app receives it through sync.

The service's own tables never leave the disciple. The prophet stores the `claim_object` rows that bind a claim to them, addressed by `object_type` and `object_id`, and the disciple settles `user_object` for them locally.

## The pull loop

The disciple pulls. There is no inbound sync endpoint on a disciple and none on the prophet; the prophet serves `GET /sam/sync/snapshot` and `GET /sam/sync/:cursor` and nothing pushes. The cursor is a `client.SyncCursor`, a `(txid, seq)` position on the prophet's feed written `<txid>.<seq>`, kept in `disciple_state.last_applied_txid` and `last_applied_seq`.

1. One pod pulls. Every pod runs the loop, and only the one holding the session level advisory lock `SyncLockKey(app slug)` does sync work; the others try again every 5 s and take over when it goes.
2. On first start the cursor is zero, so it fetches `sync/snapshot`, upserts every source row that differs and records the snapshot's cursor, in one transaction.
3. It then fetches `sync/<cursor>` for the next page, which the prophet holds open up to 25 s while there is nothing, and applies the page under `replay_mode` in one transaction that moves the cursor first. A failure doubles the delay up to 30 s.
4. A `409` from the prophet, or ten failed pulls in a row, trigger a fresh snapshot and an audit row naming the stalled cursor.
5. Between pulls, at most once a second, it drains: it settles ahead the shares of users who read at least as often as areas are raised, and trims the areas every active cursor has passed.

The feed is scoped by the prophet: organizations and locations through `app_object_ids`, every other table through `app_id`. It carries source rows only, never `user_object`, `user_users`, `user_users_diff` or `audit`. The disciple strips `app_id` from each payload and skips `samna_app`.

A snapshot upserts; it never deletes a row the prophet did not send.

The disciple binds to the prophet that first answers it, keeps its slug and endpoint in `disciple_state.prophet` and `disciple_state.endpoint`, and routes its writes there. It moves to another prophet only when that prophet accepted a handover of the app from the bound one. See [Sync in Go](./claimius/middleware/sync.md).

## Settling access

The feed carries the claims, bindings and grants, and the disciple derives its own access from them. Applying a `claim`, `claim_object` or `user_claim` raises its claim area in `access_diff`, and applying an organization, a location or any registered row keeps the closure and raises its object area. A check settles the one pair it asks about; a listing settles the reader's own share of each open area it meets. An area is trimmed once every user active within 7 days has settled past it; a user who reads after that rebuilds its reach in full. A share nobody reads is never settled. This is the same settle for the shared types and for the tables only the disciple carries.

A local write settles the acting user inside the same statement, so a handler reads its own write back. Another user's `user_object` row changes on that user's next read. A request that made routed writes answers only once the disciple's cursor has reached the position they answered with, at most 10 s, so the client's next request sees them; a read inside the same request that must see one waits on `sdb.Instance().WaitForSeq` with that position. See [Sync in Go](./claimius/middleware/sync.md#read-your-writes).

## What the disciple never does

1. Select from a claimius table directly. Reads go through `get_*` and the `sdb` lookups.
2. Set `replay_mode`. Only the pull loop does.
3. Filter by `app_id`. The column does not exist on a disciple.
4. Sign, decrypt or issue a token. It verifies tokens against the prophet's JWKS.
