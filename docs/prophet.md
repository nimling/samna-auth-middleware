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

## Notes

The prophet serves the sync snapshot and delta feed at `sync/snapshot` and `sync/:seq` under the registered group. It does not run `SyncStart`; that is the disciple side.

Object types come back schema stripped over JSON and stay schema qualified in the database.
</content>
