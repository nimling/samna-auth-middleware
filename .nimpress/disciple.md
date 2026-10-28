---
gate: samna-developer
layout: doc
title: Disciple
description: How a disciple writes through to the prophet, what it owns itself, and how the pull loop keeps its copy current.
tags: disciple, prophet, sync, write through, sync_event, access_diff, replay mode, retention, cold storage
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
3. Each role owns its database. `sdb.Init` on the role creates the process's pool through `shared.Connect` and holds it; `sdb.Instance()` answers it, the disciple's as `*sdb.DB`, which embeds `*shared.DB` with the asset `sdb.Options{AssetSource, AssetCache}` given to `sdb.Init`. `sam.Init` reads its role's `sdb.Instance()` and hands that `*shared.DB` to what it starts: `notification.Init(ctx, db)` and `analytics.Init(ctx, db)` are wiring inside the middleware, and no host calls them or passes a database.
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

The middleware holds the one retention mechanism of every Samna and Nimtech service. `claimius.retention` has one row per kind of runtime row, its `class`, its `days`, its `prune` function and, for a kept class, its `export` function. Application data has no kind and is never age pruned.

1. Classes, `claimius.retention_class`. `log`, `run`, `token`, `cache` and `queue` are deleted by age: logs, run history, delivery logs, caches, processed queue rows and expired tokens. `audit` and `record` are audit and legal records, exported to cold storage before they are deleted and never deleted otherwise.
2. The claimius kinds. On both roles `access_diff` (`log`), `audit` (`audit`) and `notification` (`log`, only a notification soft deleted or closed before the cut goes). On the prophet also `sync_event`, `peer_collision`, `analytics_event`, `analytics_session` and `analytics_heat` (`log`), `analytics_capture` (`cache`), `consent` (`record`), `rate_limit` and `par_request` (`token`).
3. Registering a kind. A host registers its own runtime tables from its own migration with `claimius.upsert_retention(kind, class, days, prune, export)`, the only way a kind enters the table. A kind already registered keeps the days it was given and takes the rest. Each function must exist with the arguments its class calls it with, or the call fails:
   1. A `log`, `run`, `token`, `cache` or `queue` kind passes `export` as `NULL` and a `prune(keep INTERVAL) RETURNS BIGINT` that deletes the rows older than `keep` and returns how many. A table partitioned by time drops whole partitions; any other table deletes in batches ordered by its key.
   2. An `audit` or `record` kind passes an `export(keep INTERVAL, after TEXT, lim INTEGER) RETURNS TABLE (key TEXT, data JSONB)` that answers at most `lim` expired rows with a key greater than `after`, an empty `after` the first batch, in key order, each row as `to_jsonb`, carrying `sa_root_id` when the row belongs to a root organization, and a `prune(keys TEXT[]) RETURNS BIGINT` that deletes exactly the rows of those keys. A key sorts as text in the order of the rows and carries no hyphen.
