package db

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type ClaimAccessLevel int

const (
	ACCESS_LEVEL_SYSTEM ClaimAccessLevel = 0
	ACCESS_LEVEL_OWNER  ClaimAccessLevel = 1
	ACCESS_LEVEL_ADMIN  ClaimAccessLevel = 2
	ACCESS_LEVEL_WRITE  ClaimAccessLevel = 3
	ACCESS_LEVEL_UPDATE ClaimAccessLevel = 4
	ACCESS_LEVEL_ACTION ClaimAccessLevel = 5
	ACCESS_LEVEL_READ   ClaimAccessLevel = 6
	ACCESS_LEVEL_MEMBER ClaimAccessLevel = 7
	ACCESS_LEVEL_GUEST  ClaimAccessLevel = 8
)

type ClaimType string

const (
	CLAIM_TYPE_SYSTEM  ClaimType = "system"
	CLAIM_TYPE_USER    ClaimType = "user"
	CLAIM_TYPE_SERVICE ClaimType = "service"
)

type ObjectType string

const (
	OBJECT_TYPE_USER         ObjectType = "user"
	OBJECT_TYPE_ORGANIZATION ObjectType = "organization"
	OBJECT_TYPE_LOCATION     ObjectType = "location"
)

type SamnaUser struct {
	bun.BaseModel `bun:"table:samna_auth.samna_user"`

	ID        uuid.UUID        `bun:"id,pk" json:"id"`
	FirstName *string          `bun:"first_name" json:"first_name"`
	LastName  *string          `bun:"last_name" json:"last_name"`
	Phone     *string          `bun:"phone" json:"phone"`
	Email     string           `bun:"email" json:"email"`
	UserID    uuid.UUID        `bun:"user_id" json:"user_id"`
	Status    *string          `bun:"status" json:"status"`
	Type      *string          `bun:"type" json:"type"`
	Config    *json.RawMessage `bun:"config" json:"config"`
	DeletedAt *time.Time       `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt time.Time        `bun:"sa_created_at" json:"created_at"`
	UpdatedAt time.Time        `bun:"sa_updated_at" json:"updated_at"`
}

type Organization struct {
	bun.BaseModel `bun:"table:samna_auth.organization,alias:o"`

	ID          uuid.UUID  `bun:"id,pk" json:"id"`
	Name        string     `bun:"name" json:"name"`
	Description *string    `bun:"description" json:"description"`
	ParentID    *uuid.UUID `bun:"sa_parent_id" json:"parent_id"`
	RootID      *uuid.UUID `bun:"sa_root_id" json:"root_id"`
	OwnerID     *uuid.UUID `bun:"sa_owner_id" json:"owner_id"`
	Type        *string    `bun:"type" json:"type"`
	Level       int        `bun:"sa_level" json:"level"`
	ExternalID  *string    `bun:"external_id" json:"external_id"`
	ProviderID  *uuid.UUID `bun:"provider_id" json:"provider_id"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedBy   uuid.UUID  `bun:"sa_created_by" json:"created_by"`
	CreatedAt   time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type Location struct {
	bun.BaseModel `bun:"table:samna_auth.location,alias:l"`

	ID          uuid.UUID  `bun:"id,pk" json:"id"`
	OwnerID     uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	RootID      uuid.UUID  `bun:"sa_root_id" json:"root_id"`
	Name        *string    `bun:"name" json:"name"`
	Description *string    `bun:"description" json:"description"`
	ParentID    *uuid.UUID `bun:"sa_parent_id" json:"parent_id"`
	Type        string     `bun:"type" json:"type"`
	Longitude   *float64   `bun:"longitude" json:"longitude"`
	Latitude    *float64   `bun:"latitude" json:"latitude"`
	Level       int        `bun:"sa_level" json:"level"`
	CodeType    *string    `bun:"code_type" json:"code_type"`
	Code        *string    `bun:"code" json:"code"`
	ExternalID  *string    `bun:"external_id" json:"external_id"`
	ProviderID  *uuid.UUID `bun:"provider_id" json:"provider_id"`
	CreatedBy   uuid.UUID  `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type Claim struct {
	bun.BaseModel `bun:"table:samna_auth.claim"`

	ID          uuid.UUID  `bun:"id,pk" json:"id"`
	Name        string     `bun:"name" json:"name"`
	Description *string    `bun:"description" json:"description"`
	AccessLevel int        `bun:"sa_access_level" json:"access_level"`
	Inherits    bool       `bun:"inherits" json:"inherits"`
	Type        ClaimType  `bun:"type" json:"type"`
	CreatedBy   *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	OwnerID     uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	RootID      uuid.UUID  `bun:"sa_root_id" json:"root_id"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type UserClaim struct {
	bun.BaseModel `bun:"table:samna_auth.user_claim"`

	ID        uuid.UUID  `bun:"id,pk" json:"id"`
	ClaimID   uuid.UUID  `bun:"claim_id" json:"claim_id"`
	UserID    uuid.UUID  `bun:"user_id" json:"user_id"`
	Reason    *string    `bun:"reason" json:"reason"`
	StartsAt  *time.Time `bun:"starts_at" json:"starts_at"`
	EndsAt    *time.Time `bun:"ends_at" json:"ends_at"`
	CreatedBy *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	OwnerID   uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	DeletedAt *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type ClaimObject struct {
	bun.BaseModel `bun:"table:samna_auth.claim_object"`

	ID         uuid.UUID  `bun:"id,pk" json:"id"`
	Reason     *string    `bun:"reason" json:"reason"`
	Function   *string    `bun:"function" json:"function"`
	ClaimID    uuid.UUID  `bun:"claim_id" json:"claim_id"`
	ObjectID   uuid.UUID  `bun:"object_id" json:"object_id"`
	ObjectType string     `bun:"object_type" json:"object_type"`
	Level      int        `bun:"sa_level" json:"level"`
	Inherits   bool       `bun:"inherits" json:"inherits"`
	RefID      *uuid.UUID `bun:"ref_id" json:"ref_id"`
	RootID     uuid.UUID  `bun:"sa_root_id" json:"root_id"`
	CreatedBy  *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	OwnerID    uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	DeletedAt  *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt  time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt  time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type ActiveClaim struct {
	bun.BaseModel `bun:"table:samna_auth.active_claim"`

	ID               uuid.UUID     `bun:"id,pk" json:"id"`
	ClaimID          uuid.UUID     `bun:"claim_id" json:"claim_id"`
	UserID           uuid.UUID     `bun:"user_id" json:"user_id"`
	ClaimName        string        `bun:"claim_name" json:"claim_name"`
	ClaimDescription *string       `bun:"claim_description" json:"claim_description"`
	AccessLevel      int           `bun:"sa_access_level" json:"access_level"`
	ClaimType        string        `bun:"claim_type" json:"claim_type"`
	CreatedBy        *uuid.UUID    `bun:"sa_created_by" json:"created_by"`
	ClaimCreatedBy   *uuid.UUID    `bun:"claim_sa_created_by" json:"claim_created_by"`
	ClaimOwnerID     uuid.UUID     `bun:"claim_sa_owner_id" json:"claim_owner_id"`
	UserOwnerID      uuid.UUID     `bun:"user_sa_owner_id" json:"user_owner_id"`
	RootID           uuid.UUID     `bun:"sa_root_id" json:"root_id"`
	Objects          []ClaimObject `bun:"rel:has-many,join:claim_id=claim_id" json:"claim_objects"`
}

type ActiveObject struct {
	ObjectID   uuid.UUID  `bun:"object_id" json:"object_id"`
	ObjectType string     `bun:"object_type" json:"object_type"`
	ClaimID    uuid.UUID  `bun:"claim_id" json:"claim_id"`
	Level      int        `bun:"sa_level" json:"level"`
	Inherits   bool       `bun:"inherits" json:"inherits"`
	RootID     uuid.UUID  `bun:"sa_root_id" json:"root_id"`
	OwnerID    uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	RefID      *uuid.UUID `bun:"ref_id" json:"ref_id"`
	Reason     *string    `bun:"reason" json:"reason"`
	Function   *string    `bun:"function" json:"function"`
	CreatedAt  time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt  time.Time  `bun:"sa_updated_at" json:"updated_at"`
	DeletedAt  *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
}

type ActiveUser struct {
	TargetUserID uuid.UUID  `bun:"target_user_id" json:"target_user_id"`
	UserID       uuid.UUID  `bun:"user_id" json:"user_id"`
	ClaimID      uuid.UUID  `bun:"claim_id" json:"claim_id"`
	OwnerID      uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	AccessLevel  int        `bun:"sa_access_level" json:"access_level"`
	CreatedAt    time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt    time.Time  `bun:"sa_updated_at" json:"updated_at"`
	DeletedAt    *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
}

type Asset struct {
	bun.BaseModel `bun:"table:Asset,alias:asset" json:"-"`

	ID          uuid.UUID  `bun:"id,pk" json:"id"`
	Name        string     `bun:"column:name" json:"name"`
	Description *string    `bun:"column:description" json:"description"`
	MimeType    string     `bun:"column:mime_type" json:"mime_type"`
	UserID      uuid.UUID  `bun:"column:user_id" json:"user_id"`
	Status      string     `bun:"column:status" json:"status"`
	Index       int        `bun:"column:index" json:"index"`
	BlobURL     *string    `bun:"blob_url,default:null" json:"-"`
	AssetURL    string     `bun:"-" json:"asset_url,omitempty"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type QueryAccess struct {
	UserID uuid.UUID
}
