package samnamw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

func AuthMiddlewareFromEnv() echo.MiddlewareFunc {
	return AuthMiddleware(nil, nil, nil, nil)
}

// AuthMiddleware returns a middleware that validates requests against the Samna Auth
func AuthMiddleware(authURL *url.URL, clientID *uuid.UUID, clientSecretKey *string, clientSecret *string) echo.MiddlewareFunc {

	if authURL == nil {
		envVal, ok := os.LookupEnv("SAMNA_AUTH_SERVER")
		if !ok {
			panic("No authURL specified for AuthMiddleware")
		}

		parsedVal, err := url.Parse(envVal)
		if err != nil {
			panic("The authURL sat from env via SAMNA_AUTH_SERVER is malformed")
		}
		authURL = parsedVal
	}

	if clientID == nil {
		envVal, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_ID")
		if !ok {
			panic("No clientID specified for AuthMiddleware")
		}

		parsedVal, err := uuid.Parse(envVal)
		if err != nil {
			panic("The clientID sat from env via SAMNA_AUTH_CLIENT_ID is malformed")
		}
		clientID = &parsedVal
	}

	if clientSecretKey == nil {
		envVal, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SECRET_KEY")
		if !ok {
			panic("No clientSecretKey specified for AuthMiddleware")
		}

		clientSecretKey = &envVal
	}

	if clientSecret == nil {
		envVal, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SECRET")
		if !ok {
			panic("No clientSecret specified for AuthMiddleware")
		}

		clientSecret = &envVal
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			req := ctx.Request()

			// Temporary bypass for direct tokens

			authHeader := req.Header.Get("Authorization")
			if len(authHeader) > 0 {
				tokenString := strings.TrimPrefix(authHeader, "Bearer ")
				if tokenString != authHeader && len(tokenString) > 0 { //TODO:: What is this?
					return validateToken(ctx, next, *clientSecret, tokenString)
				}
			}

			// Build redirect url
			redirectURI := fmt.Sprintf("%s://%s%s",
				ctx.Scheme(),
				ctx.Request().Host,
				ctx.Request().URL.String())

			// Create a reverse proxy
			proxy := httputil.NewSingleHostReverseProxy(authURL)

			// Modify the request to preserve the original Host header
			req.Host = authURL.Host

			// TODO:: Move all of these over to be configured
			req.Header.Set("Samna-Auth-Internal-Time", fmt.Sprintf("%d", time.Now().Unix()))
			req.Header.Set("Samna-Auth-Original-IP", ctx.RealIP())
			req.Header.Set("Samna-Auth-Client-ID", clientID.String())
			req.Header.Set("Samna-Auth-Secret-Key", *clientSecretKey)
			req.Header.Set("Samna-Auth-Redirect", redirectURI)

			// Create a custom response writer to capture the auth response
			crw := &customResponseWriter{
				header: make(http.Header),
			}

			// Serve the request to the auth service
			proxy.ServeHTTP(crw, req)
			if crw.statusCode >= 300 && crw.statusCode < 400 {
				//TODO:: This weird redirect has to be fixed
				location := crw.Header().Get("Location")
				if !strings.HasPrefix(location, "/auth") {
					return echo.NewHTTPError(http.StatusInternalServerError, "Internal server error")
				}

				redirectLocation, err := authURL.Parse(location)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "Internal server error")
				}

				return ctx.Redirect(http.StatusTemporaryRedirect, redirectLocation.String())
			}

			// Check if the status code is exactly 200
			if crw.statusCode != http.StatusOK {
				return echo.NewHTTPError(crw.statusCode, "Authentication failed")
			}

			// Extract JWT from Authorization header
			authHeader = crw.Header().Get("Authorization")
			if authHeader == "" {
				// TODO:: Change these?
				return echo.NewHTTPError(http.StatusUnauthorized, "Missing Authorization header")
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader { //TODO:: What is this?
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid Authorization header format")
			}

			return validateToken(ctx, next, *clientSecret, tokenString)
		}
	}
}

func validateToken(ctx echo.Context, next echo.HandlerFunc, clientSecret string, tokenString string) error {

	// Parse the JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Note: In a production environment, you should validate the signing method
		// and use the appropriate key (symmetric or asymmetric)
		// TODO:: Add some validation here
		// TODO:: Validate origin here
		return []byte(clientSecret), nil
	})

	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token")
	}

	// Extract the 'sub' claim and set it as user_id in the context
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if sub, ok := claims["sub"].(string); ok {
			userID, err := uuid.Parse(sub)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'sub' claim")
			}
			ctx.Set("user_id", userID)
		} else {
			return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'sub' claim")
		}

		if userStr, ok := claims["user"].(string); ok {
			var user ClaimUser
			if err := json.Unmarshal([]byte(userStr), &user); err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'user' claim")
			}
			ctx.Set("user", user)
		} else {
			return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'user' claim")
		}
	} else {
		return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token claims")
	}

	// If we've made it this far, the request is authenticated
	return next(ctx)
}

type customResponseWriter struct {
	header     http.Header
	statusCode int
	body       bytes.Buffer
}

func (c *customResponseWriter) Header() http.Header {
	return c.header
}

func (c *customResponseWriter) Write(b []byte) (int, error) {
	return c.body.Write(b)
}

func (c *customResponseWriter) WriteHeader(statusCode int) {
	c.statusCode = statusCode
}
