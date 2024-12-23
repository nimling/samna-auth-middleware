package sauth

import (
	"encoding/json"
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"net/http"
)

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
		return ctx.NoContent(http.StatusUnauthorized)
	}

	//TODO:: We need to wait for expiration to not overload the server

	// Extract the 'sub' claim and set it as user_id in the context
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if sub, ok := claims["sub"].(string); ok {
			userID, err := uuid.Parse(sub)
			if err != nil {
				ctx.Logger().Error("test: Invalid user ID: %s", err)
				return ctx.NoContent(http.StatusUnauthorized)
			}
			ctx.Set("user_id", userID)
			ctx.Logger().Info("test: UserID sat from token: " + sub)
		} else {
			ctx.Logger().Info("test: Failed to set UserID from token")
			return ctx.NoContent(http.StatusUnauthorized)
		}

		if userStr, ok := claims["user"].(string); ok {
			var user ClaimUser
			if err := json.Unmarshal([]byte(userStr), &user); err != nil {
				return ctx.NoContent(http.StatusUnauthorized)
			}
			ctx.Set("user", user)
			ctx.Logger().Info("test: User sat from token: " + userStr)
		} else {
			ctx.Logger().Info("test: Failed to set user from token")
			return ctx.NoContent(http.StatusUnauthorized)
		}
	} else {
		ctx.Logger().Info("test: Failed to parse claims")
		return ctx.NoContent(http.StatusUnauthorized)
	}

	// If we've made it this far, the request is authenticated
	return next(ctx)
}

func writeCookie(ctx echo.Context, cookie *http.Cookie) error {
	ctx.SetCookie(&http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: true,
		Secure:   cookie.Secure,
		SameSite: http.SameSiteStrictMode,
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
	})
	return nil
}
