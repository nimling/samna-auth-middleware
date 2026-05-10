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

const authCodeParamKey = "code"

const (
	accessTokenCookieKey  = "sauth_access_token"
	refreshTokenCookieKey = "sauth_refresh_token"
	csrfTokenCookieKey    = "sauth_csrf_token"
)

type AuthError string

func (e AuthError) Error() string {
	return string(e)
}

const (
	ErrorTokenExpired      AuthError = "Token is expired"
	ErrorUnauthorized      AuthError = "Unauthorized"
	ErrorCredentialMissing AuthError = "Credential missing"
)

type UserType string

const (
	USER_TYPE_USER   UserType = "user"
	USER_TYPE_CLIENT UserType = "client"
	USER_TYPE_SYSTEM UserType = "system"
	USER_TYPE_GUEST  UserType = "guest"
)

type AuthMiddlewareConfig struct {
	ClientSlug          string
	ClientSecret        string
	TokenURL            url.URL
	JWKSURL             url.URL
	ExpectedTokenIssuer string
	RedirectURI         string
	PageRedirectURI     string
	OpenIDConfigURL     url.URL
	AuthorizedUserID    *uuid.UUID
	DebugClientID       *uuid.UUID
	DebugClientSecret   string
}

type UserAuthOptions struct {
	ClientSlug      string
	ClientSecret    string
	RedirectURI     string
	PageRedirectURI string
	DebugUserID     *uuid.UUID
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

