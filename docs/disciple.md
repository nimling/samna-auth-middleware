# Disciple

The disciple is a local replica. It never signs or decrypts, its app identity comes from the environment, and it keeps its database current by pulling from the prophet in the background. It consumes tokens; it does not issue them.

## Packages

| Package | Import | Role |
|---------|--------|------|
| `sdb` | `disciple/sdb` | Database layer. `sdb.Init(logger)` connects the pool and loads config once. |
| `echo` | `disciple/echo` | The individual HTTP handlers: the me routes, the login flow, notifications, consent, capture and uptime. |
| `sam` | `disciple/sam` | `sam.RegisterEndpoints(group)` mounts the handlers, `sam.RegisterCapture(group)` and `sam.RegisterCaptureRead(group)` mount consent and capture, and `sam.SyncStart(ctx)` runs the pull loop. |
| `smw` | `smw` | Middleware. |

## main.go

```go
package main

import (
	"context"

	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	smw "github.com/nimling/samna-auth-middleware/v2/smw"
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

2. `sam.RegisterEndpoints(group)` mounts the login, callback, logout, me and notification routes, and consent and capture through `sam.RegisterCapture`. To wire them yourself, mount the handlers from the `echo` package one by one, for example `g.GET("/me", echo.GetMe)`.

3. `smw.UserAuthMiddleware(opts)` protects the routes. Pass `nil` to take the slug, secret, redirect and page redirect from the config.

4. `sam.SyncStart(ctx)` launches the background pull loop that keeps the local database current. Call it on every pod; one pod per database takes the sync lock and pulls, the others stand by. The puller takes a snapshot on first run, then pulls one page after its cursor at a time, each held open by the prophet while there is nothing new, backing off on failure and logging through the database logger. It returns immediately and runs until the context ends.

## App scoping

A disciple mirrors exactly one app, so nothing it holds needs an app filter. Its tables carry no `app_id` column, it has no `app_object` table, and the sync layer strips `app_id` from every payload before upsert. Write app scoping on the prophet side only. See [App scoping](app_scoping.md).

When a disciple answers 403 on every listing endpoint while the service is otherwise healthy, check the organization count in its replica first. `RootAccess` resolves through the actor's root organization, and an empty organization table fails every one of those gates.

## Serving other services

A disciple that other services read from mounts `smw.UserAuthMiddleware(nil)` on the group they call, which accepts a bearer token, a session cookie, or client credentials. Use `smw.ClientAuthMiddleware()` instead when the group is only ever reached machine to machine. The client id becomes the actor, so the same claims and the same `ScopeAccess` grants decide the answer. The client row itself is created on the prophet and arrives by sync. See [Machine clients](clients.md).

## Consent and capture

Interaction capture records clicks, pointer samples, scroll depth, inputs and page views for a visitor who has granted the analytics category, and it works for anonymous visitors as well as signed in ones. The disciple never writes it and never holds it. Every consent decision and every capture batch forwards to the prophet on the app key, and every read goes back to the prophet the same way the audit log does. Capture is app owned: each row carries the app_id of the app that recorded it, so it stays on the prophet, where a person reads it through a live analytics grant that reaches the app its claim sits in and every app id listed under `analytics.app` in its scope. `claimius.capture_user_apps` resolves that set at read time, which is how a nimployer reader sees what nim.io recorded.

The handlers are the same shape as every other one in `disciple/echo`, so a consumer mounts them itself:

```go
sauth := e.Group("/api/sauth")
sauth.GET("/analytics/consent", saecho.GetConsent)
sauth.POST("/analytics/consent", saecho.PostConsent)
sauth.POST("/analytics", saecho.PostAnalytics)
sauth.POST("/analytics/capture", saecho.PostAnalyticsCapture)
```

`sam.RegisterCapture(group)` mounts those four in one call, and `sam.RegisterEndpoints` already includes it. Mount either on a group with no session requirement: a visitor answers the banner before they have a session, and capture has to reach anonymous visitors too.

`sam.RegisterCaptureRead(group)` mounts `GET /analytics/app`, `/analytics/event`, `/analytics/session`, `/analytics/capture`, `/analytics/heat` and `/analytics/summary`. Those read across visitors, so they belong behind a session and behind whatever page grant gates the consumer's admin surface.

The browser half is one call, `configureConsent({ endpoint: '/api/sauth' })`, which reads consent, starts capture only once the analytics category is granted, and raises the banner when the visitor has not decided under the current policy. The policy link and version arrive in the consent response, resolved on the prophet from its endpoint and its `/legal/cookie-policy` page.

## Uptime

The prophet probes the app from outside; `uptime.Init` is the app reporting from inside. Call it once after `sdb.Init`, hand it the checks that decide whether the app is healthy, and it registers the app's `heartbeat` monitor on the prophet and beats on the interval. The [Uptime](../README.md#uptime) section of the readme carries the options, the handlers and the prophet contract.

```go
uptime.Init(e.Logger, uptime.Options{
	Version: version,
	Checks:  []uptime.Check{{Name: "database", Run: sdb.Instance().PingContext}},
})
```

`saecho.GetUptime` answers with the app's own last beat and `saecho.GetUptimeStatus` proxies the prophet's public status for the app's slug. Mount either where the app wants the view; neither is part of `sam.RegisterEndpoints`.

## Notes

Sync is pull only. There is no inbound sync endpoint on a disciple and none on the prophet; the disciple reaches out to `GET /sam/sync/snapshot` and `GET /sam/sync/:cursor` through `sam.SyncStart`, with a `(txid, seq)` cursor written `<txid>.<seq>`. The feed carries source rows only, and the disciple settles its own access from them. It binds to the prophet that answers it and routes its writes there. See [Sync](sync.md).

Object types pass through the middleware untouched over JSON and into the database. A disciple's own tables are qualified by the consumer in their `table:` tags, which `ObjectTypeOf` returns as written, and `upsert_table_info` raises on an unqualified name. `ObjectType.Validate` is the opt in check for the consumer's own types.
</content>
