# Samna Auth Middleware

This package provides libraries for integrating with the Samna Auth service. It includes middleware for request authentication, a client library for making API calls, and a hook library for handling webhooks.

## Table of Contents

- [Middleware](#middleware) - Request authentication and JWT validation
- [Hook](#hook) - Webhook consumer for receiving events from samna-auth
- [Client](#client) - Client library for making API calls to samna-auth
- [Contributing](#contributing)
- [License](#license)

---

## Middleware

This package provides a simple middleware for the Echo web framework that validates requests against the Samna Auth service. It handles JWT token validation, token refresh, and authorization code exchange following OAuth 2.0 and OIDC standards.

## Installation

```
go get github.com/nimling/samna-auth-middleware
```

## How It Works

The middleware follows this step-by-step authentication flow:

### Step 1: Check for Access Token in Authorization Header
- The middleware first checks for an `Authorization` header with a `Bearer` token
- Format: `Authorization: Bearer <access_token>`
- If found, proceeds to Step 3 (validation)

### Step 2: Check for Access Token in Cookie
- If no Authorization header is present, checks for an `sauth_access_token` cookie
- If found, proceeds to Step 3 (validation)

### Step 3: Validate Access Token
- Validates the token signature using JWKS (JSON Web Key Set) from the auth server.
    **Note:** for managing JWKS was used [github.com/MicahParks/keyfunc](https://github.com/MicahParks/keyfunc) - in memory storage
- Verifies the token's issuer matches the expected issuer
- Checks token expiration
- If valid:
  - Extracts user claims from token claims
  - Sets them in Echo context
  - Allows request to proceed
- If expired or invalid:
  - Logs the error
  - Proceeds to Steps 4-5 (refresh via `auth_code` or `refresh_token`)

### Step 4: Check for Authorization Code
- If no valid access token is found, checks for `sauth_auth_code` query parameter
- This typically happens after OAuth provider callback
- If found:
  - Exchanges authorization code for access/refresh token pair via OAuth token endpoint
  - Sets tokens in secure HTTP-only cookies:
    - `sauth_access_token` (with expiration)
    - `sauth_refresh_token` (long-lived)
  - Redirects to the original request URL (without the auth code parameter)

### Step 5: Refresh Tokens
- If no authorization code is present, attempts to refresh tokens
- Reads `sauth_refresh_token` from cookies
- Calls OAuth token endpoint with:
    - `grant_type=sauth_refresh_token` (instead of standard `refresh_token`)
    - Basic authentication using `clientSlug` as username and `clientSecret` as password
- If successful:
    - Extracts user claims from token claims
    - Sets them in Echo context
    - Allows request to proceed
- If refresh fails (e.g., refresh token expired):
    - Returns `401 Unauthorized`

### Step 6: Extract User Information
- After successful authentication (via valid token or refresh/exchange)
- User claims and access token are available in Echo context
- **Note:** for now only `userID` is available from user claims
- Access user ID in your handlers: `userID := ctx.Get(samnamw.UserIDKey).(uuid.UUID)`
- Access token is also set in context and can be retrieved: `accessToken := ctx.Get(samnamw.AccessTokenKey).(string)`

## Configuration

### Option 1: Environment Variables (Recommended)

Configure the middleware using environment variables. This is the simplest approach for most use cases.

```go
package main

import (
    "github.com/labstack/echo/v4"
    samnamw "github.com/nimling/samna-auth-middleware/middleware"
)

func main() {
    e := echo.New()
    
    // Middleware automatically loads configuration from environment variables
    e.Use(samnamw.AuthMiddlewareEnv())
    
    // Your protected routes
    e.GET("/api/protected", func(ctx echo.Context) error {
        userID := ctx.Get(samnamw.UserIDKey).(uuid.UUID)
        return ctx.JSON(200, map[string]interface{}{
            "user_id": userID,
        })
    })
    
    e.Start(":8080")
}
```

**Required Environment Variables:**

```env
# Client identifier (UUID format)
SAMNA_AUTH_CLIENT_ID=00000000-0000-0000-0000-000000000000

# Client slug (used for OAuth endpoints)
SAMNA_AUTH_CLIENT_SLUG=myapp

# Client secret for authenticating requests to auth server
SAMNA_AUTH_CLIENT_SECRET=your-client-secret

# Cookie domain (optional, for cross-subdomain cookies)
# If not set, cookies are set for the current domain only
SAMNA_AUTH_COOKIE_DOMAIN=.example.com

# Redirect URI (used during authorization code exchange)
# Must match the redirect URI registered in the auth server
SAMNA_AUTH_CLIENT_REDIRECT_URI=https://myapp.example.com/callback

# OpenID configuration URL
SAMNA_AUTH_OPENID_CONFIG_URL=http://localhost:3200/api/client/bookable/.well-known/openid-configuration
```

**Example `.env` file:**

```env
SAMNA_AUTH_CLIENT_ID=00000000-0000-0000-0000-000000000000
SAMNA_AUTH_CLIENT_SLUG=myapp
SAMNA_AUTH_CLIENT_SECRET=your-client-secret
SAMNA_AUTH_COOKIE_DOMAIN=localhost
SAMNA_AUTH_CLIENT_REDIRECT_URI=http://localhost:8080/callback
SAMNA_AUTH_OPENID_CONFIG_URL=http://localhost:3200/api/client/bookable/.well-known/openid-configuration
```

### Option 2: Manual Configuration

For programmatic configuration or when you need to set values at runtime:

```go
package main

import (
	"net/url"
	"os"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	samnamw "github.com/nimling/samna-auth-middleware/middleware"
)

func main() {
	e := echo.New()

	// Build configuration manually
	openIDUrl, _ := url.Parse("http://localhost:3200/api/client/sauth/.well-known/openid-configuration")

	config := &samnamw.AuthMiddlewareConfig{
		ClientSlug:   "myapp",
		ClientSecret: "your-client-secret",
		OpenIDConfigURL: openIDUrl,
		CookieDomain: ".example.com",
		RedirectURI:  "https://myapp.example.com/callback",
	}

	// Apply middleware with manual config
	e.Use(samnamw.AuthMiddleware(config))

	e.Start(":8080")
}
```

## Accessing User Information

After successful authentication, access the user ID and access token in your handlers:

```go
import (
    "github.com/google/uuid"
    samnamw "github.com/nimling/samna-auth-middleware/middleware"
)

func protectedHandler(ctx echo.Context) error {
    // Get user ID from context
    userID := ctx.Get(samnamw.UserIDKey).(uuid.UUID)
    
    // Get access token from context (useful for making API calls to samna-auth)
    accessToken := ctx.Get(samnamw.AccessTokenKey).(string)
    
    // Use user ID and access token in your logic
    return ctx.JSON(200, map[string]interface{}{
        "user_id": userID.String(),
        // Note: Don't expose access token in responses for security reasons
    })
}
```

**Note:** The access token is automatically set in the Echo context by the middleware and can be used for making authenticated API calls to the samna-auth service (e.g., using the client library).

## Cookie Configuration

The middleware automatically manages authentication cookies:

- **Cookie Names:**
  - `sauth_access_token` - Access token (short-lived)
  - `sauth_refresh_token` - Refresh token (long-lived, 30 days default)

- **Cookie Settings:**
  - `HttpOnly: true` - Prevents JavaScript access (XSS protection)
  - `Secure: true` - Only sent over HTTPS (automatically disabled for localhost)
  - `SameSite: Lax` - CSRF protection
  - Domain and path can be configured via `SAMNA_AUTH_COOKIE_DOMAIN`

## Error Handling

The middleware returns appropriate HTTP status codes:

- **200 OK** - Authentication successful, request proceeds
- **302 Found** - Redirect after authorization code exchange
- **401 Unauthorized** - Authentication failed (invalid/expired tokens, missing credentials)
- **500 Internal Server Error** - Server error

All errors are logged via Echo's logger for debugging.

## Example Application

A complete example is provided in the `/example/middleware` directory. To run it:

1. Create a `.env` file with your configuration:
```env
SAMNA_AUTH_SERVER=http://localhost:3200
SAMNA_AUTH_CLIENT_ID=your-client-id
SAMNA_AUTH_CLIENT_SLUG=your-client-slug
SAMNA_AUTH_CLIENT_SECRET=your-client-secret
SAMNA_AUTH_OPENID_CONFIG_URL=http://localhost:3200/api/client/bookable/.well-known/openid-configuration
SAMNA_AUTH_CLIENT_REDIRECT_URI=http://localhost:8080/callback
```

2. Run the example:
```bash
cd example/middleware
go run main.go
```

## Testing

Run tests with:

```bash
go test ./...
```

Tests validate JWT token parsing and validation logic.

## Development Mode (Temporary Feature)

⚠️ **Warning**: This is a temporary feature intended for development and testing only. It will be removed in future versions.

For development and testing purposes, you can bypass all authentication by setting `AuthorizedUserID` in the configuration. When this is set, the middleware will:

- Skip all authentication checks (no token validation, no refresh, no code exchange)
- Automatically set the specified user ID in the Echo context
- Allow all requests to proceed without requiring valid tokens

### Using AuthorizedUserID via Environment Variable

The simplest way to enable development mode is to set the `SAMNA_AUTH_AUTHORIZED_USER_ID` environment variable:

```env
# Development mode: bypass authentication
SAMNA_AUTH_AUTHORIZED_USER_ID=123e4567-e89b-12d3-a456-426614174000
```

When using `AuthMiddlewareEnv()`, the middleware will automatically read this environment variable and enable development mode if it's set.

### Using AuthorizedUserID with Manual Configuration

```go
package main

import (
    "net/url"

    "github.com/google/uuid"
    "github.com/labstack/echo/v4"
    samnamw "github.com/nimling/samna-auth-middleware/middleware"
)

func main() {
    e := echo.New()

    // Development mode: bypass authentication
    devUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
    
    config := &samnamw.AuthMiddlewareConfig{
        AuthorizedUserID: &devUserID,  // Set this to bypass auth
        // Other config fields are not required when AuthorizedUserID is set
    }
    
    e.Use(samnamw.AuthMiddleware(config))
    
    e.GET("/protected", func(ctx echo.Context) error {
        // This will always be the AuthorizedUserID value
        userID := ctx.Get(samnamw.UserIDKey).(uuid.UUID)
        return ctx.JSON(200, map[string]interface{}{
            "user_id": userID,
        })
    })
    
    e.Start(":8080")
}
```

---

## Hook

The hook library provides functionality for consuming webhooks from the samna-auth service. It handles webhook payload validation, signature verification, and provides a type-safe way to handle different entity types and events.

### Features

- **Signature Verification**: Validates webhook payloads using RSA signature verification
- **Type-Safe Handlers**: Provides strongly-typed handlers for different entity types (User, Organization, Location, Claim, etc.)
- **Event Handling**: Supports create, update, and delete events for each entity type
- **Easy Integration**: Simple middleware for Echo framework

### How It Works

1. **Webhook Authentication**: The `WebhookAuthMiddleware` verifies the RSA signature of incoming webhook requests
2. **Payload Parsing**: The payload is parsed and the entity type is determined
3. **Handler Execution**: Based on the entity type and event type, the appropriate handler is called

### Usage

```go
package main

import (
    "context"
    "crypto/rsa"
    "net/http"

    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"
    "github.com/nimling/samna-auth-middleware/hook"
)

type Repository struct {
    // Your database or storage implementation
}

func (r *Repository) UpsertOrganization(ctx context.Context, org hook.Organization) error {
    // Handle organization create/update
    return nil
}

func (r *Repository) DeleteOrganization(ctx context.Context, org hook.Organization) error {
    // Handle organization delete
    return nil
}

func (r *Repository) CreateLocation(ctx context.Context, loc hook.Location) error {
    // Handle location create
    return nil
}

func (r *Repository) UpdateLocation(ctx context.Context, loc hook.Location) error {
    // Handle location update
    return nil
}

func main() {
    e := echo.New()
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())

    repo := &Repository{}
    
    // Get the client's public key (from environment or configuration)
    publicKey := &rsa.PublicKey{} // Load your actual public key here

    e.POST("/webhook", func(ctx echo.Context) error {
        var p hook.Payload
        if err := ctx.Bind(&p); err != nil {
            return ctx.String(http.StatusBadRequest, "Malformed payload")
        }

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
            // Add handlers for other entity types as needed
        }

        err := p.UnmarshalAndHandle(ctx.Request().Context(), handlers)
        if err != nil {
            return ctx.String(http.StatusInternalServerError, "Internal Server Error")
        }

        return ctx.String(http.StatusOK, "OK")
    }, hook.WebhookAuthMiddleware(publicKey))

    e.Logger.Fatal(e.Start(":8080"))
}
```

### Supported Entity Types

- **User**: User account information
- **Organization**: Organization entities
- **Location**: Location entities
- **Claim**: Access control claims
- **UserClaim**: User-claim associations
- **ClaimObject**: Claim-object associations

### Event Types

- **Create**: Entity was created
- **Update**: Entity was updated
- **Delete**: Entity was deleted

### Handling Out-of-Order Events

⚠️ **Important**: Due to network delays and distributed system architecture, webhook events may not arrive in the same order they were created or sent. It is crucial to handle this properly to ensure data consistency.

#### Recommendations

1. **Use Upsert Pattern for Updates**: Since an update event might arrive before a create event, it's recommended to handle update events like create events (upsert pattern). This means your update handler should be able to create the entity if it doesn't exist:

```go
func (r *Repository) UpsertOrganization(ctx context.Context, org hook.Organization) error {
    // Check if organization exists
    existing, err := r.GetOrganization(ctx, org.ID)
    if err != nil && !errors.Is(err, ErrNotFound) {
        return err
    }
    
    if existing == nil {
        // Entity doesn't exist, create it
        return r.CreateOrganization(ctx, org)
    }
    
    // Entity exists, update it
    return r.UpdateOrganization(ctx, org)
}
```

2. **Check `updated_at` Timestamp**: To prevent processing stale or duplicate events, always compare the event's `updated_at` timestamp with the entity's current `updated_at` in your database. If the event's timestamp is older, skip processing:

```go
func (r *Repository) UpsertOrganization(ctx context.Context, org hook.Organization) error {
    existing, err := r.GetOrganization(ctx, org.ID)
    if err != nil && !errors.Is(err, ErrNotFound) {
        return err
    }
    
    // If entity exists and event is older, skip it
    if existing != nil && existing.UpdatedAt.After(org.UpdatedAt) {
        // Event is outdated, skip processing
        return nil
    }
    
    // Proceed with create or update
    if existing == nil {
        return r.CreateOrganization(ctx, org)
    }
    return r.UpdateOrganization(ctx, org)
}
```

3. **Idempotent Operations**: Ensure all your handlers are idempotent - processing the same event multiple times should have the same effect as processing it once. This helps handle duplicate events gracefully.

### Signature Verification

The webhook middleware verifies the RSA signature sent in the `X-Signature-RSA` header. You need to provide the client's public key when setting up the middleware:

```go
// Load public key from environment or configuration
publicKey := loadPublicKey() // Your implementation

e.POST("/webhook", handler, hook.WebhookAuthMiddleware(publicKey))
```

### Example Application

A complete example is provided in the `/example/hook` directory. To run it:

1. Set up your public key (replace the placeholder in the example)
2. Run the example:
```bash
cd example/hook
go run main.go
```

---

## Client

The client library provides a convenient way to make API calls to the samna-auth service. It handles authentication, request metadata, and provides type-safe methods for common operations.

### Features

- **Type-Safe API**: Strongly-typed request and response models
- **Automatic Authentication**: Handles client authentication headers automatically
- **Request Metadata**: Supports request ID and user IP tracking
- **Error Handling**: Provides structured error types for different HTTP status codes

### Usage

```go
package main

import (
    "errors"
    "net/http"
    "os"

    "github.com/google/uuid"
    "github.com/joho/godotenv"
    "github.com/labstack/echo/v4"
    "github.com/nimling/samna-auth-middleware/client"
    sauthmw "github.com/nimling/samna-auth-middleware/middleware"
)

func main() {
    _ = godotenv.Load(".env")

    // Initialize the client
    apiURL := os.Getenv("SAMNA_AUTH_SERVER_API_URL")
    clientSlug := os.Getenv("SAMNA_AUTH_CLIENT_SLUG")
    clientSecret := os.Getenv("SAMNA_AUTH_CLIENT_SECRET")

    sauthClient := client.NewSAuthClient(apiURL, clientSlug, clientSecret)

    e := echo.New()
    authMiddleware := sauthmw.AuthMiddlewareEnv()

    // Example: Update user
    e.PUT("/user/:id", func(ctx echo.Context) error {
        userID, err := uuid.Parse(ctx.Param("id"))
        if err != nil {
            return ctx.String(http.StatusBadRequest, "Invalid user ID")
        }

        // Get access token from context (set by auth middleware)
        accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
        if !ok {
            return ctx.String(http.StatusUnauthorized, "Missing access token")
        }

        // Prepare request metadata
        meta := client.RequestMetadata{
            AccessToken: accessToken,
            RequestID:   ctx.Request().Header.Get("X-Request-ID"),
            UserIP:      ctx.RealIP(),
        }

        // Update user
        body := client.UpdateUserBody{
            FirstName: stringPtr("John"),
            LastName:  stringPtr("Doe"),
            Email:     stringPtr("john.doe@example.com"),
        }

        err = sauthClient.PutUser(ctx.Request().Context(), meta, userID, body)
        if err != nil {
            return handleClientError(ctx, err)
        }

        return ctx.NoContent(http.StatusNoContent)
    }, authMiddleware)

    e.Logger.Fatal(e.Start(":8080"))
}

func handleClientError(ctx echo.Context, err error) error {
    switch {
    case errors.Is(err, client.ErrBadRequest):
        return ctx.String(http.StatusBadRequest, "Bad Request")
    case errors.Is(err, client.ErrForbidden):
        return ctx.String(http.StatusForbidden, "Forbidden")
    case errors.Is(err, client.ErrNotFound):
        return ctx.String(http.StatusNotFound, "Not Found")
    default:
        return ctx.String(http.StatusInternalServerError, "Internal Server Error")
    }
}

func stringPtr(s string) *string {
    return &s
}
```

### Available Operations

The client library provides methods for:

- **User Operations**:
  - `PutUser` - Update user information

- **Organization Operations**:
  - `PostOrganization` - Create organization
  - `PutOrganization` - Update organization
  - `DeleteOrganization` - Delete organization

- **Location Operations**:
  - `PostLocation` - Create location
  - `PutLocation` - Update location
  - `DeleteLocation` - Delete location

- **Claim Operations**:
  - `PostClaim` - Create claim
  - `PutClaim` - Update claim
  - `DeleteClaim` - Delete claim
  - `PostClaimObject` - Associate claim with object
  - `DeleteClaimObject` - Remove claim-object association
  - `PostClaimUser` - Associate claim with user
  - `DeleteClaimUser` - Remove claim-user association

### Configuration

The client requires the following environment variables:

```env
# Samna Auth Server API URL
SAMNA_AUTH_SERVER_API_URL=http://localhost:3201

# Client credentials for authenticating with samna-auth
SAMNA_AUTH_CLIENT_SLUG=your-client-slug
SAMNA_AUTH_CLIENT_SECRET=your-client-secret
```

### Request Metadata

When making API calls, you should provide request metadata:

```go
meta := client.RequestMetadata{
    AccessToken: accessToken,  // Required: from auth middleware context
    RequestID:   requestID,   // Optional: for request tracing
    UserIP:      userIP,      // Optional: for logging/analytics
}
```

### Error Handling

The client library provides structured error types:

- `client.ErrBadRequest` - 400 Bad Request
- `client.ErrForbidden` - 403 Forbidden
- `client.ErrNotFound` - 404 Not Found

Handle errors appropriately in your code:

```go
err := sauthClient.PutUser(ctx.Request().Context(), meta, userID, body)
if err != nil {
    switch {
    case errors.Is(err, client.ErrBadRequest):
        // Handle bad request
    case errors.Is(err, client.ErrForbidden):
        // Handle forbidden
    case errors.Is(err, client.ErrNotFound):
        // Handle not found
    default:
        // Handle other errors
    }
}
```

### Example Application

A complete example is provided in the `/example/client` directory. To run it:

1. Create a `.env` file with your configuration (see `.env.example`):
```env
SAMNA_AUTH_SERVER_API_URL=http://localhost:3201
SAMNA_AUTH_CLIENT_SLUG=your-client-slug
SAMNA_AUTH_CLIENT_SECRET=your-client-secret
```

2. Run the example:
```bash
cd example/client
go run main.go
```

---

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT (Martin Invented This) License.