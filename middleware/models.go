package middleware

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type UserType string
type AuthError string

func (e AuthError) Error() string {
	return string(e)
}

const (
	ErrorTokenExpired AuthError = "Token is expired"
	ErrorUnauthorized AuthError = "Unauthorized"
	// These are for server access and not part of the access control itself
	USER_TYPE_ADMIN UserType = "admin" // A system user type with lower access than system itself
	USER_TYPE_USER  UserType = "user"  // The regular users, all users should be of this type
)

type UserDetail struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type TokenClaims struct {
	jwt.RegisteredClaims
}

// TODO:: for now only ID is set in token generation
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
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type ResponseClient struct {
	ClientID uuid.UUID `json:"client_id"`
	Valid    bool      `json:"valid"`
}
