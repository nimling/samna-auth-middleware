# Samna Auth Middleware

Go libraries for integrating with the Samna Auth service. Provides JWT middleware, a sync engine for keeping a local copy of auth data, Echo HTTP handlers, a client library for API calls, direct database access, and the canonical SQL schema.

All consuming services run a local PostgreSQL database with the `samna_auth` schema. On startup the sync engine pulls a full snapshot from the auth server, and incremental updates arrive via the sync endpoint. This local copy powers middleware decisions, graph queries, and claim resolution without round trips to the auth server.

## Installation

```
go get github.com/nimling/samna-auth-middleware
```

## Packages

| Package | Description |
|---------|-------------|
| `middleware` | Echo middleware for JWT validation, token refresh, and client authentication |
| `sync` | Pull based sync engine that keeps the local auth database up to date |
| `echo` | Echo HTTP handlers bridging the client and database layers |
| `client` | HTTP client for the Samna Auth API (framework agnostic) |
| `db` | Database layer for direct PostgreSQL access via Bun ORM |
| `database` | Canonical SQL schema, functions, views, and triggers for `samna_auth` |
| `utils` | Cookie management, environment helpers, and debug utilities |

## Examples

| Example | Description | When to use |
|---------|-------------|-------------|
| [basic](example/basic) | Minimal setup with middleware, sync, and a single protected route | Starting a new service that needs user authentication |
| [echo_handlers](example/echo_handlers) | Full API with all handler groups mounted | Building a service that proxies the full auth API |
| [client_only](example/client_only) | Direct HTTP calls to the auth server without local database | Service to service calls where you already have a bearer token |

Each example has a `.env.example` file. Copy it to `.env` and fill in your values before running.

```bash
cd example/basic
cp .env.example .env
go run main.go
```

## Startup Sequence

Every service using this library follows the same pattern:

1. Register `sync.PullSnapshot` as the sync function
2. Call `db.Init()` which connects to PostgreSQL, runs the initial snapshot sync, and retries up to 5 times on failure
3. Mount the middleware and handlers

```go
db.RegisterSyncFunc(sync.PullSnapshot)
db.Init(e.Logger)
```

The sync endpoint (`saecho.PostSync`) receives incremental updates from the auth server. Mount it so your local database stays current:

```go
e.POST("/sync", saecho.PostSync)
```

## Middleware

The middleware validates requests against the Samna Auth service. It handles JWT token validation, token refresh, and authorization code exchange following OAuth 2.0 and OIDC standards.

### Authentication Flow

1. Check `Authorization` header for a `Bearer` token
2. Check `sauth_access_token` cookie if no header present
3. Validate the token using JWKS, verify issuer and expiration
4. If valid, set `user_id` and `access_token` in the Echo context
5. If expired, check for `sauth_auth_code` query parameter and exchange it
6. If no auth code, attempt refresh using `sauth_refresh_token` cookie
7. If all fail, return `401 Unauthorized`

### User Authentication

```go
e.Use(middleware.AuthMiddlewareEnv())
```

Or with manual configuration:

```go
config := &middleware.AuthMiddlewareConfig{
    ClientSlug:   "myapp",
    ClientSecret: "your-secret",
    RedirectURI:  "https://myapp.example.com/callback",
}
e.Use(middleware.AuthMiddleware(config))
```

### Client Authentication

For service to service calls using client key and secret headers:

```go
e.Use(middleware.Auth(&middleware.AuthOptions{
    ClientAuth: &middleware.ClientAuthOptions{},
}))
```

### Combined Authentication

`Auth` chains user and client authentication. If client headers (`SAuth-Client-Key`, `SAuth-Client-Secret`) are present and no `Authorization` header exists, the request is authenticated as a client. Otherwise it falls through to user authentication.

```go
e.Use(middleware.Auth(nil))
```

### Context Values

After successful authentication:

```go
userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
accessToken := ctx.Get(middleware.AccessTokenKey).(string)
```

