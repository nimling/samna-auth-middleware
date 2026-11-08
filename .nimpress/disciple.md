---
gate: samna-developer
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
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

if _, err := sdb.Init(e.Logger); err != nil {
	e.Logger.Fatal(err)
}
if err := notification.Init(ctx); err != nil {
	e.Logger.Fatal(err)
}
if err := sam.Init(ctx); err != nil {
	e.Logger.Fatal(err)
}
if err := uptime.Init(ctx, e.Logger, uptime.Options{}); err != nil {
	e.Logger.Fatal(err)
}
api := e.Group("/api", smw.UserAuthMiddleware(nil))
sam.RegisterEndpoints(api)

go func() {
	if err := e.Start(":" + port); err != nil && !errors.Is(err, http.ErrServerClosed) {
		e.Logger.Fatal(err)
	}
}()
<-ctx.Done()
shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := e.Shutdown(shutdown); err != nil {
	e.Logger.Errorf("shutdown: %v", err)
}
<-notification.Done()
<-sam.Done()
<-uptime.Done()
if err := sdb.Instance().Close(); err != nil {
	e.Logger.Errorf("close database: %v", err)
}
```

## Lifecycle

Every background runner of the middleware has the same two calls: `sam.Init` and `sam.PeerInit`, `notification.Init`, `analytics.Init` and `uptime.Init`, each joined by its package's `Done()`.

1. One context. `main` makes it with `signal.NotifyContext` and hands it to every `Init`. Cancelling it is the only way a runner ends; there is no `Stop`.
2. `Init(ctx) error` starts the goroutines and returns at once. It returns an error only when the runner cannot start: no database (`shared.ErrNotConnected`), the first `LISTEN` failing, or a second call while the runner still runs (`ErrStarted`). A host treats that error as fatal. After start, failures are logged and retried.
3. One database. `shared.Connect`, which `sdb.Init` calls once, creates the process's pool and records it; `sdb.Instance` on either role is `shared.Instance`, one accessor answering that `*shared.DB`, and the runners read it themselves. The disciple's `sdb.Init(logger, shared.Options{AssetSource, AssetCache})` sets the asset options on that same `*shared.DB`; there is no disciple wrapper type. No `Init` takes a database.
4. One `LISTEN` connection. `(*shared.DB).Listen(ctx, channel)` hands each subscriber its own channel of notifications on one `pq.Listener` per process, adding the channel on the first subscriber and dropping it on the last. `notification` listens on `claimius_notification`, and a prophet's held `/sam/sync/:cursor` pull on `claimius_sync` for as long as the request holds. The connection closes when the last subscriber's context ends. A `nil` notification means the connection was re-established and notifications may have been lost.
5. A clean end. On cancel `notification` and `analytics` stop taking work, write what is queued within 10 s and close their `Done()`; every open notification stream ends at once, so `e.Shutdown` is not held by it. A write after that is logged as dropped. `uptime` and `sam` return their workers and close `Done()`.
6. Order. The pool first, then the runners, then the routes, then serve. On the signal: shut the http server down, wait on every `Done()`, and close the database last. `(*shared.DB).Close` writes the usage counts still held before it closes the pool.
7. No hidden state. Before `Init`, `notification.Write` and `WriteUser` and `analytics.Post` log the drop, `notification.Stream`, `analytics.PostSession`, `GetConsent` and `PostConsent` return `ErrNotStarted`, and `Done()` logs it and answers a closed channel.
8. One replica folds. The hourly analytics fold and expiry run under `shared.LeadSync` with the lock key `claimius.analytics_fold`, so of N prophet replicas one folds.
9. Debug mode. `sam.Init`, `sam.PeerInit` and `uptime.Init` start nothing in debug mode and still return `nil`, and their `Done()` closes once the context ends.

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
4. Sign or issue a token. It verifies tokens against the prophet's JWKS. A secret written through the builder is encrypted on the prophet under the app's `private_seed` and syncs back as ciphertext. The disciple fetches its own app's seed from `GET /sam/app` at startup and holds it in memory, never in a table, file or log; `SamnaSecret.Resolve(ctx, db, userID)` reads the value through `get_secret_value` only for a user with read on the secret and opens it in process. No `/sam` route answers plaintext.
