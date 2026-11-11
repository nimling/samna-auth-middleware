# Prophet

The prophet is the source of truth. It holds the full app row including key and seed material, authenticates the apps that talk to it, serves the sync feed, and exposes the full admin API. It is the home of its own apps and decides their writes; a prophet that mirrors an app of a peer prophet pulls that one app through its `sam.Init`.

## Packages

| Package | Import | Role |
|---------|--------|------|
| `sdb` | `prophet/sdb` | Database layer. `sdb.Init(logger)` connects the pool, loads config once, and resolves the OIDC endpoints. |
| `echo` | `prophet/echo` | The individual HTTP handlers. |
| `sam` | `prophet/sam` | `sam.RegisterEndpoints(group)` mounts the full API on a group. |
| `smw` | `smw` | Middleware. |

## main.go

```go
package main

import (
	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	smw "github.com/nimling/samna-auth-middleware/v2/smw"
)

func main() {
	e := echo.New()

	if _, err := sdb.Init(e.Logger); err != nil {
		e.Logger.Fatal(err)
	}

	api := e.Group("/sam", smw.AppMiddleware)
	sam.RegisterEndpoints(api)

	protected := e.Group("/api", smw.UserAuthMiddleware(nil))
	protected.GET("/ping", func(ctx echo.Context) error { return ctx.String(200, "pong") })

	e.Logger.Fatal(e.Start(":8080"))
}
```

## Steps

1. `sdb.Init(e.Logger)` connects the database, loads the config from the environment once, and resolves and logs the OIDC issuer, token and jwks endpoints. Nothing else in your code reads the `SAUTH_` variables.

2. `smw.AppMiddleware` guards the group the apps reach the prophet through. It authenticates the app slug and secret and sets the app context the handlers rely on. It is internal to the ecosystem. `sam.RegisterEndpoints` adds `smw.SyncSeq`, which answers every successful write with the app's feed position in `SAuth-Seq`, and `smw.AppHome`, which refuses a write for an app another prophet decides with `409` and that prophet in `SAuth-Home`.

3. `sam.RegisterEndpoints(group)` mounts the user, relation, organization, location, claim, asset, object_asset, app, sync, client, secret, search, code, cost, profile, user_name, field, notification, consent and analytics routes; `POST user/guest` (`PostUserGuest`) runs `claimius.insert_organization_guest(created_by, owner_id, user, ends_at, reason)` and answers the `samna_user` of type `guest` with `201` for a new one and `200` for a reused one, signing no token, and a grant to the anonymous user, `shared.AnonymousUserID`, through `POST claim/:claim_id/user` with `options` is the public grant of its claim. The sync feed the disciples consume is part of this set. Organization, location, user, claim, client, secret, object_asset, cost and relation take `PATCH` beside `PUT`. A binding and a grant are addressed by claim and their own row id within the calling app: `PATCH` and `DELETE` on `claim/:claim_id/object/:id` name the `claim_object` row, and on `claim/:claim_id/user/:id` the `user_claim` row; `DELETE samna_client/:id/claim/:claim_id` removes a client's claim.

