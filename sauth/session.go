package sauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func (e AuthError) Error() string {
	return string(e)
}

// validateToken validates an access token
func validateToken(ctx echo.Context, clientSecret string, tokenString string) (*ClaimUser, error) {

	ctx.Logger().Info("Validating token")
	// Parse the JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		ctx.Logger().Info("Validating token with secret")
		// Note: In a production environment, you should validate the signing method
		// and use the appropriate key (symmetric or asymmetric)
		// TODO:: Validate origin here
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(clientSecret), nil // TODO:: Like with microsoft we do not validate the key here...need to figure out a way to do it though
	})

	// Check for errors other than signature
	if err != nil {
		if ve, ok := err.(*jwt.ValidationError); ok {
			if ve.Errors&jwt.ValidationErrorExpired != 0 {
				ctx.Logger().Info("Token is expired")
				return nil, ErrorTokenExpired
			}
		}
		if !strings.Contains(err.Error(), "signature is invalid") { //TODO:: This needs to be fixed
			ctx.Logger().Infof("Error parsing token: %v", err)
			return nil, err
		}
		token.Valid = true
	}

	if token == nil {
		ctx.Logger().Infof("Error parsing token: %v", err)
		return nil, err
	}

	//TODO:: We need to wait for expiration to not overload the server

	// Extract the 'sub' claim and set it as user_id in the context
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if sub, ok := claims["sub"].(string); ok {
			userID, err := uuid.Parse(sub)
			if err != nil {
				ctx.Logger().Infof("Invalid user ID: %s", err)
				return nil, err
			}
			ctx.Set("user_id", userID) // TODO:: remove and only keep the user object?
		} else {
			ctx.Logger().Info("Failed to set UserID from token")
			return nil, err
		}

		if userStr, ok := claims["user"].(string); ok {
			var user ClaimUser
			if err := json.Unmarshal([]byte(userStr), &user); err != nil {
				return nil, err
			}

			ctx.Set("user", user)
			return &user, nil
		} else {
			ctx.Logger().Info("Failed to set user from token")
			return nil, err
		}
	}

	ctx.Logger().Info("Failed to parse claims")
	return nil, err
}

// refreshTokens refreshes the access and refresh token
func refreshTokens(ctx echo.Context, authURL url.URL, clientID uuid.UUID, clientSecret string) (*ClaimUser, error) {
	// Try to get refresh token from cookie
	refreshToken, err := ctx.Cookie("refresh_token")
	if err != nil {
		ctx.Logger().Info("No refresh token found")
		return nil, err
	}

	tokens, err := performTokenRequest(ctx, authURL, clientID, clientSecret, nil, func(req *http.Request) error {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", refreshToken.Value))
		return nil
	})

	if err != nil {
		ctx.Logger().Info("Error performing token request")
		return nil, err
	}

	// Write cookies
	err = writeCookie(ctx, &http.Cookie{
		Name:   "access_token",
		Value:  tokens.AccessToken,
		Path:   "/",
		MaxAge: 3600, // TODO:: We need to get this from the server
	})

	if err != nil {
		ctx.Logger().Info("Error creating cookie")
		return nil, err
	}

	err = writeCookie(ctx, &http.Cookie{
		Name:   "refresh_token",
		Value:  tokens.RefreshToken,
		Path:   "/",     // TODO:: Set this to another specific path?
		MaxAge: 2592000, // TODO:: We need to get this from the server
	})

	if err != nil {
		ctx.Logger().Info("Error creating cookie")
		return nil, err
	}

	return validateToken(ctx, clientSecret, tokens.AccessToken)
}

// writeCookie writes a user client cookie
func writeCookie(ctx echo.Context, cookie *http.Cookie) error {

	cookie = &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
	}

	cookieDomain, ok := os.LookupEnv("SAMNA_AUTH_COOKIE_DOMAIN")
	if ok {
		if strings.Contains(cookieDomain, "localhost") {
			cookie.Secure = false
		}
		cookie.Domain = cookieDomain
	}

	ctx.SetCookie(cookie)
	return nil
}

// performTokenRequest performs a token refresh or initial request
func performTokenRequest(ctx echo.Context, authURL url.URL, clientID uuid.UUID, clientSecret string, body io.Reader, onRequest func(req *http.Request) error) (*ResponseToken, error) {

	// Verify with auth service
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, authURL.String(), body)
	if err != nil {
		ctx.Logger().Error("Failed to create a request to redirect login URL") //TODO:: Encrypt this a bit more?
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Samna-Client-Id", clientID.String())
	req.Header.Set("Samna-Client-Secret", clientSecret)

	if onRequest != nil {
		if err = onRequest(req); err != nil {
			ctx.Logger().Error("Failed to execute onRequest function")
			return nil, err
		}
	}

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		ctx.Logger().Infof("Failed to perform request : %s", err) //TODO:: Encrypt this a bit more?
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		ctx.Logger().Infof("Unauthorized: %d", resp.StatusCode) //TODO:: Encrypt this a bit more?
		return nil, ErrorUnauthorized
	}

	defer resp.Body.Close()

	// Parse response body
	var respBody ResponseToken
	if err = json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		ctx.Logger().Info("Failed to parse auth response body") //TODO:: Encrypt this a bit more?
		return nil, ErrorUnauthorized
	}

	//TODO:: This should be redirected to the original request performing the call
	return &respBody, nil
}

// exchangeAuthorizationCode redirects the request to the auth middleware
func exchangeAuthorizationCode(ctx echo.Context, next echo.HandlerFunc, authCode string, authURL url.URL, clientID uuid.UUID, clientSecret string) error {
	bodyCode := BodyCode{
		Code: &authCode,
	}

	jsonBody, err := json.Marshal(bodyCode)
	if err != nil {
		ctx.Logger().Error("Malformed validation body")
		return ctx.String(http.StatusInternalServerError, "Malformed validation body")
	}

	tokens, err := performTokenRequest(ctx, authURL, clientID, clientSecret, bytes.NewBuffer(jsonBody), nil)
	if err != nil {
		ctx.Logger().Infof("Error performing token request: %s", err)
		if errors.Is(err, ErrorUnauthorized) {
			return ctx.String(http.StatusUnauthorized, "Unauthorized")
		}
		return ctx.String(http.StatusInternalServerError, "Malformed validation body")
	}

	// Write cookies
	err = writeCookie(ctx, &http.Cookie{
		Name:   "access_token",
		Value:  tokens.AccessToken,
		Path:   "/",
		MaxAge: 3600, // TODO:: We need to get this from the server
	})

	if err != nil {
		ctx.Logger().Info("Error creating cookie")
		return ctx.NoContent(http.StatusInternalServerError)
	}

	err = writeCookie(ctx, &http.Cookie{
		Name:   "refresh_token",
		Value:  tokens.RefreshToken,
		Path:   "/",     // TODO:: Set this to another specific path?
		MaxAge: 2592000, // TODO:: We need to get this from the server
	})

	if err != nil {
		ctx.Logger().Info("Error creating cookie")
		return ctx.NoContent(http.StatusInternalServerError)
	}

	return next(ctx) //TODO:: This should be redirected to the original request performing the call
}
