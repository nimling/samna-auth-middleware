# Disciple

The disciple is a local replica. It never signs or decrypts, its app identity comes from the environment, and it keeps its database current by pulling from the prophet in the background. It consumes tokens; it does not issue them.

## Packages

| Package | Import | Role |
|---------|--------|------|
| `sdb` | `disciple/sdb` | Database layer. `sdb.Init(logger)` connects the pool and loads config once. |
| `echo` | `disciple/echo` | The individual HTTP handlers: the me routes and the login flow. |
| `sam` | `disciple/sam` | `sam.RegisterEndpoints(group)` mounts the handlers, and `sam.SyncStart(ctx)` runs the pull loop. |
| `smw` | `smw` | Middleware. |

## main.go

```go
package main

import (
	"context"

	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	smw "github.com/nimling/samna-auth-middleware/smw"
)

func main() {
	e := echo.New()

	if _, err := sdb.Init(e.Logger); err != nil {
		e.Logger.Fatal(err)
	}

	api := e.Group("", smw.UserAuthMiddleware(nil))
	sam.RegisterEndpoints(api)

	sam.SyncStart(context.Background())

	e.Logger.Fatal(e.Start(":8080"))
}
```

## Steps

1. `sdb.Init(e.Logger)` connects the database and loads the config from the environment once. The app identity comes from `SAUTH_APP_SLUG` and `SAUTH_APP_SECRET`. Nothing else in your code reads the `SAUTH_` variables.

2. `sam.RegisterEndpoints(group)` mounts the login, callback, logout and me routes. To wire them yourself, mount the handlers from the `echo` package one by one, for example `g.GET("/me", echo.GetMe)`.

3. `smw.UserAuthMiddleware(opts)` protects the routes. Pass `nil` to take the slug, secret, redirect and page redirect from the config.

4. `sam.SyncStart(ctx)` launches the background pull loop that keeps the local database current. It takes a snapshot on first run, then polls the prophet for deltas, backing off on failure and logging through the database logger. Call it once; it returns immediately and runs until the context ends.

## Notes

There is no inbound sync endpoint on a disciple. The disciple reaches out to the prophet.

Object types come back schema stripped over JSON and stay schema qualified in the database.
</content>
