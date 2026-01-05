package hook

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	EventTypeCreate EventType = "create"
	EventTypeUpdate EventType = "update"
	EventTypeDelete EventType = "delete"
)

type EntityType string

const (
	EntityTypeOrganization EntityType = "organization"
	EntityTypeLocation     EntityType = "location"
	EntityTypeUser         EntityType = "user"
	EntityTypeClaim        EntityType = "claim"
	EntityTypeUserClaim    EntityType = "user-claim"
	EntityTypeObjectClaim  EntityType = "object-claim"
)

type Payload struct {
	EventType  EventType       `json:"event_type"`
	EntityType EntityType      `json:"entity_type"`
	Timestamp  time.Time       `json:"timestamp"`
	Data       json.RawMessage `json:"data"`
}

type User struct {
	ID        uuid.UUID  `json:"id"`
	FirstName string     `json:"first_name"`
	LastName  string     `json:"last_name"`
	UserName  string     `json:"user_name"`
	Phone     string     `json:"phone"`
	Email     string     `json:"email"`
	Location  string     `json:"location"`
	Status    string     `json:"status"`
	Type      string     `json:"type"`
	AssetID   *uuid.UUID `json:"asset_id,omitempty"`
	AssetURL  *string    `json:"asset_url,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type Organization struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty"`
	RootID      *uuid.UUID `json:"root_id,omitempty"`
	Type        *string    `json:"type,omitempty"`
	Level       int        `json:"level"`
	ExternalID  *string    `json:"external_id,omitempty"`
	ProviderID  *string    `json:"provider_id,omitempty"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Location struct {
	ID          uuid.UUID  `json:"id"`
	OwnerID     uuid.UUID  `json:"owner_id"`
	RootID      uuid.UUID  `json:"root_id,"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	ParentID    *uuid.UUID `json:"parent_id"`
	Type        string     `json:"type"`
	Longitude   *float64   `json:"longitude,omitempty"`
	Latitude    *float64   `json:"latitude,omitempty"`
	Level       int        `json:"level"`
	CodeType    *string    `json:"code_type,omitempty"`
	Code        *string    `json:"code,omitempty"`
	ExternalID  *string    `json:"external_id,omitempty"`
	ProviderID  *string    `json:"provider_id,omitempty"`
	CreatedBy   *uuid.UUID `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

type Claim struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	AccessLevel int        `json:"access_level"`
	Inherits    bool       `json:"inherits"`
	Type        string     `json:"type"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
	OwnerID     uuid.UUID  `json:"owner_id"`
	RootID      uuid.UUID  `json:"root_id"`
	DeletedAt   *time.Time `json:"deleted_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type UserClaim struct {
	ID        uuid.UUID  `json:"id"`
	ClaimID   uuid.UUID  `json:"claim_id"`
	UserID    uuid.UUID  `json:"user_id"`
	Reason    *string    `json:"reason,omitempty"`
	StartsAt  *time.Time `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at"`
	CreatedBy *uuid.UUID `json:"created_by"`
	OwnerID   uuid.UUID  `json:"owner_id"`
	DeletedAt *time.Time `json:"deleted_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type ClaimObject struct {
	ID         uuid.UUID  `json:"id"`
	Reason     *string    `json:"reason,omitempty"`
	ClaimID    uuid.UUID  `json:"claim_id"`
	ObjectID   uuid.UUID  `json:"object_id"`
	ObjectType string     `json:"object_type"`
	Level      int        `json:"level"`
	Inherits   bool       `json:"inherits"`
	RefID      *uuid.UUID `json:"ref_id,omitempty"`
	RootID     uuid.UUID  `json:"root_id"`
	CreatedBy  *uuid.UUID `json:"created_by"`
	OwnerID    uuid.UUID  `json:"owner_id"`
	DeletedAt  *time.Time `json:"deleted_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
