package client

import (
	"time"

	"github.com/google/uuid"
)

type RequestMetadata struct {
	AccessToken string
	// These can be very helpful for log filtering / traceability
	RequestID string
	UserIP    string
}

type UpdateUserBody struct {
	FirstName *string    `json:"first_name"`
	LastName  *string    `json:"last_name"`
	UserName  *string    `json:"user_name"`
	Phone     *string    `json:"phone"`
	Email     *string    `json:"email"`
	Location  *string    `json:"location"`
	AssetID   *uuid.UUID `json:"asset_id"`
	AssetURL  *string    `json:"asset_url"`
}

type CreateOrganizationBody struct {
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	ParentID    *uuid.UUID `json:"parent_id"`
	RootID      *uuid.UUID `json:"root_id"`
	Type        *string    `json:"type"`
	Level       *int       `json:"level"`
	ExternalID  *string    `json:"external_id"`
	ProviderID  *string    `json:"provider_id"`
}

type UpdateOrganizationBody struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	ExternalID  *string `json:"external_id"`
	ProviderID  *string `json:"provider_id"`
}

type CreateLocationBody struct {
	OwnerID     *uuid.UUID `json:"owner_id"`
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	ParentID    *uuid.UUID `json:"parent_id"`
	Type        *string    `json:"type"`
	Longitude   *float64   `json:"longitude"`
	Latitude    *float64   `json:"latitude"`
	Level       *int       `json:"level"`
	CodeType    *string    `json:"code_type"`
	Code        *string    `json:"code"`
	ExternalID  *string    `json:"external_id"`
	ProviderID  *string    `json:"provider_id"`
}

type UpdateLocationBody struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Type        *string  `json:"type"`
	Longitude   *float64 `json:"longitude"`
	Latitude    *float64 `json:"latitude"`
	CodeType    *string  `json:"code_type"`
	Code        *string  `json:"code"`
	ExternalID  *string  `json:"external_id"`
	ProviderID  *string  `json:"provider_id"`
}

type ClaimAccessLevel int

const (
	// ACCESS_LEVEL_SYSTEM System only operations, utility stuffs
	ACCESS_LEVEL_SYSTEM ClaimAccessLevel = 0
	// ACCESS_LEVEL_OWNER The top most access level, this should only be handed out to actual owners and used with care
	ACCESS_LEVEL_OWNER ClaimAccessLevel = 1
	// ACCESS_LEVEL_ADMIN The second access level grants update, create, and destructive actions. Can be sat for all objects but only and Owner can delete an organization
	ACCESS_LEVEL_ADMIN ClaimAccessLevel = 2
	// ACCESS_LEVEL_WRITE The third access level grants update, and possible create, but no destructive actions except for objects created by the user itself.
	ACCESS_LEVEL_WRITE ClaimAccessLevel = 3
	// ACCESS_LEVEL_UPDATE The fourth access level grants update access for a server object with inheritance
	ACCESS_LEVEL_UPDATE ClaimAccessLevel = 4
	// ACCESS_LEVEL_ACTION The fifth access level grants read and allows the user to take relevant actions on the object like book.
	ACCESS_LEVEL_ACTION ClaimAccessLevel = 5
	// ACCESS_LEVEL_READ The sixth access level is the classic read, no cud is allowed.
	ACCESS_LEVEL_READ ClaimAccessLevel = 6
	// ACCESS_LEVEL_MEMBER The seventh access level is used to grant a user inclusion into a claim, but not allow for any read.
	ACCESS_LEVEL_MEMBER ClaimAccessLevel = 7
	// ACCESS_LEVEL_GUEST The eight access level is the same as sixth level but ear marked for externals, or temporary users.
	ACCESS_LEVEL_GUEST ClaimAccessLevel = 8
)

type ClaimType string

const (
	CLAIM_TYPE_SYSTEM ClaimType = "system"
	CLAIM_TYPE_USER   ClaimType = "user"
	CLAIM_TYPE_SAT    ClaimType = "sat"
)

type CreateClaimBody struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	AccessLevel *ClaimAccessLevel `json:"access_level"`
	Type        *ClaimType        `json:"type"`
	OwnerID     *uuid.UUID        `json:"owner_id"`
}

type UpdateClaimBody struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	AccessLevel *ClaimAccessLevel `json:"access_level"`
}

type ObjectType string

const (
	OBJECT_TYPE_USER         ObjectType = "user"
	OBJECT_TYPE_ORGANIZATION ObjectType = "organization"
	OBJECT_TYPE_LOCATION     ObjectType = "location"
)

type CreateClaimObjectBody struct {
	Reason     *string     `json:"reason"`
	ObjectID   *uuid.UUID  `json:"object_id"`
	ObjectType *ObjectType `json:"object_type"`
	Level      *int        `json:"level"`
	Inherits   *bool       `json:"inherits"`
	RefID      *uuid.UUID  `json:"ref_id"`
}

type CreateClaimUserBody struct {
	UserID   *uuid.UUID `json:"user_id"`
	Reason   *string    `json:"reason"`
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
}

type Asset struct {
	AssetID  uuid.UUID `json:"id"`
	AssetURL string    `json:"url"`
}