## Sync

The sync package keeps the local `samna_auth` database in sync with the auth server.

### Initial Snapshot

`PullSnapshot` fetches all current auth data and applies it to the local database. This runs automatically during `db.Init()` when a sync function is registered.

### Incremental Sync

`PullSync` fetches events that occurred after the last applied sequence number. Use this for periodic catchup.

### Sync Endpoint

The `saecho.PostSync` handler receives push events from the auth server. It validates the request using app slug and secret headers, applies the payload, and updates the sequence number.

### Supported Entity Types

`organization`, `location`, `samna_user`, `claim`, `user_claim`, `claim_object`, `client_claim`, `samna_client`

## Echo Handlers

Ready to use HTTP handlers that bridge the `client` and `db` packages. Each handler extracts request metadata from the Echo context and delegates to the appropriate layer.

When `SAUTH_DEBUG_USER_ID` is set, handlers query the database directly instead of calling the auth API.

### Login and Logout

```go
e.GET("/login", saecho.GetLogin)
e.GET("/login/:provider", saecho.GetLogin)
e.GET("/logout", saecho.GetLogout)
```

### Available Handlers

**User**: `GetUsers`, `GetUser`, `GetUserClaims`, `GetUserConnections`, `PostUser`, `PutUser`, `DeleteUser`, `GetUserApps`, `GetUserApp`, `PutUserApp`

**Organization**: `GetOrganizations`, `GetOrganization`, `GetOrganizationGraph`, `GetOrganizationClaims`, `PostOrganization`, `PutOrganization`, `DeleteOrganization`

**Location**: `GetLocations`, `GetLocation`, `GetLocationClaims`, `PostLocation`, `PutLocation`, `DeleteLocation`

**Claim**: `GetClaimGraph`, `GetClaims`, `GetClaim`, `GetClaimAudit`, `PostClaim`, `PutClaim`, `DeleteClaim`, `PostClaimObject`, `DeleteClaimObject`, `PostClaimUser`, `DeleteClaimUser`

**Me**: `GetMe`, `PutMe`, `GetMeClaims`, `PostMeAsset`, `DeleteMeAsset`, `GetMeRelations`, `GetMeModules`, `GetMeOrganizations`, `GetMeLocations`, `GetMeApps`, `GetMeApp`

**Asset**: `GetAssets`, `GetAsset`, `GetAssetData`, `PostAsset`, `PostAssetUpload`, `PutAsset`, `DeleteAsset`

**Sync**: `PostSync`

## Client

The client library makes HTTP calls to the Samna Auth API. It is framework agnostic and takes `context.Context` and `RequestMetadata`.

```go
meta := client.RequestMetadata{
    AccessToken: "bearer-token",
    RequestID:   "req-123",
    UserIP:      "127.0.0.1",
}

body, err := client.GetUsers(ctx, meta)
if err != nil {
    log.Fatal(err)
}
defer body.Close()
```

The client reads `SAUTH_ENDPOINT`, `SAUTH_APP_SLUG`, and `SAUTH_APP_SECRET` from environment variables.

### Error Handling

```go
if errors.Is(err, client.ErrNotFound) {
    // 404
} else if errors.Is(err, client.ErrForbidden) {
    // 403
} else if errors.Is(err, client.ErrBadRequest) {
    // 400
}
```

## Database

The `db` package provides direct PostgreSQL access via Bun ORM with a singleton pattern.

### Models

`SamnaUser`, `Organization`, `Location`, `Claim`, `UserClaim`, `ClaimObject`, `SamnaClient`, `UserClaimAccess`, `ObjectField`, `UserField`, `UserRelation`, `Audit`, `UserObject`, `GraphNode`

### Access Levels

