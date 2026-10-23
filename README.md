# Samna Auth Middleware

Go libraries for integrating with the Samna Auth service. The module ships two roles that share one symmetric surface: a prophet, the source of truth that serves the sync feed and the full admin API, and a disciple, a local replica that pulls from the prophet and consumes tokens.

Every consuming service runs a local PostgreSQL database with the `claimius` schema. The prophet holds every app; a disciple mirrors exactly one. This local copy powers middleware decisions, graph queries, and claim resolution without round trips to the auth server.

## Installation

```
go get github.com/nimling/samna-auth-middleware/v2
```

## The two roles

| Role | Holds | Sync | API |
|------|-------|------|-----|
| Prophet | Full app row with key and seed material | Serves the snapshot and delta feed | `sam.RegisterEndpoints` mounts the full admin API |
| Disciple | App identity from the environment | Pulls in the background via `sam.Init`, one pod per database | `sam.RegisterEndpoints` mounts the me and login routes |
| Hybrid | A prophet set up with `claimius.upsert_prophet_peer` | Serves its own apps, and pulls one app of a peer prophet through the prophet `sam.Init` | As the prophet; writes of the mirrored app are refused and belong to the peer |

A binary is one role. Both roles wire the same four identifiers. Each app has one home prophet that decides its writes, and a disciple relies on exactly one prophet, the one it is bound to.

## Packages

| Package | Prophet import | Disciple import | Role |
|---------|----------------|-----------------|------|
| `sdb` | `prophet/sdb` | `disciple/sdb` | Database layer. `sdb.Init(logger)` connects the pool and loads config once. |
| `echo` | `prophet/echo` | `disciple/echo` | The individual HTTP handlers. |
| `sam` | `prophet/sam` | `disciple/sam` | `sam.RegisterEndpoints(group)`; `sam.Init(ctx)`, the one runtime start of the role: sync and notification on the disciple, notification, analytics and the peer sync on the prophet. |
| `smw` | `smw` | `smw` | Middleware, shared by both roles. |
| `client` | `client` | `client` | HTTP client for the Samna Auth API. |
| `utils` | `utils` | `utils` | Config loader, cookies, and debug helpers. |
| `database` | `prophet/database` | `disciple/database` | The `claimius` base SQL set for the role, read by a consumer as a smig git include. |
| `uptime` | | `uptime` | The heartbeat reporter. `uptime.Init(ctx, logger, opts)` registers the app's `heartbeat` monitor and beats to the prophet. |
| `notification` | `notification` | `notification` | In app notifications: `Write`, `WriteUser` and the SSE `Stream` behind the notification routes. Started by `sam.Init` on either role. |
| `analytics` | `analytics` | | Consent, sessions, events and the hourly fold on the prophet. Started by the prophet `sam.Init`. |

## Getting started

Follow the walkthrough for your role:

- [Prophet](docs/prophet.md)
- [Disciple](docs/disciple.md)

Then the cross cutting topics:

- [Sync](docs/sync.md), the disciple pull loop
- [App scoping](docs/app_scoping.md), `app_id` columns and `app_object` links
- [Machine clients](docs/clients.md), service to service calls with client credentials
- [Marshalling](docs/marshalling.md), the JSON shape over the wire

Runnable examples live under [example/prophet](example/prophet) and [example/disciple](example/disciple). Each has a `.env.example`; copy it to `.env`, fill it in, and run `go run main.go`.

## Middleware

The `smw` package validates requests against the Samna Auth service following OAuth 2.0 and OIDC. Its entrypoints:

- `smw.AppMiddleware` is internal. It guards the endpoints apps use to reach the prophet, authenticating the app slug and secret and setting the app context. On that group `smw.SyncSeq` answers every successful write with the app's feed position in `SAuth-Seq`, and `smw.AppHome` refuses a write for an app another prophet decides with `409` and that prophet in `SAuth-Home`.
- `smw.UserAuthMiddleware(opts)` authenticates browser and bearer users. It checks a bearer token, then a session cookie, then client credentials, and reads the resolved OIDC endpoints from the config that `sdb.Init` loaded. Pass `nil` to take the slug, secret, redirect, and page redirect from the config. Once the actor is known it stamps the request context with the actor's claimius `sa_root_id` and `sa_owner_id` through `nimtel.Organization`, so every span and every log line written with that context carries `samna.sa_root_id` and `samna.sa_owner_id`. A service never calls the stamp itself.
- `smw.TelemetryMiddleware()` is the OpenTelemetry server span middleware named after the app slug. Mount it once on the echo instance before the auth middleware; the service starts the SDK with `nimtel.Init` at boot and the platform provides the collector through the standard `OTEL_EXPORTER_OTLP_*` variables.
- `smw.ClientAuthMiddleware()` authenticates service to service callers by their client credentials.

