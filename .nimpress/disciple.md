---
gate: samna-developer
layout: doc
title: Disciple
description: How a disciple writes through to the prophet, what it owns itself, and how the pull loop keeps its copy current.
tags: disciple, prophet, sync, write through, sync_event, access_diff, replay mode, retention
lastUpdated: true
---

# Disciple

A disciple is a product service that mirrors one app from the prophet. It answers every access question from its own database, and it carries application tables the prophet has never seen.

## Wiring

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
runCtx, cancelRun := context.WithCancel(context.Background())
defer cancelRun()

if _, err := sdb.Init(e.Logger); err != nil {
	e.Logger.Fatal(err)
}
if err := sam.Init(runCtx); err != nil {
	e.Logger.Fatal(err)
}
if err := uptime.Init(runCtx, e.Logger, uptime.Options{}); err != nil {
	e.Logger.Fatal(err)
}
api := e.Group("/api", smw.UserAuthMiddleware(nil))
sam.RegisterEndpoints(api)
e.Server.RegisterOnShutdown(notification.EndStreams)

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
cancelRun()
<-sam.Done()
<-uptime.Done()
if err := sdb.Instance().Close(); err != nil {
	e.Logger.Errorf("close database: %v", err)
}
```

## Lifecycle

Each role has one runtime start, `sam.Init(ctx) error`, joined by `sam.Done()`. The disciple's `disciple/sam.Init` starts the sync, the app seed, the Initiate apps, notification and the retention job; the prophet's `prophet/sam.Init` starts notification, analytics, the retention job and the peer sync. `uptime.Init(ctx, logger, opts)` is the one other host call, joined by `uptime.Done()`, and takes no database.

1. One run context. `main` makes the signal context with `signal.NotifyContext` and a run context it cancels after `e.Shutdown` returns, and hands the run context to `sam.Init` and `uptime.Init`. Cancelling it is the only way a runner ends; there is no `Stop`.
2. `Init(ctx) error` starts the goroutines and returns at once. It returns an error only when the runtime cannot start: no database (`shared.ErrNotConnected`), the first `LISTEN` failing, or a second call while it still runs (`ErrStarted`). A host treats that error as fatal. After start, failures are logged and retried.
3. Each role owns its database. `sdb.Init` on the role creates the process's pool through `shared.Connect` and holds it; `sdb.Instance()` answers it, the disciple's as `*sdb.DB`, which embeds `*shared.DB`. The asset callbacks are `disciple/echo` `Options{AssetSource, AssetCache}`, given to `PutOptions`. `sam.Init` reads its role's `sdb.Instance()` and hands that `*shared.DB` to what it starts: `notification.Init(ctx, db)` and `analytics.Init(ctx, db)` are wiring inside the middleware, and no host calls them or passes a database.
4. One `LISTEN` connection. `(*shared.DB).Listen(ctx, channel)` hands each subscriber its own channel of notifications on one `pq.Listener` per process, adding the channel on the first subscriber and dropping it on the last. `notification` listens on `claimius_notification`, and a prophet's held `/sam/sync/:cursor` pull on `claimius_sync` for as long as the request holds. The connection closes when the last subscriber's context ends. A `nil` notification means the connection was re-established and notifications may have been lost.
5. A clean end. On cancel `notification` and `analytics` stop taking work, write what is queued within 10 s and end; a write after that is logged as dropped. The sync and peer workers return. `sam.Done()` closes once everything its `Init` started has ended, and `uptime.Done()` once the reporter has.
6. Long lived handlers end with the server, not the runner. `notification.EndStreams` ends every open notification stream, and on a prophet `prophetecho.EndStreams` (`prophet/echo`) answers every held `/sam/sync/:cursor` pull with what it has. A host registers each one it mounts with `e.Server.RegisterOnShutdown(notification.EndStreams)` before it serves, so `e.Shutdown` ends them as it begins instead of waiting for its deadline; the runtime keeps writing until the run context ends.
7. Order. The pool first, then `sam.Init` and `uptime.Init`, then the routes and their `RegisterOnShutdown`, then serve. On the signal: shut the http server down, cancel the run context, wait on `sam.Done()` and `uptime.Done()`, and close the database last. `(*shared.DB).Close` writes the usage counts still held before it closes the pool.
8. No hidden state. Before `sam.Init`, `notification.Write` and `WriteUser` and `analytics.Post` log the drop, `notification.Stream`, `analytics.PostSession`, `GetConsent` and `PostConsent` return `ErrNotStarted`, and `Done()` logs it and answers a closed channel.
9. One replica folds. The hourly analytics fold and expiry run under `shared.LeadSync` with the lock key `claimius.analytics_fold`, so of N prophet replicas one folds.
10. Debug mode. `sam.Init` starts notification, the retention job, and on the prophet analytics, in debug mode too; the sync, seed, Initiate apps and peer sync start only outside it. `sam.Done()` closes once the context ends. `uptime.Init` starts nothing in debug mode, returns `nil` and its `Done()` is closed at once.

`UserAuthMiddleware` also holds the answer of a request that made routed writes until the pull has applied them, so the client's next request reads them back. A route outside it gets the same with `smw.WaitForSeq(timeout)`.

`RegisterEndpoints` mounts `login`, `callback` and `logout` under the group. `login?next=<path>` returns the visitor to that same origin path after the callback. The path travels in the OAuth `state` as well as in a cookie, so a callback that reaches a browser without the flow cookies, a login link opened in another browser, starts the login again with the same `next` and still lands on it, a cli approval page included.

## Retention

Retention is a setting. Each runtime table has its days and its kind has a size, and the job on both roles deletes, once an hour and only from a table over the size limit, every row whose time column is older than its days. Application data is never aged: no user, organization, location, app, client, secret, claim, grant or binding has a lifetime, and the derived tables are rebuilt, not aged.

1. The settings. One JSON object, `SAUTH_RETENTION` in the service's `.env` as the default, overridden by the same object in the app's settings through `sdb.Instance().Retention`, a function the host sets that returns `shared.Retention`. Every runtime table belongs to a kind. Days are set per table, `<table>_days`, or per kind, `<kind>_days`; the size a table has to reach is set per kind, `<kind>_size_mb`, or for all, `size_mb`. The defaults are lenient: `audit` 730 days, `analytics` 365, `notification` 365, `token` 7, `log` 180, and 4096 MB; 0 days leaves a table alone.
2. The groups. `audit`: `audit` by `sa_created_at`. `analytics`, prophet only: `analytics_event`, `analytics_session`, `analytics_capture`, `analytics_heat`. `notification`: `notification` by `closed_at` and `sa_deleted_at`. `token`: `dpop_jti` on both roles, and on the prophet `session`, `jwks_cache`, `par_request`, `rate_limit` and `code` by `ends_at` and `revoked_at`. `log`, prophet only: `consent` by `revoked_at` and `peer_collision`.
3. The job. `claimius.prune(p_settings JSONB)` holds the whole rule in SQL: the groups, the tables, their time columns and the size check. `sam.Init` calls it once an hour on both roles under `shared.LeadSync` with `shared.RetentionLockKey`, so of N replicas one runs it.
4. A host's own runtime rows. A service defines `public.prune(p_settings JSONB) RETURNS BIGINT` of the same shape in its own base SQL, over its own logs, runs, delivery rows, caches and expired tokens, reading the same settings; `claimius.prune` calls it when it exists. Nothing is registered, mounted or synced.
5. `sync_event` and `access_diff` are not retention: they are bounded by cursors and trimmed by `trim_sync_event` and `trim_access_diff` once every reader has passed, whatever their age.

### Cost

```
entries(a run) = one size call and one delete per table and time column of the list, notification and code twice; a range where an index leads on the column, a scan of the table elsewhere
```

Only `session`, `dpop_jti`, `par_request`, `jwks_cache`, `rate_limit` and `audit` have an index leading on their time column. The job is one advisory lock an hour per database, taken by one replica.

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

## Public profile items

A disciple writes data of its own into the public profile of the user it belongs to: one entry per field, `{"schema": {...}, "value": ...}`, the schema telling the renderer what the value is and how to show it, stored by the prophet under `<app slug>.<field>` in `samna_user.profile` on every row of the user, and carried back by the sync. A disciple reads the profile from its own `samna_user` row; nothing is fetched.

```go
ac, err := sdb.Instance().MutationAccess(ctx, userID, certificateID, "public.certificate", sdb.ACCESS_READ)
if err != nil {
	return err
}
profile, err := sam.Disciple().PutProfileItem(ctx, ac, shared.BodySamProfileItem{
	UserID: userID, Field: "certificate",
	Schema: json.RawMessage(`{"type": "string", "title": "Certificate", "format": "uri", "component": "link"}`),
	Value:  json.RawMessage(`"https://example.no/certificates/` + certificateID.String() + `"`),
})
```

1. `sam.Disciple().PutProfileItem(ctx, ac, item)` writes the entry, or replaces the entry of the same field, and answers the whole profile. `DeleteProfileItem(ctx, ac, key)` withdraws one field, or every entry of the app when `Field` is nil, and answers the profile.
2. The `AccessClaim` is the user's own, with READ: a disciple writes only for the user it shows, never for another user. Anything else is `ErrNoAccess`.
3. The shape is checked on both sides, `ErrBadProfileItem` and `400`: `field` is lower case letters, digits, `_` and `.`, starting with a letter; `schema` an object with a `type`; `value` JSON. `shared.ProfileSchema` is a typed struct: a key outside its vocabulary is refused at decode, `Validate` checks the type, format, component, pattern, items, properties and default, `Check` the value against it, on both roles.
4. The user needs a live row in the app on the prophet, else `ErrNotFound` and `404`.
5. A disciple without a prophet answers `sam.ErrNoProphet`. The bodies, their `Validate` and `ErrBadProfileItem` are `shared`, where the prophet checks with them too.
6. Over the wire: `PUT /sam/profile` with `user_id`, `field`, `schema` and `value`, `DELETE /sam/profile` with `user_id` and an optional `field`, each answering the profile.
7. Whether the page shows an app's entries is the user's choice on Samna Auth.

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