4. The job. `sam.Init` starts it on both roles under `shared.LeadSync` with `shared.RetentionLockKey`, in every mode, debug included, so of N replicas one runs it once a day. A deleted by age kind runs `claimius.delete_expired(kind)`, which refuses an `audit` or `record` kind with `22023`.
5. Export. For an `audit` or `record` kind the job reads a batch of 5,000 through `claimius.get_expired(kind, after, lim)` and groups it by root organization: a row's root is the `sa_root_id` of its `data`, which a host table carrying `sa_root_id` answers through `to_jsonb` and the `audit` export adds from the owner organization; a row with none has root `none`. Each root's rows are written as gzip NDJSON to `[export/]<app_slug>/<root_id>/<kind>/<yyyy>/<mm>/<first key>-<last key>.ndjson.gz` in that root's sink, and only then the job calls `claimius.delete_exported(kind, root_id, path, sha256, keys, first)`, which writes the manifest row in `claimius.retention_export` (kind, root, object path, first and last key, row count, sha256, exported_at) and runs the prune for exactly those keys, in the same order, in one transaction. A failed write deletes nothing and is logged. The `audit` prune deletes the exported rows and drops a monthly partition only once its month is past the retention and it holds no row, so every partition is exported before it is dropped.
6. The sink per root. `shared.ColdStorage`, one method `PutObject(ctx, path, body) error`, is a sink; `shared.ColdSink` pairs one with a path `Prefix`. The job resolves each root through `shared.ColdStore`, `GetColdSink(ctx, rootID uuid.UUID) (ColdSink, error)`, held as `(*shared.DB).ColdStore`, and writes rows with no root to the shared sink `(*shared.DB).ColdStorage`. `shared.Connect` wires an Azure Blob container when `SAUTH_COLD_STORAGE_CONNECTION_STRING` is set, into `SAUTH_COLD_STORAGE_CONTAINER_NAME`, as both the shared sink and `shared.SharedColdStorage`, the default store that answers the shared sink for every root. A host that stores per customer sets its own store on `sdb.Instance().ColdStore` before `sam.Init`, following the root's `cold_storage.mode` on Samna Auth: `shared` answers `ColdSink{Storage: db.ColdStorage}`; `export` answers the shared sink with `Prefix: shared.ColdExportPrefix`, `export/`, under which the customer collects its rows; `dedicated` answers a sink built from the root's `samna_secret` with `azure.GetColdStorage(connectionString, containerName)`. With no store the `audit` and `record` kinds are kept and the job logs that once per run; a root whose sink does not resolve keeps its rows, not deleted, and the job logs the root once per run of the kind.
7. Reading and editing. `sam.RegisterRetention(group)` mounts `GET /retention` with read and `PUT /retention/:kind` with write on the owner organization of the app; a row answers `class` and `export` beside the days.

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

A disciple flags data of its own, one field of one object, that Samna Auth may show on the public profile page of the user it belongs to. The prophet stores what the disciple sends in `claimius.profile_item`; it reads nothing of the disciple's tables and never syncs the rows back.

```go
ac, err := sdb.Instance().MutationAccess(ctx, userID, certificateID, "public.certificate", sdb.ACCESS_READ)
if err != nil {
	return err
}
item, err := sdb.Instance().PutProfileItem(ctx, ac, sdb.BodySamProfileItem{
	UserID: userID, ObjectType: "public.certificate", ObjectID: certificateID,
	Field: "name", Label: "Certificate", Value: certificate.Name, Link: &certificateURL,
})
```

1. `PutProfileItem(ctx, ac, item)` flags the field, or replaces the label, value, link and index of the live flag with the same app, user, object and field. `DeleteProfileItem(ctx, ac, key)` withdraws one field, or with no `field` every field of the object. `GetProfileItems(ctx, userID)` reads back the app's own flags for the user.
2. The `AccessClaim` is the flagged user's own, with READ on the object: a disciple flags only what the user it shows can read, and never for another user. Anything else is `ErrNoAccess`.
3. The shape is checked on both sides, `ErrBadProfileItem` and `400`: `field` is lower case letters, digits, `_` and `.`, starting with a letter, at most 64 characters; `label` 1 to 80 characters; `value` 1 to 500; `link`, when set, an `https://` url of at most 2048 characters; `object_type` at most 120 characters.
4. The user needs a live row in the app on the prophet, else `ErrNotFound` and `404`.
5. A disciple without a prophet answers `ErrNoProphet`: there is no public profile to show the item on.
6. Over the wire: `PUT /sam/profile` with the item, `DELETE /sam/profile` with `user_id`, `object_type`, `object_id` and an optional `field`, `GET /sam/profile?user_id=`, each answering the stored rows with `app_slug` and `app_name`. On the prophet, `claimius.upsert_profile_item`, `delete_profile_item` and `get_profile_items(user, app?)`.
7. Whether the page shows the items is the user's choice on Samna Auth: their profile must be public and show the `apps` field.

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
4. Sign or issue a token. It verifies tokens against the prophet's JWKS. A secret written through the builder is encrypted on the prophet under the `private_seed` of the app that wrote it and syncs back as ciphertext. The disciple fetches its own app's seed from `GET /sam/app` at startup and holds it in memory, never in a table, file or log; `SamnaSecret.Resolve(ctx, db, userID)` reads the value through `get_secret_value` only for a user with read on the secret and opens it in process. No `/sam` route answers plaintext.