| Level | Name | Value |
|-------|------|-------|
| System | `ACCESS_LEVEL_SYSTEM` | 0 |
| Owner | `ACCESS_LEVEL_OWNER` | 1 |
| Admin | `ACCESS_LEVEL_ADMIN` | 2 |
| Write | `ACCESS_LEVEL_WRITE` | 3 |
| Update | `ACCESS_LEVEL_UPDATE` | 4 |
| Action | `ACCESS_LEVEL_ACTION` | 5 |
| Read | `ACCESS_LEVEL_READ` | 6 |
| Member | `ACCESS_LEVEL_MEMBER` | 7 |
| Guest | `ACCESS_LEVEL_GUEST` | 8 |

### Graph Queries

Two graph functions query the `samna_auth` schema directly:

- `GetOwnerGraph` returns organizations and locations visible to a user at a given access level
- `GetClaimGraph` returns claims, user claims, users, organizations, locations, and unresolved typed object references for domain objects

Both return `[]GraphNode` where each node carries an id, label, type, and JSONB data payload. Consuming services extend the graph by joining their own domain tables onto the unresolved references.

## SQL Schema

The `database/` directory contains Flyway style migrations for the `samna_auth` PostgreSQL schema.

| File | Description |
|------|-------------|
| `V1.0__baseline.sql` | Core tables: `samna_user`, `organization`, `location`, `claim`, `user_claim`, `claim_object`, `samna_client` |
| `V1.1__functions.sql` | Claim management, recursive tree traversal, inheritance, access control, graph functions |
| `V1.2__views.sql` | `active_claim`, `active_object`, `active_users` views |
| `V1.3__triggers.sql` | Timestamp update triggers |
| `V1.4__init.sql` | Calls `init_auth_views` to register domain tables for claim inheritance |

## Environment Variables

### Required

| Variable | Used by | Description |
|----------|---------|-------------|
| `SAUTH_ENDPOINT` | middleware, client, sync | Base URL of the Samna Auth server |
| `SAUTH_APP_SLUG` | middleware, client, sync | App identifier slug |
| `SAUTH_APP_SECRET` | middleware, client, sync | App secret for authentication |
| `SAUTH_REDIRECT_URI` | middleware | OAuth redirect URI for auth code exchange |

### Database

| Variable | Description |
|----------|-------------|
| `PGHOST` | PostgreSQL host |
| `PGDATABASE` | Database name |
| `PGPORT` | PostgreSQL port |
| `PGUSER` | Database user |
| `PGPASSWORD` | Database password |
| `PGSSLMODE` | SSL mode (`disable`, `require`, etc.) |

### Optional

| Variable | Used by | Description |
|----------|---------|-------------|
| `SAUTH_COOKIE_DOMAIN` | utils | Cookie domain for cross subdomain cookies |
| `SAUTH_DEBUG_USER_ID` | middleware, echo | UUID to bypass authentication (see below) |

## Debug Mode

Setting `SAUTH_DEBUG_USER_ID` to a valid UUID enables two levels of bypass:

**Middleware**: Skips all authentication checks and sets the specified user ID directly in the Echo context. No token validation, no refresh, no code exchange.

**Echo Handlers**: Query the database directly instead of calling the auth API. This allows running handlers without a running auth server as long as the database is available. Graph handlers (`GetOrganizationGraph`, `GetClaimGraph`) query the `samna_auth` graph functions directly. Handlers without a database equivalent return `501 Not Implemented`.

```env
SAUTH_DEBUG_USER_ID=123e4567-e89b-12d3-a456-426614174000
```

Or programmatically:

```go
devUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
config := &middleware.AuthMiddlewareConfig{
    AuthorizedUserID: &devUserID,
}
e.Use(middleware.AuthMiddleware(config))
```

## Testing

```bash
go test ./...
```

The token validation test requires:

```env
SAUTH_JWKS_URL=http://localhost:3201/api/client/sauth/jwks
TEST_TOKEN_STRING=<valid JWT token>
TEST_TOKEN_ISS=http://localhost:3201
```

## License

This project is licensed under the MIT (Martin Invented This) License.