The prophet starts its runtime once with `sam.Init(runCtx)` (`prophet/sam`), which reads `sdb.Instance()` and starts notification, which the SSE notification stream needs, analytics, whose hourly fold runs on one replica, and the peer sync for every `prophet_peer` row, on a run context `main` cancels after `e.Shutdown`. The host registers `notification.EndStreams` and `prophetecho.EndStreams` with `e.Server.RegisterOnShutdown` and joins `sam.Done()` after the http server has shut down, as [Lifecycle](../.nimpress/disciple.md#lifecycle) orders. A held `GET sync/:cursor` waits on `claimius_sync` through the process's one listener, `(*shared.DB).Listen`, for as long as the request holds.

4. `smw.UserAuthMiddleware(opts)` protects browser and bearer facing routes. Pass `nil` to take the slug, secret, redirect and page redirect from the config. `smw.ClientAuthMiddleware()` protects service to service routes. A host that runs its own credential check, as samna_auth does with its session middleware, chains `smw.AnonymousMiddleware` after it: a request that already carries an actor in `smw.UserIDKey` passes through untouched, any other is resolved as the anonymous user exactly as on a disciple, with the rate limit, the code from `samk`, `SAuth-Code` or the cookie, the in memory public grants and the `403` to `401` rewrite, per app: the app from `app_id` on the context, else the one `SAuth-App-Slug` names, else the system app. Each application decides where it mounts it. Three steps for such a host: chain the middleware; open its own pool through `shared.GetCodeHashConnector(connector)`, the wrapper `shared.Connect` uses, which sets `claimius.samk` on the connection before every statement whose context carries the presented code, so `GetAccess`, `MutationAccess` and `ObjectWhere` on that pool judge a code gated grant (`sql.OpenDB(shared.GetCodeHashConnector(pgdriver.NewConnector(...)))`); and call `shared.DeletePublicClaimCache()` after its own writes of claims, bindings and grants outside the `/sam/claim` handlers.

## App scoping

The prophet hosts every app, so every read it performs is scoped. Most tables carry an `app_id` column. `organization` and `location` do not: they are shared objects linked to apps through `claimius.app_object`, filtered with `app_object_ids(app_id, object_type)` and linked with `upsert_app_object`. See [App scoping](app_scoping.md).

## Machine clients

`/samna_client` under the registered group creates the service accounts other services authenticate with. `POST` returns the raw secret once and stores only its bcrypt hash; `POST /:id/rotate` issues a new one; `POST /:id/claim` binds a claim to the client. The client is always created under the app whose API it will call, because the sync feed is filtered by `app_id`. See [Machine clients](clients.md).

## Consent and capture

The prophet is where consent and interaction capture live. `claimius.consent` records a decision per app and per visitor, keyed on the user when there is one and on the anonymous id when there is not, with the granted categories, the policy version, the ip and the user agent. `claimius.analytics_session` and `claimius.analytics_event` hold what was captured, one row per signal.

`RegisterEndpoints` mounts the routes a disciple forwards to: `GET` and `POST /consent`, `POST /analytics` and `POST /analytics/capture`, and `GET /analytics/app`, `/analytics/event`, `/analytics/session`, `/analytics/capture`, `/analytics/heat` and `/analytics/summary` for reading back. `sam.RegisterCapture(group)` and `sam.RegisterCaptureRead(group)` mount the same handlers for the prophet's own portal. `sam.Init` starts the async analytics writer, so capture never blocks a request.

Consent is enforced in SQL, not in the handler. `insert_analytics_events` writes nothing when the analytics category is absent for that visitor, so a disciple that forwards a batch for someone who has not granted it stores nothing.

Reach is enforced in SQL too. Every capture row carries the app_id of the app that recorded it, and a person reads capture through a live analytics grant: a `claim_object` with no object whose `scope` carries `analytics`, with read and no deny. The grant reaches the app its claim sits in and every app id listed under `analytics.app` in its scope. `claimius.capture_user_apps` returns that set for one person and `claimius.analytics_enabled` answers whether an app has capture switched on. `get_analytics_events`, `get_analytics_sessions` and `get_analytics_summary` take the reader and an optional producer app and return only rows of apps in that set, so the scope cannot be bypassed by a handler; the app id names the producer and never decides access.

The policy a decision is recorded against and the document the banner links are held in the middleware. The link resolves from `SAUTH_ENDPOINT` and the `/legal/cookie-policy` page the prophet serves beside its other legal documents, so a consumer configures neither.

## Notes

Sync is pull only. The prophet serves the snapshot and delta feed at `GET sync/snapshot` and `GET sync/:cursor` under the registered group, the delta read through `claimius.get_sync_events` after a `<txid>.<seq>` cursor, source rows only, organizations and locations once per linked app. A pull is served only when `claimius.check_sync_home` agrees the prophet serves that consumer; otherwise it answers `409` with the prophet the consumer stays with in `SAuth-Home`. There is no inbound sync route on the prophet and its `sam.Init` runs no disciple pull; that is the disciple side.

Federation runs in SQL and in one Go call. Each `claimius.prophet_peer` row names a peer prophet, its endpoint and the slug and secret of one app mirrored from it; the prophet `sam.Init(ctx)` pulls every row as that app, with `peer=true`, and applies each row through `claimius.apply_peer_event`. `claimius.insert_app_handover` and `claimius.update_app_handover` move an app's home between two prophets. See [Prophets and disciples](../.nimpress/claimius/prophets.md).

Object types are schema qualified over JSON and in the database alike.
</content>
