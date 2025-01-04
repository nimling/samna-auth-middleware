package sauth

import (
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
)

type UserType string
type AuthError string

const (
	ErrorTokenExpired AuthError = "Token is expired"
	ErrorUnauthorized AuthError = "Unauthorized"
	ErrorNoCookie     AuthError = "No cookie"
	// These are for server access and not part of the access control itself
	USER_TYPE_ADMIN UserType = "admin" // A system user type with lower access than system itself
	USER_TYPE_USER  UserType = "user"  // The regular users, all users should be of this type
)

type UserDetail struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type TokenClaims struct {
	ID        uuid.UUID  `json:"jti,omitempty"`
	Subject   uuid.UUID  `json:"sub,omitempty"`
	User      *ClaimUser `json:"user,omitempty"`
	Audience  string     `json:"aud,omitempty"`
	ExpiresAt int64      `json:"exp,omitempty"`
	IssuedAt  int64      `json:"iat,omitempty"`
	Issuer    string     `json:"iss,omitempty"`
	NotBefore int64      `json:"nbf,omitempty"`
	jwt.StandardClaims
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

type BodyCode struct {
	Code *string `json:"code"`
}

type ResponseToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
