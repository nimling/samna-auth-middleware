package middleware

import (
	"net/url"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	UserIDKey      = "user_id"
	AccessTokenKey = "access_token"
)

const authCodeParamKey = "sauth_auth_code"

const (
	accessTokenCookieKey  = "sauth_access_token"
	refreshTokenCookieKey = "sauth_refresh_token"
)

type AuthError string

func (e AuthError) Error() string {
	return string(e)
}

const (
	ErrorTokenExpired AuthError = "Token is expired"
	ErrorUnauthorized AuthError = "Unauthorized"
)

type UserType string

const (
	USER_TYPE_ADMIN UserType = "admin"
	USER_TYPE_USER  UserType = "user"
)

type AuthMiddlewareConfig struct {
	ClientSlug          string
	ClientSecret        string
	TokenURL            url.URL
	JWKSURL             url.URL
	ExpectedTokenIssuer string
	RedirectURI         string
	OpenIDConfigURL     url.URL
	AuthorizedUserID    *uuid.UUID
}

type AuthOptions struct {
	UserAuth   *UserAuthOptions
	ClientAuth *ClientAuthOptions
}

type UserAuthOptions struct {
	ClientSlug   string
	ClientSecret string
	RedirectURI  string
	DebugUserID  *uuid.UUID
}

type ClientAuthOptions struct {
}

type UserDetail struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type TokenClaims struct {
	jwt.RegisteredClaims
}

type ClaimUser struct {
	ID        uuid.UUID     `json:"id"`
	FirstName string        `json:"first_name"`
	LastName  string        `json:"last_name"`
	UserName  string        `json:"user_name"`
	Phone     string        `json:"phone"`
	Email     string        `json:"email"`
	Location  string        `json:"location"`
	Type      UserType      `json:"type"`
	Details   []*UserDetail `json:"details,omitempty"`
}

type ResponseToken struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	AccessExpiresIn  int    `json:"access_expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	TokenType        string `json:"token_type"`
}

type openIDConfiguration struct {
	Issuer        string `json:"issuer"`
	TokenEndpoint string `json:"token_endpoint"`
	JwksUri       string `json:"jwks_uri"`
}
