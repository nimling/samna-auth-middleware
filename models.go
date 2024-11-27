package samnamw

import "github.com/google/uuid"

type ClaimUser struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	UserName  string    `json:"user_name"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	Location  string    `json:"location"`
}
