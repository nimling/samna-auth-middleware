# Auth Middleware

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
- Calls OAuth token endpoint with `grant_type=refresh_token`
- If successful:
    - Extracts user claims from token claims
    - Sets them in Echo context
    - Allows request to proceed
- If refresh fails (e.g., refresh token expired):
  - Returns `401 Unauthorized`

### Step 6: Extract User Information
- After successful authentication (via valid token or refresh/exchange)
- User Claims ars available in Echo context
- **Note:** for now only `userID is available`
- Access it in your handlers: `userID := ctx.Get(samnamw.UserIDKey).(uuid.UUID)`

## Configuration

### Option 1: Environment Variables (Recommended)

Configure the middleware using environment variables. This is the simplest approach for most use cases.

```go
package main

import (
    "github.com/labstack/echo/v4"
    samnamw "github.com/nimling/samna-auth-middleware/sauth"
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

# OAuth token endpoint URL (for token exchange and refresh)
SAMNA_AUTH_TOKEN_URL=https://auth.example.com/oauth/token

# JWKS endpoint URL (for fetching public keys to validate JWT signatures)
SAMNA_AUTH_JWKS_URL=https://auth.example.com/api/client/myapp/jwks

# Expected token issuer (must match the issuer claim in JWT tokens)
SAMNA_AUTH_EXPECTED_TOKEN_ISSUER=https://auth.example.com

# Cookie domain (optional, for cross-subdomain cookies)
# If not set, cookies are set for the current domain only
SAMNA_AUTH_COOKIE_DOMAIN=.example.com

# Redirect URI (used during authorization code exchange)
# Must match the redirect URI registered in the auth server
SAMNA_AUTH_CLIENT_REDIRECT_URI=https://myapp.example.com/callback
```

**Example `.env` file:**

```env
SAMNA_AUTH_CLIENT_ID=00000000-0000-0000-0000-000000000000
SAMNA_AUTH_CLIENT_SLUG=myapp
SAMNA_AUTH_CLIENT_SECRET=your-client-secret
SAMNA_AUTH_TOKEN_URL=http://localhost:3200/oauth/token
SAMNA_AUTH_JWKS_URL=http://localhost:3200/api/client/myapp/jwks
SAMNA_AUTH_EXPECTED_TOKEN_ISSUER=http://localhost:3200
SAMNA_AUTH_COOKIE_DOMAIN=localhost
SAMNA_AUTH_CLIENT_REDIRECT_URI=http://localhost:8080/callback
```

### Option 2: Manual Configuration

For programmatic configuration or when you need to set values at runtime:

```go
package main

import (
    "net/url"
    
    "github.com/google/uuid"
    "github.com/labstack/echo/v4"
    samnamw "github.com/nimling/samna-auth-middleware/sauth"
)

func main() {
    e := echo.New()
    
    // Build configuration manually
    tokenURL, _ := url.Parse("https://auth.example.com/oauth/token")
    jwksURL, _ := url.Parse("https://auth.example.com/api/client/myapp/jwks")
    
    config := &samnamw.AuthMiddlewareConfig{
        ClientID:            uuid.MustParse("00000000-0000-0000-0000-000000000000"),
        ClientSlug:          "myapp",
        ClientSecret:        "your-client-secret",
        TokenURL:            *tokenURL,
        JWKSURL:             *jwksURL,
        ExpectedTokenIssuer: "https://auth.example.com",
        CookieDomain:        ".example.com",
        RedirectURI:         "https://myapp.example.com/callback",
    }
    
    // Apply middleware with manual config
    e.Use(samnamw.AuthMiddleware(config))
    
    e.Start(":8080")
}
```

## Accessing User Information

After successful authentication, access the user ID in your handlers:

```go
import (
    "github.com/google/uuid"
    samnamw "github.com/nimling/samna-auth-middleware/sauth"
)

func protectedHandler(ctx echo.Context) error {
    // Get user ID from context
    userID := ctx.Get(samnamw.UserIDKey).(uuid.UUID)
    
    // Use user ID in your logic
    return ctx.JSON(200, map[string]interface{}{
        "user_id": userID.String(),
    })
}
```

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

A complete example is provided in the `/example` directory. To run it:

1. Create a `.env` file with your configuration:
```env
SAMNA_AUTH_SERVER=http://localhost:3200
SAMNA_AUTH_CLIENT_ID=your-client-id
SAMNA_AUTH_CLIENT_SLUG=your-client-slug
SAMNA_AUTH_CLIENT_SECRET=your-client-secret
SAMNA_AUTH_TOKEN_URL=http://localhost:3200/oauth/token
SAMNA_AUTH_JWKS_URL=http://localhost:3200/api/client/your-client-slug/jwks
SAMNA_AUTH_EXPECTED_TOKEN_ISSUER=http://localhost:3200
SAMNA_AUTH_CLIENT_REDIRECT_URI=http://localhost:8080/callback
```

2. Run the example:
```bash
cd example
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

### Using AuthorizedUserID with Manual Configuration

```go
package main

import (
    "net/url"

    "github.com/google/uuid"
    "github.com/labstack/echo/v4"
    samnamw "github.com/nimling/samna-auth-middleware/sauth"
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

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT (Martin Invented This) License.