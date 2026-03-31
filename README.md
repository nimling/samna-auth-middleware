# Samna Auth Middleware

Libraries for integrating with the Samna Auth service. Provides middleware for request authentication, a client library for API calls, echo handlers for building authenticated APIs, a database layer for direct access, a hook library for consuming webhooks, and the canonical SQL schema for the `samna_auth` database.

## Table of Contents

- [Installation](#installation)
- [Packages](#packages)
- [Middleware](#middleware)
- [Echo Handlers](#echo-handlers)
- [Client](#client)
- [Database](#database)
- [SQL Schema](#sql-schema)
- [Hook](#hook)
- [Environment Variables](#environment-variables)
- [Debug User Bypass](#debug-user-bypass)
- [Examples](#examples)
- [Testing](#testing)

## Installation

```
go get github.com/nimling/samna-auth-middleware
```

## Packages

| Package | Description |
|---------|-------------|
| `middleware` | Echo middleware for JWT validation, token refresh, and auth code exchange |
| `echo` | Echo HTTP handlers that bridge the client and database layers |
| `client` | HTTP client for calling the Samna Auth API (framework agnostic) |
| `db` | Database layer for direct PostgreSQL access via Bun ORM |
| `database` | Canonical SQL schema, functions, views, and triggers for the `samna_auth` PostgreSQL schema |
| `hook` | Webhook consumer with RSA signature verification |
| `utils` | Cookie management, environment helpers, and debug utilities |

## Middleware

The middleware validates requests against the Samna Auth service. It handles JWT token validation, token refresh, and authorization code exchange following OAuth 2.0 and OIDC standards.

### Authentication Flow

1. **Check Authorization header** for a `Bearer` token
2. **Check cookie** `sauth_access_token` if no header present
3. **Validate the token** using JWKS from the auth server, verify issuer and expiration
4. If valid, extract user claims, set `user_id` and `access_token` in Echo context
5. If expired, check for `sauth_auth_code` query parameter and exchange it for tokens
6. If no auth code, attempt refresh using `sauth_refresh_token` cookie
7. If all fail, return `401 Unauthorized`

### Usage

```go
import (
    "github.com/labstack/echo/v4"
    "github.com/nimling/samna-auth-middleware/middleware"
)

func main() {
    e := echo.New()

    e.Use(middleware.AuthMiddlewareEnv())

    e.GET("/protected", func(ctx echo.Context) error {
        userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
        accessToken := ctx.Get(middleware.AccessTokenKey).(string)
        return ctx.JSON(200, map[string]any{"user_id": userID})
    })

    e.Start(":8080")
}
```

### Manual Configuration

```go
import "github.com/nimling/samna-auth-middleware/middleware"

openIDUrl, _ := url.Parse("https://auth.example.com/api/client/myapp/.well-known/openid-configuration")

config := &middleware.AuthMiddlewareConfig{
    ClientSlug:      "myapp",
    ClientSecret:    "your-secret",
    RedirectURI:     "https://myapp.example.com/callback",
    OpenIDConfigURL: *openIDUrl,
}

e.Use(middleware.AuthMiddleware(config))
```

### Client Validation Middleware

`ClientMiddleware` validates requests using client slug and secret headers against the auth server:

```go
e.POST("/api/internal", handler, middleware.ClientMiddleware)
```

## Echo Handlers

The `echo` package provides ready to use HTTP handlers that bridge the `client` and `db` packages. Each handler extracts request metadata from the Echo context and delegates to the appropriate client function or database query.

When `SAUTH_DEBUG_USER_ID` is set, handlers that support it will query the database directly instead of calling the auth API.

### Login and Logout

```go
import saecho "github.com/nimling/samna-auth-middleware/echo"

e.GET("/login", saecho.GetLogin)
e.GET("/login/:provider", saecho.GetLogin)
e.GET("/logout", saecho.GetLogout)
```

`GetLogin` detects whether a provider parameter is present. Without a provider it redirects to the default login page. With a provider (e.g. `/login/google`) it redirects to that provider's login page.

`GetLogout` clears authentication cookies and notifies the auth server.

### Available Handlers

**User**: `GetUsers`, `GetUser`, `GetUserClaims`, `GetUserConnections`, `PostUser`, `PutUser`, `DeleteUser`, `GetUserClients`, `GetUserClient`, `PutUserClient`

**Organization**: `GetOrganizations`, `GetOrganization`, `GetOrganizationGraph`, `GetOrganizationClaims`, `PostOrganization`, `PutOrganization`, `DeleteOrganization`

**Location**: `GetLocations`, `GetLocation`, `GetLocationClaims`, `PostLocation`, `PutLocation`, `DeleteLocation`

**Claim**: `GetClaimGraph`, `GetClaims`, `GetClaim`, `GetClaimAudit`, `GetClaimSearch`, `PostClaim`, `PutClaim`, `DeleteClaim`, `PostClaimObject`, `DeleteClaimObject`, `PostClaimUser`, `DeleteClaimUser`

**Me**: `GetMe`, `PutMe`, `GetMeClaims`, `PostMeAsset`, `DeleteMeAsset`, `GetMeConnections`, `GetMeModules`, `GetMeOrganizations`, `GetMeLocations`, `GetMeClients`, `GetMeClient`

**Asset**: `GetAssets`, `GetAsset`, `GetAssetData`, `PostAsset`, `PostAssetUpload`, `PutAsset`, `DeleteAsset`

## Client

The client library makes HTTP calls to the Samna Auth API. It is framework agnostic and takes `context.Context` and `RequestMetadata` instead of any framework specific types.

### Usage

```go
import "github.com/nimling/samna-auth-middleware/client"

meta := client.RequestMetadata{
    AccessToken: "bearer-token",
    RequestID:   "req-123",
    UserIP:      "127.0.0.1",
}

body, err := client.GetUsers(ctx, meta)
if err != nil {
    // handle error
}
defer body.Close()
```

The client reads `SAUTH_ENDPOINT`, `SAUTH_CLIENT_SLUG`, and `SAUTH_CLIENT_SECRET` from environment variables internally.

### Error Handling

```go
import "github.com/nimling/samna-auth-middleware/client"

body, err := client.GetUser(ctx, meta, userID)
if errors.Is(err, client.ErrNotFound) {
    // 404
} else if errors.Is(err, client.ErrForbidden) {
    // 403
} else if errors.Is(err, client.ErrBadRequest) {
    // 400
}
```

## Database

The `db` package provides direct PostgreSQL access via the Bun ORM. It uses a singleton pattern initialized with `db.Init()`.

### Models

`SamnaUser`, `Organization`, `Location`, `Claim`, `UserClaim`, `ClaimObject`, `ActiveClaim`, `ActiveObject`, `ActiveUser`, `GraphNode`

### Access Levels

`SYSTEM` (0), `OWNER` (1), `ADMIN` (2), `WRITE` (3), `UPDATE` (4), `ACTION` (5), `READ` (6), `MEMBER` (7), `GUEST` (8)

### Required Environment Variables

```env
PGHOST=localhost
PGDATABASE=samna
PGPORT=5432
PGUSER=postgres
PGPASSWORD=password
PGSSLMODE=disable
```

### Graph Queries

The `db` package provides two graph query methods that call the `samna_auth` schema functions directly:

- `GetOwnerGraph` returns organizations and locations visible to a user at a given access level
- `GetClaimGraph` returns claims, user claims, users, organizations, locations, and unresolved typed object references for domain objects not managed by the auth schema

Both return `[]GraphNode` where each node carries an id, label, type, and JSONB data payload.

## SQL Schema

The `database/` directory contains the canonical SQL schema for the `samna_auth` PostgreSQL schema. These are Flyway style migration files that define all tables, functions, views, and triggers used by the auth system.

| File | Description |
|------|-------------|
| `V1.0__baseline.sql` | Core tables: `samna_user`, `organization`, `location`, `claim`, `user_claim`, `claim_object`, `samna_client` |
| `V1.1__functions.sql` | Claim management, recursive tree traversal, inheritance queries, access control, graph functions |
| `V1.2__views.sql` | `active_claim`, `active_object`, and `active_users` views |
| `V1.3__triggers.sql` | Timestamp update triggers on all auth tables |
| `V1.4__init.sql` | Calls `init_auth_views` to register domain tables for claim inheritance |

### Graph Functions

Two graph functions are defined in `V1.1__functions.sql`:

- `samna_auth.get_owner_graph(user_id, access_level)` returns organization and location nodes for the ownership hierarchy
- `samna_auth.get_claim_graph(user_id, access_level)` returns claim, user_claim, user, organization, location nodes, and unresolved typed references for domain specific objects

Consuming services extend these by joining their own domain tables onto the unresolved references. For example, the bookable server joins bookable, booking, asset, and code tables onto the graph results.

## Hook

The hook library consumes webhooks from the samna auth service with RSA signature verification and type safe event handlers.

### Supported Entities

User, Organization, Location, Claim, UserClaim, ClaimObject

### Event Types

Create, Update, Delete

### Usage

```go
import "github.com/nimling/samna-auth-middleware/hook"

handlers := hook.Handlers{
    Organization: hook.ActionHandler[hook.Organization]{
        OnCreate: repo.UpsertOrganization,
        OnUpdate: repo.UpsertOrganization,
        OnDelete: repo.DeleteOrganization,
    },
    Location: hook.ActionHandler[hook.Location]{
        OnCreate: repo.CreateLocation,
        OnUpdate: repo.UpdateLocation,
    },
}

e.POST("/webhook", func(ctx echo.Context) error {
    var p hook.Payload
    if err := ctx.Bind(&p); err != nil {
        return ctx.String(http.StatusBadRequest, "Malformed payload")
    }

    err := p.UnmarshalAndHandle(ctx.Request().Context(), handlers)
    if err != nil {
        return ctx.String(http.StatusInternalServerError, "Internal Server Error")
    }

    return ctx.String(http.StatusOK, "OK")
}, hook.WebhookAuthMiddleware(publicKey))
```

Webhook events may arrive out of order. Use upsert patterns and compare `updated_at` timestamps to handle this correctly.

## Environment Variables

### Required

| Variable | Used by | Description |
|----------|---------|-------------|
| `SAUTH_ENDPOINT` | client, middleware | Base URL of the Samna Auth server (e.g. `https://auth.example.com`) |
| `SAUTH_CLIENT_SLUG` | client, middleware | Client identifier slug |
| `SAUTH_CLIENT_SECRET` | client, middleware | Client secret for authentication |
| `SAUTH_REDIRECT_URI` | middleware | OAuth redirect URI for auth code exchange |

### Optional

| Variable | Used by | Description |
|----------|---------|-------------|
| `SAUTH_COOKIE_DOMAIN` | utils | Cookie domain for cross subdomain cookies. Automatically disables `Secure` flag for localhost |
| `SAUTH_DEBUG_USER_ID` | middleware, echo | UUID to bypass authentication entirely (see [Debug User Bypass](#debug-user-bypass)) |

### Database (only needed when using `db` package)

| Variable | Description |
|----------|-------------|
| `PGHOST` | PostgreSQL host |
| `PGDATABASE` | Database name |
| `PGPORT` | PostgreSQL port |
| `PGUSER` | Database user |
| `PGPASSWORD` | Database password |
| `PGSSLMODE` | SSL mode (`disable`, `require`, etc.) |

### Version

| Variable | Description |
|----------|-------------|
| `APP_VERSION` | Application version, managed by sbump |

## Debug User Bypass

Setting `SAUTH_DEBUG_USER_ID` to a valid UUID enables two levels of bypass:

### Middleware Bypass

The auth middleware skips all authentication checks (no token validation, no refresh, no code exchange) and sets the specified user ID directly in the Echo context. All requests proceed as if authenticated by that user.

```env
SAUTH_DEBUG_USER_ID=123e4567-e89b-12d3-a456-426614174000
```

### Echo Handler Bypass

When the debug user ID is set, echo handlers that support it will query the database directly instead of calling the Samna Auth API through the client. This allows running and testing handlers without a running auth server, as long as the database is available. This includes graph handlers (`GetOrganizationGraph`, `GetClaimGraph`) which query the `samna_auth` graph functions directly.

Handlers that do not have a database equivalent (e.g. asset operations, connection lookups) return `501 Not Implemented` in debug mode.

### Manual Configuration

You can also set it programmatically:

```go
devUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

config := &middleware.AuthMiddlewareConfig{
    AuthorizedUserID: &devUserID,
}

e.Use(middleware.AuthMiddleware(config))
```

## Examples

### Middleware Example

```bash
cd example/middleware
cp .env.example .env  # edit with your values
go run main.go
```

### Client Example

```bash
cd example/client
cp .env.example .env  # edit with your values
go run main.go
```

### Hook Example

```bash
cd example/hook
go run main.go
```

## Testing

```bash
go test ./...
```

The token validation test requires environment variables:

```env
SAUTH_JWKS_URL=http://localhost:3201/api/client/sauth/jwks
TEST_TOKEN_STRING=<valid JWT token>
TEST_TOKEN_ISS=http://localhost:3201
```

## License

This project is licensed under the MIT (Martin Invented This) License.
