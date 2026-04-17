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
	CLAIM_TYPE_VIRTUAL ClaimType = "virtual"
)

const (
	OBJECT_TYPE_USER         = "user"
	OBJECT_TYPE_ORGANIZATION = "organization"
	OBJECT_TYPE_LOCATION     = "location"
	OBJECT_TYPE_CLAIM        = "claim"
	OBJECT_TYPE_USER_CLAIM   = "user_claim"
	OBJECT_TYPE_CLAIM_OBJECT = "claim_object"
)

type OrganizationType string

const (
	ORGANIZATION_TYPE_SYSTEM  OrganizationType = "system"
	ORGANIZATION_TYPE_COMPANY OrganizationType = "company"
	ORGANIZATION_TYPE_CHILD   OrganizationType = "child"
)

type LocationType string

const (
	LOCATION_TYPE_USER     LocationType = "user"
	LOCATION_TYPE_LOCATION LocationType = "location"
	LOCATION_TYPE_BOOKABLE LocationType = "bookable"
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
	bun.BaseModel `bun:"table:samna_auth.organization"`

	ID          uuid.UUID        `bun:"id,pk" json:"id"`
	Name        string           `bun:"name" json:"name"`
	Description *string          `bun:"description" json:"description"`
	RootID      *uuid.UUID       `bun:"sa_root_id" json:"root_id"`
	OwnerID     *uuid.UUID       `bun:"sa_owner_id" json:"owner_id"`
	Type        OrganizationType `bun:"type" json:"type"`
	Level       int        `bun:"sa_level" json:"level"`
	ExternalID  *string    `bun:"external_id" json:"external_id"`
	ProviderID  *uuid.UUID `bun:"provider_id" json:"provider_id"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedBy   uuid.UUID     `bun:"sa_created_by" json:"created_by"`
	CreatedAt   time.Time     `bun:"sa_created_at" json:"created_at"`
	UpdatedAt   time.Time     `bun:"sa_updated_at" json:"updated_at"`
	Parent      *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"parent,omitempty"`
}