Client credentials arrive as HTTP basic auth or as `client_id` and `client_secret` form values, and the client id becomes the actor, so a machine is authorized by the same claims a human is. [Machine clients](docs/clients.md) covers creating one, granting it a claim, and calling with it.

After a successful pass the user id and access token sit in the context:

```go
userID := ctx.Get(smw.UserIDKey).(uuid.UUID)
accessToken := ctx.Get(smw.AccessTokenKey).(string)
```

## Uptime

The prophet probes every app from outside on its `monitor` rows. The `uptime` package is the inside half: a disciple imports it, calls it once at boot, and from then on the app reports its own health to the prophet on the app key, with the checks it chose to run. Its entrypoints:

- `uptime.Init(ctx, logger, opts) error` reads the app slug, secret and endpoint from the same config the `client` package uses, registers one monitor named `heartbeat` of kind `heartbeat` for the app if none exists, and starts a goroutine that runs the checks and posts a beat every `opts.Interval`, default sixty seconds. A registration or beat that fails is logged and retried on the next tick; nothing here is fatal, and `Init` returns an error only on a second call while it runs. Debug mode disables it the way it disables the sync pull loop.
- `uptime.Options` carries `Interval`, `Version`, the string reported with every beat, and `Checks`, a list of `uptime.Check{Name, Run}` the app supplies. A check is any `func(ctx context.Context) error`, a database ping being the usual one. A check that returns an error fails the beat and carries its message.
- Cancelling `ctx` ends the goroutine and closes `uptime.Done()`. `uptime.GetHeartbeat()` returns the app's own last beat, its checks, its version and whether the prophet accepted it.

```go
if err := uptime.Init(ctx, e.Logger, uptime.Options{
	Version: version,
	Checks: []uptime.Check{
		{Name: "database", Run: sdb.Instance().PingContext},
	},
}); err != nil {
	e.Logger.Fatal(err)
}
```

