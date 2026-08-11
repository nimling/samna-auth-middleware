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

3. `sam.RegisterEndpoints(group)` mounts the user, organization, location, claim, asset, app, guest, sync, client, secret, search, code and field routes. The sync feed the disciples consume is part of this set.

4. `smw.UserAuthMiddleware(opts)` protects browser and bearer facing routes. Pass `nil` to take the slug, secret, redirect and page redirect from the config. `smw.ClientAuthMiddleware()` protects service to service routes.

## App scoping

The prophet hosts every app, so every read it performs is scoped. Most tables carry an `app_id` column. `organization` and `location` do not: they are shared objects linked to apps through `claimius.app_object`, filtered with `app_object_ids(app_id, object_type)` and linked with `link_app_object`. See [App scoping](app_scoping.md).

## Machine clients

`/samna_client` under the registered group creates the service accounts other services authenticate with. `POST` returns the raw secret once and stores only its bcrypt hash; `POST /:id/rotate` issues a new one; `POST /:id/claim` binds a claim to the client. The client is always created under the app whose API it will call, because the sync feed is filtered by `app_id`. See [Machine clients](clients.md).

## Consent and capture

The prophet is where consent and interaction capture live. `claimius.consent` records a decision per app and per visitor, keyed on the user when there is one and on the anonymous id when there is not, with the granted categories, the policy version, the ip and the user agent. `claimius.analytics_session` and `claimius.analytics_event` hold what was captured, one row per signal.

`RegisterEndpoints` mounts the routes a disciple forwards to: `GET` and `POST /consent`, `POST /analytics`, and `GET /analytics/event` and `/analytics/session` for reading back. `analytics.Init(db)` starts the async writer, so capture never blocks a request.

Consent is enforced in SQL, not in the handler. `post_analytics_events` writes nothing when the analytics category is absent for that visitor, so a disciple that forwards a batch for someone who has not granted it stores nothing.

`SAUTH_CONSENT_POLICY_VERSION` and `SAUTH_CONSENT_POLICY_PATH` set the policy a decision is recorded against and the document the banner links. Raising the version makes every visitor decide again.

## Notes

The prophet serves the sync snapshot and delta feed at `sync/snapshot` and `sync/:seq` under the registered group. It does not run `SyncStart`; that is the disciple side.

Object types come back schema stripped over JSON and stay schema qualified in the database.
</content>
