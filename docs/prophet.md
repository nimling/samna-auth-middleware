# Prophet

The prophet is the source of truth. It holds the full app row including key and seed material, authenticates the apps that talk to it, serves the sync feed, and exposes the full admin API. A prophet never pulls; it serves.

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
	sam "github.com/nimling/samna-auth-middleware/prophet/sam"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	smw "github.com/nimling/samna-auth-middleware/smw"
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

2. `smw.AppMiddleware` guards the group the apps reach the prophet through. It authenticates the app slug and secret and sets the app context the handlers rely on. It is internal to the ecosystem.

3. `sam.RegisterEndpoints(group)` mounts the user, relation, organization, location, claim, asset, app, guest, sync, client, secret, search, code, field and notification routes. The sync feed the disciples consume is part of this set. Organization, location, user, claim, client, secret and relation take `PATCH` beside `PUT`. A grant is addressed by claim and member within the calling app: `PATCH` and `DELETE` on `claim/:claim_id/object/:id` name the bound object, and on `claim/:claim_id/user/:id` the granted user.

4. `smw.UserAuthMiddleware(opts)` protects browser and bearer facing routes. Pass `nil` to take the slug, secret, redirect and page redirect from the config. `smw.ClientAuthMiddleware()` protects service to service routes.

## App scoping

The prophet hosts every app, so every read it performs is scoped. Most tables carry an `app_id` column. `organization` and `location` do not: they are shared objects linked to apps through `claimius.app_object`, filtered with `app_object_ids(app_id, object_type)` and linked with `link_app_object`. See [App scoping](app_scoping.md).

## Machine clients

`/samna_client` under the registered group creates the service accounts other services authenticate with. `POST` returns the raw secret once and stores only its bcrypt hash; `POST /:id/rotate` issues a new one; `POST /:id/claim` binds a claim to the client. The client is always created under the app whose API it will call, because the sync feed is filtered by `app_id`. See [Machine clients](clients.md).

## Consent and capture

The prophet is where consent and interaction capture live. `claimius.consent` records a decision per app and per visitor, keyed on the user when there is one and on the anonymous id when there is not, with the granted categories, the policy version, the ip and the user agent. `claimius.analytics_session` and `claimius.analytics_event` hold what was captured, one row per signal.

`RegisterEndpoints` mounts the routes a disciple forwards to: `GET` and `POST /consent`, `POST /analytics`, and `GET /analytics/event` and `/analytics/session` for reading back. `analytics.Init(db)` starts the async writer, so capture never blocks a request.

Consent is enforced in SQL, not in the handler. `post_analytics_events` writes nothing when the analytics category is absent for that visitor, so a disciple that forwards a batch for someone who has not granted it stores nothing.

Reach is enforced in SQL too. Every capture row carries the app_id of the app that recorded it, and an app reads another app's capture by holding a live `app_object` link to that app's `samna_app` row, the same link organizations and locations cross apps on. `claimius.capture_app_ids` returns the readable set, `claimius.analytics_enabled` answers whether an app has capture switched on, and `claimius.capture_app_access` combines the two. `get_analytics_events` and `get_analytics_sessions` take the calling app and the target app and return nothing when the pair does not resolve, so the scope cannot be bypassed by a handler.

The policy a decision is recorded against and the document the banner links are held in the middleware. The link resolves from `SAUTH_ENDPOINT` and the `/legal/cookie-policy` page the prophet serves beside its other legal documents, so a consumer configures neither.

## Notes

Sync is pull only. The prophet serves the snapshot and delta feed at `GET sync/snapshot` and `GET sync/:seq` under the registered group, the delta read through `claimius.get_sync_events`, which scopes organizations and locations through `app_object`. There is no inbound sync route on the prophet and it does not run `SyncStart`; that is the disciple side.

Object types are schema qualified over JSON and in the database alike.
</content>