type Location struct {
	bun.BaseModel `bun:"table:samna_auth.location"`

	ID          uuid.UUID  `bun:"id,pk" json:"id"`
	OwnerID     uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	RootID      uuid.UUID  `bun:"sa_root_id" json:"root_id"`
	Name        *string    `bun:"name" json:"name"`
	Description *string    `bun:"description" json:"description"`
	ParentID    *uuid.UUID   `bun:"sa_parent_id" json:"parent_id"`
	Type        LocationType `bun:"type" json:"type"`
	Longitude   *float64   `bun:"longitude" json:"longitude"`
	Latitude    *float64   `bun:"latitude" json:"latitude"`
	Level       int        `bun:"sa_level" json:"level"`
	CodeType    *string    `bun:"code_type" json:"code_type"`
	Code        *string    `bun:"code" json:"code"`
	ExternalID  *string    `bun:"external_id" json:"external_id"`
	ProviderID  *uuid.UUID `bun:"provider_id" json:"provider_id"`
	CreatedBy   uuid.UUID     `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time    `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time     `bun:"sa_created_at" json:"created_at"`
	UpdatedAt   time.Time     `bun:"sa_updated_at" json:"updated_at"`
	Owner       *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
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
	DeletedAt    *time.Time    `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt    time.Time     `bun:"sa_created_at" json:"created_at"`
	UpdatedAt    time.Time     `bun:"sa_updated_at" json:"updated_at"`
	ClaimObjects []ClaimObject `bun:"rel:has-many,join:id=claim_id" json:"claim_objects,omitempty"`
	ClaimUsers   []UserClaim   `bun:"rel:has-many,join:id=claim_id" json:"claim_users,omitempty"`
	Owner        *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
}

type UserClaim struct {
	bun.BaseModel `bun:"table:samna_auth.user_claim"`

	ID        uuid.UUID  `bun:"id,pk" json:"id"`
	ClaimID   uuid.UUID  `bun:"claim_id" json:"claim_id"`
	UserID    uuid.UUID  `bun:"user_id" json:"user_id"`
	Reason    *string    `bun:"reason" json:"reason"`
	StartsAt  *time.Time `bun:"starts_at" json:"starts_at"`
	EndsAt    *time.Time `bun:"ends_at" json:"ends_at"`
	CreatedBy *uuid.UUID    `bun:"sa_created_by" json:"created_by"`
	OwnerID   uuid.UUID     `bun:"sa_owner_id" json:"owner_id"`
	DeletedAt *time.Time    `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt time.Time     `bun:"sa_created_at" json:"created_at"`
	UpdatedAt time.Time     `bun:"sa_updated_at" json:"updated_at"`
	Owner     *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
	User      *SamnaUser    `bun:"rel:belongs-to,join:user_id=user_id" json:"user,omitempty"`
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
	CreatedBy  *uuid.UUID    `bun:"sa_created_by" json:"created_by"`
	OwnerID    uuid.UUID     `bun:"sa_owner_id" json:"owner_id"`
	DeletedAt  *time.Time    `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt  time.Time     `bun:"sa_created_at" json:"created_at"`
	UpdatedAt  time.Time     `bun:"sa_updated_at" json:"updated_at"`
	Owner      *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
}

type ActiveClaim struct {

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

type Asset struct {
	bun.BaseModel `bun:"table:asset" json:"-"`

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

// GraphNode represents a single node in an ownership or claim graph.
// Each node carries a typed label and arbitrary JSONB data payload.
type GraphNode struct {
	ID    uuid.UUID        `bun:"id" json:"id"`
	Label string           `bun:"label" json:"label"`
	Type  string           `bun:"type" json:"type"`
	Data  json.RawMessage  `bun:"data,type:jsonb" json:"data"`
}

type SamnaClient struct {
	bun.BaseModel `bun:"table:samna_auth.samna_client"`

	ID         uuid.UUID  `bun:"id,pk" json:"id"`
	Name       string     `bun:"name" json:"name"`
	Description *string   `bun:"description" json:"description"`
	AppID      uuid.UUID  `bun:"app_id" json:"app_id"`
	ClientKey  uuid.UUID  `bun:"client_key" json:"client_key"`
	SecretHash string     `bun:"secret_hash" json:"-"`
	Status     string     `bun:"status" json:"status"`
	ExpiresAt  *time.Time `bun:"expires_at" json:"expires_at"`
	OwnerID    uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy  *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	DeletedAt  *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt  time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt  time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type ObjectField struct {
	bun.BaseModel `bun:"table:samna_auth.object_field"`

	ID          uuid.UUID        `bun:"id,pk" json:"id"`
	AppID       uuid.UUID        `bun:"app_id" json:"app_id"`
	ObjectType  string           `bun:"object_type" json:"object_type"`
	Name        string           `bun:"name" json:"name"`
	Description *string          `bun:"description" json:"description"`
	JsonSchema  *json.RawMessage `bun:"json_schema,type:jsonb" json:"json_schema"`
	Required    bool             `bun:"required" json:"required"`
	Value       *string          `bun:"value" json:"value"`
	OwnerID     *uuid.UUID       `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy   *uuid.UUID       `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time       `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time        `bun:"sa_created_at" json:"created_at"`
	UpdatedAt   time.Time        `bun:"sa_updated_at" json:"updated_at"`
}

