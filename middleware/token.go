package middleware

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func getAccessToken(ctx echo.Context) (string, error) {
	// Check if auth header is present
	authHeader := ctx.Request().Header.Get(echo.HeaderAuthorization)
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer "), nil
	}

	// Get the access token from the cookie
	cookie, err := ctx.Cookie(accessTokenCookieKey)
	if err != nil {
		return "", err
	}

	if cookie.Value == "" {
		return "", errors.New("access_token cookie is empty")
	}

	return cookie.Value, nil
}

func ValidateToken(tokenString, expectedIss string, keyFunc jwt.Keyfunc) (*ClaimUser, error) {

	var claims TokenClaims
	token, err := jwt.ParseWithClaims(tokenString, &claims, keyFunc)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrorTokenExpired
		}

		return nil, fmt.Errorf("parse token: %v", err)
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	if claims.Issuer != expectedIss {
		return nil, fmt.Errorf("unexpected token issuer: [%v], expected: [%v]", claims.Issuer, expectedIss)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("claims subject is not a valid UUID: %v", err)
	}

	user := &ClaimUser{
		ID: userID,
	}

	return user, nil
}

func claimsFromTokenUnsafe(token string) (*ClaimUser, error) {
	var claims TokenClaims
	_, _, err := new(jwt.Parser).ParseUnverified(token, &claims)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}

	return &ClaimUser{ID: userID}, nil
}
