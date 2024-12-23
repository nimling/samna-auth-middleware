package sauth

import (
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
)

type ClaimUser struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	UserName  string    `json:"user_name"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	Location  string    `json:"location"`
}

type Claims struct {
	UserID uuid.UUID `json:"sub"`
	Email  string    `json:"email"`
	jwt.StandardClaims
}

type BodyValidate struct {
	ClientID     uuid.UUID `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
}

type ResponseToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