type UserField struct {
	bun.BaseModel `bun:"table:samna_auth.user_field"`

	ID        uuid.UUID  `bun:"id,pk" json:"id"`
	FieldID   uuid.UUID  `bun:"field_id" json:"field_id"`
	UserID    uuid.UUID  `bun:"user_id" json:"user_id"`
	Value     *string    `bun:"value" json:"value"`
	OwnerID   *uuid.UUID `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	DeletedAt *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type UserRelation struct {
	bun.BaseModel `bun:"table:user_relation" json:"-"`

	ID          uuid.UUID  `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	UserID      uuid.UUID  `bun:"user_id,notnull,type:uuid" json:"user_id"`
	AppID       *uuid.UUID `bun:"app_id,type:uuid" json:"app_id"`
	Description *string    `bun:"description" json:"description"`
	ObjectType  string     `bun:"column:object_type,type:string,notnull" json:"object_type"`
	ObjectID    uuid.UUID  `bun:"object_id,notnull,type:uuid" json:"object_id"`
	Pinned      int        `bun:"pinned" json:"pinned"`
	Priority    int        `bun:"priority" json:"priority"`
	ParentID    *uuid.UUID `bun:"sa_parent_id,type:uuid,default:null" json:"parent_id"`
	SiblingID   *uuid.UUID `bun:"sibling_id,type:uuid,default:null" json:"sibling_id"`
	UsedAt      *time.Time `bun:"used_at,default:current_timestamp" json:"used_at"`
	DeletedAt   *time.Time `bun:"sa_deleted_at,default:null" json:"deleted_at,omitempty"`
	CreatedAt   time.Time  `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
}

type Audit struct {
	bun.BaseModel `bun:"table:samna_auth.audit"`

	ID         uuid.UUID `bun:"id,pk" json:"id"`
	Operation  string    `bun:"operation" json:"operation"`
	Message    string    `bun:"message" json:"message"`
	Type       string    `bun:"type" json:"type"`
	ObjectType string    `bun:"object_type" json:"object_type"`
	ObjectID   uuid.UUID `bun:"object_id" json:"object_id"`
	CreatedBy  uuid.UUID `bun:"sa_created_by" json:"created_by"`
	OwnerID    uuid.UUID `bun:"sa_owner_id" json:"owner_id"`
	CreatedAt  time.Time `bun:"sa_created_at" json:"created_at"`
	UpdatedAt  time.Time `bun:"sa_updated_at" json:"updated_at"`
}

type UserObject struct {
	bun.BaseModel `bun:"table:samna_auth.user_object"`

	ID            string     `bun:"id,pk" json:"id"`
	UserID        uuid.UUID  `bun:"user_id" json:"user_id"`
	ObjectID      uuid.UUID  `bun:"object_id" json:"object_id"`
	ObjectType    string     `bun:"object_type" json:"object_type"`
	ClaimID       uuid.UUID  `bun:"claim_id" json:"claim_id"`
	AccessLevel   int        `bun:"sa_access_level" json:"sa_access_level"`
	OwnerID       *uuid.UUID `bun:"sa_owner_id" json:"sa_owner_id"`
	LocationID    *uuid.UUID `bun:"sa_location_id" json:"sa_location_id"`
	Name          *string    `bun:"sa_name" json:"sa_name"`
	Description   *string    `bun:"sa_description" json:"sa_description"`
	Link          *string    `bun:"sa_link" json:"sa_link"`
	CreatedAt     time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt     time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type UserUsers struct {
	bun.BaseModel    `bun:"table:samna_auth.user_users"`

	ID               string    `bun:"id,pk" json:"id"`
	ViewerID         uuid.UUID `bun:"viewer_id" json:"viewer_id"`
	TargetUserID     uuid.UUID `bun:"target_user_id" json:"target_user_id"`
	FirstSharedAt    time.Time `bun:"first_shared_at" json:"first_shared_at"`
	LastSharedAt     time.Time `bun:"last_shared_at" json:"last_shared_at"`
	SharingObjCount  int       `bun:"sharing_object_count" json:"sharing_object_count"`
}

type ObjectUsers struct {
	bun.BaseModel `bun:"table:samna_auth.object_users"`

	ID         string    `bun:"id,pk" json:"id"`
	ObjectID   uuid.UUID `bun:"object_id" json:"object_id"`
	ObjectType string    `bun:"object_type" json:"object_type"`
	UserID     uuid.UUID `bun:"user_id" json:"user_id"`
}

type UserObjectEvent struct {
	bun.BaseModel `bun:"table:samna_auth.user_object_event"`

	Seq        int64           `bun:"seq,pk" json:"seq"`
	ObjectType string          `bun:"object_type" json:"object_type"`
	Obj        json.RawMessage `bun:"obj,type:jsonb" json:"obj"`
	WrittenAt  time.Time       `bun:"written_at" json:"written_at"`
}

type ClientState struct {
	bun.BaseModel  `bun:"table:samna_auth.client_state"`

	ID             int       `bun:"id,pk" json:"id"`
	LastAppliedSeq int64     `bun:"last_applied_seq" json:"last_applied_seq"`
	UpdatedAt      time.Time `bun:"sa_updated_at" json:"updated_at"`
}
