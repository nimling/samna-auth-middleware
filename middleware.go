package samnamw

import (
	"encoding/base64"
	"encoding/json"
	"github.com/labstack/echo/v4"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

// AuthMiddleware returns a middleware that validates requests against the Samna Auth
func AuthMiddleware(authURL *url.URL) echo.MiddlewareFunc {

	if authURL == nil {
		authURLString, ok := os.LookupEnv("SAMNA_AUTH_SERVER")
		if !ok {
			panic("No authURL specified for AuthMiddleware")
		}

		parsedURL, err := url.Parse(authURLString)
		if err != nil {
			panic("The authURL sat from env via SAMNA_AUTH_SERVER is malformed")
		}
		authURL = parsedURL
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			req := ctx.Request()
			res := ctx.Response()

			// Create a reverse proxy
			proxy := httputil.NewSingleHostReverseProxy(authURL)

			// Modify the request to preserve the original Host header
			req.Host = authURL.Host

			// Create a custom response writer to capture the status code and headers
			crw := &customResponseWriter{ResponseWriter: res.Writer}
			res.Writer = crw

			// Serve the request to the auth service
			proxy.ServeHTTP(res.Writer, req)

			// Check if the status code is exactly 200
			if crw.status != http.StatusOK {
				return echo.NewHTTPError(crw.status, "Authentication failed")
			}

			// Extract JWT from Authorization header
			authHeader := crw.Header().Get("Authorization")
			if authHeader == "" {
				// TODO:: Change these?
				return echo.NewHTTPError(http.StatusUnauthorized, "Missing Authorization header")
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid Authorization header format")
			}

			// Extract the 'sub' claim from the JWT payload
			parts := strings.Split(tokenString, ".")
			if len(parts) != 3 {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token format")
			}

			payload, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token payload")
			}

			var claims map[string]interface{}
			if err := json.Unmarshal(payload, &claims); err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token claims")
			}

			if sub, ok := claims["sub"].(string); ok {
				ctx.Set("user_id", sub)
			} else {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'sub' claim")
			}

			// If we've made it this far, the request is authenticated
			return next(ctx)
		}
	}
}

type customResponseWriter struct {
	http.ResponseWriter
	status int
}

func (crw *customResponseWriter) WriteHeader(status int) {
	crw.status = status
	crw.ResponseWriter.WriteHeader(status)
}