After the http server has shut down, `<-uptime.Done()`. The order every runner follows is [Lifecycle](.nimpress/disciple.md#lifecycle).

Two handlers in `disciple/echo` give a disciple a view of it without knowing the prophet. `GetUptime` answers with the last `Heartbeat`, and `GetUptimeStatus` proxies the prophet's public status page for the app's own slug, so a page the disciple serves can draw its uptime:

```go
api.GET("/uptime", saecho.GetUptime)
e.GET("/api/uptime/status", saecho.GetUptimeStatus)
```

The typescript package mirrors the types and reads the public status directly with `getAppStatus(endpoint, slug)`, so a page served from anywhere can draw the same view.

### The prophet contract

The reporter speaks to three routes under `/api/app/:app_slug` on the prophet, all with the `SAuth-App-Slug` and `SAuth-App-Secret` headers behind `ClientAuthMiddleware`.

1. `GET /monitor/client` lists the app's monitors. The reporter reads it first and reuses the monitor named `heartbeat` when one exists, so a restart never registers twice.

2. `POST /monitor/client` with `{"name": "heartbeat", "kind": "heartbeat", "interval_seconds": <interval>}` creates it. `heartbeat` is a monitor kind beside `http`, `tcp` and `disciple`. It has no `target`, the scheduler never probes it, and `interval_seconds` is the beat interval the app promised.

3. `POST /monitor/:id/beat/client` with the beat body records one `monitor_check` row with `ok`, `latency_ms` and, when the beat failed, `error` set to the failed checks' messages, and answers with that `MonitorCheck`. The monitor must belong to the calling app and be of kind `heartbeat`.

```json
{
  "ok": true,
  "latency_ms": 12,
  "version": "1.4.2",
  "checks": [
    {"name": "database", "ok": true},
    {"name": "cache", "ok": false, "error": "dial tcp: connection refused"}
  ]
}
```

A beat is a check, so the incident state machine is unchanged: three failed beats open an incident, the first passing beat resolves it. The scheduler adds one rule for the kind: a `heartbeat` monitor whose newest check is older than two intervals is marked with a failed `monitor_check` whose `error` is `missed heartbeat`, so an app that stops beating goes down the same way an app that answers a probe with an error does. The public `GET /status` lists the heartbeat beside the other monitors with its `last_checked_at`, `last_ok` and `days`.

## Configuration

`sdb.Init` reads every `SAUTH_` variable once and shares it across the module. Your own code never reads a `SAUTH_` variable and never passes one to the library; you set the environment and call `sdb.Init`.

| Variable | Role | Description |
|----------|------|-------------|
| `SAUTH_ENDPOINT` | both | Base URL of the Samna Auth server |
| `SAUTH_APP_SLUG` | both | App identifier slug |
| `SAUTH_APP_SECRET` | disciple | App secret |
| `SAUTH_REDIRECT_URI` | disciple | OAuth redirect URI for the login flow |
| `SAUTH_PAGE_REDIRECT` | disciple | Where the callback lands the browser |
| `SAUTH_DEBUG_USER_ID` | both | UUID that bypasses authentication |
| `SAUTH_DEBUG_CLIENTS` | both | JSON map of debug client credentials |
| `PGHOST` `PGPORT` `PGUSER` `PGPASSWORD` `PGDATABASE` `PGSSLMODE` | both | Database connection |
| `SAUTH_REPLICA_DSN` | both | Optional DSN of a hot standby. Reads that never settle, the scope check, the app, client and tree reads, go to it; one the standby refuses with SQLSTATE `25006` runs again on the primary |

At `sdb.Init` the prophet and disciple resolve the OIDC issuer, token, and jwks endpoints once and log them through the echo logger. A miss disables auth and serves an unavailable response rather than failing startup.

## Object types

A consumer object type passes through the middleware untouched in both directions: `ObjectType.MarshalJSON` sends the string the row stores and `UnmarshalJSON` runs `ResolveObjectType`, so a bare claimius name arrives qualified and every other type is kept as given. The middleware validates only claimius's own types: `ResolveObjectType` in `shared` resolves a bare claimius name such as `organization` to `claimius.organization` and refuses a `claimius.` type the schema does not register, and passes every other type through as given. A consumer type is metadata with an id; the middleware neither qualifies nor refuses it, and `ObjectTypeOf` returns a model's `table:` tag as written. A consumer that wants its own types checked calls `ObjectType.Validate`, which requires the schema and, given a list, membership in it. `upsert_table_info` raises on an unqualified name, so a consumer qualifies its table tags.

The middleware knows the shared claimius types only: `organization`, `location`, `claim`, `claim_object`, `user_claim`, `samna_user`, `samna_app`, `samna_client`, `samna_secret`, `audit`, `object_field`, `user_field`, `user_identity` and `user_relation`. A consumer's own tables register with `upsert_table_info`, and the consumer wraps `claimius.search` and the graph functions in its own SQL for its own shapes.

## Debug mode

Setting `SAUTH_DEBUG_USER_ID` to a valid UUID makes the middleware skip authentication and set that user id directly. Setting `SAUTH_DEBUG_CLIENTS` enables client credential bypass from a static map. Debug mode also skips the sync pull loop.

## Claimius SQL: prophet and disciple sets

The repo ships the Claimius schema as two complete, role tailored base SQL sets, and nothing else for the schema:

- `prophet/database` is the full schema. It hosts many apps and scopes them two ways: an `app_id` column on most tables, and `claimius.app_object` links for `organization` and `location`, which carry no `app_id`. It is the source of truth: a write keeps the closure, settles the writer's own pairs in `user_object`, records what else it changed in `access_diff`, and emits one `sync_event` row per source row it wrote for downstream disciples. It refuses with `P0403` a claim, grant or binding whose owning organization, or whose bound organization or location, is not linked to the row's app, so every row of an app's feed can land on its disciples.
- `disciple/database` is the same schema with `app_id` removed from every table, index, and function, without `app_object`, and without `merge_user`, which samna_auth runs on the prophet. A disciple mirrors exactly one app, so it needs neither mechanism, and it bootstraps with `claimius.upsert_disciple_state(p_disciple_app_slug)`. The feed carries source rows only, and the disciple applies each page in one transaction; nothing is filtered or parked. Under `replay_mode` the calc triggers keep the closure and raise the areas each applied row moves, and the disciple settles its own `user_object` from them on its reads, for the shared types and its own tables alike. In debug mode the same engine serves standalone local writes.

Both scoping mechanisms exist only on the prophet. The disciple schema knows nothing about either. [App scoping](docs/app_scoping.md) has the full rules.

### Reaching a set from a consumer

A consumer holds none of the SQL on disk. Its `database/migrate.yml` reads the role folder as a smig git include at the ref its `go.mod` pins, and `smig up` applies it:

```yaml
- name: Claimius Schema
  type: base
  slug: claimius
  git:
    repo: nimling/samna-auth-middleware
    ref: <tag>
    path: disciple/database
```

A prophet consumer points the same step at `prophet/database`. Moving the ref replays the changed base files through smig's preflight. The table alters a consumer needs to reach the new shape are the consumer's own migration, `V2.0.0__claimius_unified.sql` in its own `database/migrations/`; the middleware carries no `migrations/` folder under either role.

## Testing

```
just test
just test-hurl
```

`just test` runs `go test ./...`, then for each role runs `smig test` on `<role>/database/migrate.yml` fresh and `--from` the last tag, then the `dbtest` tagged Go suites against the container that run keeps. The SQL tests are pgTAP files under `test/sql/<role>/`. The bench suites, `bench.sql` and `settle_share.sql` per role, live untracked under `bench/<role>/` with a schema file pointing at the role's database, and run on demand with `just bench`, which keeps a container per role, sets `claimius.bench` on and prints the measured steps. `just test-hurl` builds the example disciple against a kept disciple database and runs the hurl files under `test/hurl/` against it.

## License

This project is licensed under the MIT (Martin Invented This) License.
</content>
