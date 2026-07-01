# Samna Auth Middleware

Go libraries for integrating with the Samna Auth service. The module ships two roles that share one symmetric surface: a prophet, the source of truth that serves the sync feed and the full admin API, and a disciple, a local replica that pulls from the prophet and consumes tokens.

Every consuming service runs a local PostgreSQL database with the `claimius` schema. The prophet holds every app; a disciple mirrors exactly one. This local copy powers middleware decisions, graph queries, and claim resolution without round trips to the auth server.

## Installation

```
go get github.com/nimling/samna-auth-middleware
```

## The two roles

| Role | Holds | Sync | API |
|------|-------|------|-----|
| Prophet | Full app row with key and seed material | Serves the snapshot and delta feed | `sam.RegisterEndpoints` mounts the full admin API |
| Disciple | App identity from the environment | Pulls in the background via `sam.SyncStart` | `sam.RegisterEndpoints` mounts the me and login routes |

A binary is one role. Both roles wire the same four identifiers.

## Packages

| Package | Prophet import | Disciple import | Role |
|---------|----------------|-----------------|------|
| `sdb` | `prophet/sdb` | `disciple/sdb` | Database layer. `sdb.Init(logger)` connects the pool and loads config once. |
| `echo` | `prophet/echo` | `disciple/echo` | The individual HTTP handlers. |
| `sam` | `prophet/sam` | `disciple/sam` | `sam.RegisterEndpoints(group)`, and on the disciple `sam.SyncStart(ctx)`. |
| `smw` | `smw` | `smw` | Middleware, shared by both roles. |
| `client` | `client` | `client` | HTTP client for the Samna Auth API. |
| `utils` | `utils` | `utils` | Config loader, cookies, and debug helpers. |
| `database` | `prophet/database` | `disciple/database` | The canonical `claimius` SQL set for the role. |

## Getting started

Follow the walkthrough for your role:

- [Prophet](docs/prophet.md)
- [Disciple](docs/disciple.md)

Runnable examples live under [example/prophet](example/prophet) and [example/disciple](example/disciple). Each has a `.env.example`; copy it to `.env`, fill it in, and run `go run main.go`.

## Middleware

The `smw` package validates requests against the Samna Auth service following OAuth 2.0 and OIDC. Its entrypoints:

- `smw.AppMiddleware` is internal. It guards the endpoints apps use to reach the prophet, authenticating the app slug and secret and setting the app context.
- `smw.UserAuthMiddleware(opts)` authenticates browser and bearer users. It checks a bearer token, then a session cookie, then client credentials, and reads the resolved OIDC endpoints from the config that `sdb.Init` loaded. Pass `nil` to take the slug, secret, redirect, and page redirect from the config.
- `smw.ClientAuthMiddleware()` authenticates service to service callers by their client credentials.

After a successful pass the user id and access token sit in the context:

```go
userID := ctx.Get(smw.UserIDKey).(uuid.UUID)
accessToken := ctx.Get(smw.AccessTokenKey).(string)
```

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

At `sdb.Init` the prophet and disciple resolve the OIDC issuer, token, and jwks endpoints once and log them through the echo logger. A miss disables auth and serves an unavailable response rather than failing startup.

## Object types

Object types are schema qualified going into and out of the database and schema stripped over JSON and HTTP, so a caller sees `device` while the row stores `public.device`.

## Debug mode

Setting `SAUTH_DEBUG_USER_ID` to a valid UUID makes the middleware skip authentication and set that user id directly. Setting `SAUTH_DEBUG_CLIENTS` enables client credential bypass from a static map. Debug mode also skips the sync pull loop.

## Claimius SQL: prophet and disciple sets

The repo ships the Claimius schema as two complete, role tailored SQL sets, both embedded in the module:

- `prophet/database` is the full schema, copied as is from the canonical claimius source. It carries `app_id` on every scoped table and hosts many apps. It is the source of truth: writes fire calc triggers that materialize `user_object` and emit `sync_event` rows for downstream disciples.
- `disciple/database` is the same schema with `app_id` removed from every table, index, and function, and with the functions that no consumer calls removed (`init_prophet`, `init_hybrid`, `merge_user`, `recompute_state`). A disciple mirrors exactly one app, so it needs no `app_id`. Its calc engine still runs in debug mode for standalone local writes, and stays dormant under `replay_mode` while sync applies the relation tables shipped from the prophet.

`app_id` exists only on the prophet. It is read from the deployment env into `prophet_state` at init. The disciple schema knows nothing about it.

### Choosing a role at migrate time

A consumer picks its role when it pulls the SQL into its own `database` tree, then migrates with smig:

```
# disciple consumer (e.g. bookable_server)
go run github.com/nimling/samna-auth-middleware/cmd/sqlsync --role=disciple --out=./database/claimius

# prophet consumer (e.g. samna_auth)
go run github.com/nimling/samna-auth-middleware/cmd/sqlsync --role=prophet --out=./database/claimius
```

The materialized files land where `database/migrate.yml` already points its claimius step, and `smig up` applies them. Each role package also exposes `prophet.WriteSQL(dir)` and `disciple.WriteSQL(dir)` if a consumer wants to drive the export from Go. Because Go compiles only the imported package, a disciple consumer that imports `.../disciple` never pulls the prophet set, and the reverse holds for a prophet consumer importing `.../prophet`.

## Testing

```
just test
```

## License

This project is licensed under the MIT (Martin Invented This) License.
</content>
