package samnamw

import (
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/nimling/samna-auth-middleware/writer"
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

	// Create CORS middleware specifically for auth endpoints
	corsMiddleware := middleware.CORSWithConfig(middleware.CORSConfig{
		AllowCredentials: true,
		AllowOrigins:     []string{"https://<auth-host>", "https://<bookable-host>"},
		AllowHeaders:     []string{"Cookie", "Set-Cookie"},
		ExposeHeaders:    []string{"Set-Cookie"},
	})

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return corsMiddleware(func(ctx echo.Context) error {
			req := ctx.Request()

			// Build redirect url
			redirectURI := fmt.Sprintf("%s://%s%s",
				ctx.Scheme(),
				ctx.Request().Host,
				ctx.Request().URL.String()) //TODO:: How to build this for the proper return?

			// Only rewrite path for initial requests to our API
			if req.Host != authURL.Host {
				// Set the validate path
				req.URL.Scheme = authURL.Scheme
				req.URL.Host = authURL.Host
				req.URL.Path = authURL.Path
				req.Host = authURL.Host
				ctx.Logger().Infof("test: Initial request, reverse proxy to: %s", req.URL.String())
			} else {
				ctx.Logger().Infof("test: Redirect request, keeping original path: %s", req.URL.Path)
			}

			ctx.Logger().Info("test: built redirect uri " + redirectURI)

			// Create a reverse proxy
			proxy := httputil.NewSingleHostReverseProxy(authURL)
			proxy.Director = func(req *http.Request) {

				req.URL.Scheme = authURL.Scheme
				req.URL.Host = authURL.Host

				// Properly forward all required headers
				req.Header.Set("X-Forwarded-For", req.Header.Get("X-Forwarded-For"))
				req.Header.Set("X-Forwarded-Proto", req.Header.Get("X-Forwarded-Proto"))
				req.Header.Set("X-Forwarded-Host", req.Header.Get("X-Forwarded-Host"))
				req.Header.Set("Host", authURL.Host)

				// Explicitly preserve and forward all cookies, including access_token
				if originalCookie := ctx.Request().Header.Get("Cookie"); originalCookie != "" {
					req.Header.Set("Cookie", originalCookie)
					ctx.Logger().Infof("director: Forwarding cookies: %s", originalCookie)
				} else {
					ctx.Logger().Info("director: No cookies found to forward")
				}

				// Important: Copy ALL original cookies
				if cookies := req.Cookies(); len(cookies) > 0 {
					for _, cookie := range cookies {
						ctx.Logger().Infof("director: Cookie being forwarded: %s", cookie.Name)
					}
				}

				// Add required auth headers TODO:: Are these really needed?
				req.Header.Set("Samna-Auth-Internal-Time", fmt.Sprintf("%d", time.Now().Unix()))
				req.Header.Set("Samna-Auth-Original-IP", ctx.RealIP())
				req.Header.Set("Samna-Auth-Client-ID", clientID.String())
				req.Header.Set("Samna-Auth-Secret-Key", *clientSecretKey)
				req.Header.Set("Samna-Auth-Redirect", redirectURI)

				// Nice logging for redirect tracing
				ctx.Logger().Infof("director: Final URL being requested: %s", req.URL.String())
			}

			// Enhanced response modification
			proxy.ModifyResponse = func(resp *http.Response) error {
				// Ensure CORS headers for cookie handling
				resp.Header.Set("Access-Control-Allow-Credentials", "true")
				resp.Header.Add("Access-Control-Expose-Headers", "Set-Cookie")

				// Properly handle Set-Cookie headers
				if cookies := resp.Header.Values("Set-Cookie"); len(cookies) > 0 {
					// Clear existing Set-Cookie headers to prevent duplicates
					resp.Header.Del("Set-Cookie")
					for _, cookie := range cookies {
						resp.Header.Add("Set-Cookie", cookie)
					}
				}
				return nil
			}

			proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				ctx.Logger().Errorf("Proxy error: %v", err)
				w.WriteHeader(http.StatusBadGateway)
			}

			// Create an enhanced response writer
			crw := writer.NewCustomResponseWriter(ctx.Response().Writer)

			// Serve the request to the auth service
			proxy.ServeHTTP(crw, req)

			// Extract JWT from Authorization header
			authHeader := crw.Header().Get("Authorization")
			if authHeader != "" {
				ctx.Logger().Info("test: auth header: " + authHeader)
			} else if crw.StatusCode >= 300 && crw.StatusCode < 400 {
				// TODO:: Geting favicon as an extra redirect here ??
				//TODO:: This weird redirect has to be fixed
				location := crw.Header().Get("Location")

				ctx.Logger().Info("test: received code 300-400 with location: " + location)

				if !strings.HasPrefix(location, "/auth") {
					return echo.NewHTTPError(http.StatusInternalServerError, "Internal server error")
				}

				redirectLocation, err := authURL.Parse(location)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "Internal server error")
				}

				ctx.Logger().Info("test: redirecting to: " + redirectLocation.String())

				return ctx.Redirect(http.StatusTemporaryRedirect, redirectLocation.String())
			} else if crw.StatusCode != http.StatusOK {
				ctx.Logger().Infof("test: response not ok, code %d", crw.StatusCode)
				//TODO:: Add proper returns here, maybe expose the respond package for echo in golang
				return echo.NewHTTPError(crw.StatusCode, "Authentication failed")
			}

			if authHeader == "" {
				ctx.Logger().Info("test: Missing auth header in auth response")
				// TODO:: Change these?
				return echo.NewHTTPError(http.StatusUnauthorized, "Missing Authorization header")
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader { //TODO:: What is this?
				ctx.Logger().Info("test: Invalid Authorization header format")
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid Authorization header format")
			}

			return validateToken(ctx, next, *clientSecret, tokenString)
		})
	}
}

func validateToken(ctx echo.Context, next echo.HandlerFunc, clientSecret string, tokenString string) error {

	ctx.Logger().Info("test: Validating token")
	// Parse the JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Note: In a production environment, you should validate the signing method
		// and use the appropriate key (symmetric or asymmetric)
		// TODO:: Add some validation here
		// TODO:: Validate origin here
		ctx.Logger().Info("test: Validating token with secret")
		return []byte(clientSecret), nil
	})

	if err != nil {
		ctx.Logger().Info("test: Validating token") // TODO:: WE need request id to do proepr logging here
		return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token")
	}

	//TODO:: We need to wait for expiration to not overload the server

	// Extract the 'sub' claim and set it as user_id in the context
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if sub, ok := claims["sub"].(string); ok {
			userID, err := uuid.Parse(sub)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'sub' claim")
			}
			ctx.Set("user_id", userID)
			ctx.Logger().Info("test: UserID sat from token: " + sub)
		} else {
			ctx.Logger().Info("test: Failed to set UserID from token")
			return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'sub' claim")
		}

		if userStr, ok := claims["user"].(string); ok {
			var user ClaimUser
			if err := json.Unmarshal([]byte(userStr), &user); err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'user' claim")
			}
			ctx.Set("user", user)
			ctx.Logger().Info("test: User sat from token: " + userStr)
		} else {
			ctx.Logger().Info("test: Failed to set user from token")
			return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token: missing or invalid 'user' claim")
		}
	} else {
		ctx.Logger().Info("test: Failed to parse claims")
		return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token claims")
	}

	// If we've made it this far, the request is authenticated
	return next(ctx)
}
