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
	OBJECT_TYPE_USER         = "claimius.samna_user"
	OBJECT_TYPE_ORGANIZATION = "claimius.organization"
	OBJECT_TYPE_LOCATION     = "claimius.location"
	OBJECT_TYPE_CLAIM        = "claimius.claim"
	OBJECT_TYPE_USER_CLAIM   = "claimius.user_claim"
	OBJECT_TYPE_CLAIM_OBJECT = "claimius.claim_object"
	OBJECT_TYPE_SAMNA_APP    = "claimius.samna_app"
	OBJECT_TYPE_SAMNA_CLIENT = "claimius.samna_client"
	OBJECT_TYPE_SAMNA_SECRET = "claimius.samna_secret"
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
	bun.BaseModel `bun:"table:claimius.samna_user"`

	ID        uuid.UUID        `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	UserID    uuid.UUID        `bun:"user_id,default:gen_random_uuid()" json:"user_id"`
	AppID     uuid.UUID        `bun:"app_id" json:"-"`
	FirstName *string          `bun:"first_name" json:"first_name"`
	LastName  *string          `bun:"last_name" json:"last_name"`
	UserName  *string          `bun:"user_name" json:"user_name"`
	UserImage *string          `bun:"user_image" json:"user_image"`
	Phone     *string          `bun:"phone" json:"phone"`
	Email     string           `bun:"email" json:"email"`
	Status    *string          `bun:"status,default:'active'" json:"status"`
	Type      *string          `bun:"type,default:'user'" json:"type"`
	Config    *json.RawMessage `bun:"config" json:"config"`
	DeletedAt *time.Time       `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type Organization struct {
	bun.BaseModel `bun:"table:claimius.organization"`

	ID          uuid.UUID        `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID        `bun:"app_id" json:"-"`
	Name        string           `bun:"name" json:"name"`
	Description *string          `bun:"description" json:"description"`
	Type        OrganizationType `bun:"type" json:"type"`
	OwnerID     uuid.UUID        `bun:"sa_owner_id" json:"owner_id"`
	RootID      uuid.UUID        `bun:"sa_root_id" json:"root_id"`
	LocationID  *uuid.UUID       `bun:"sa_location_id" json:"location_id"`
	Level       int              `bun:"sa_level,default:0" json:"level"`
	ExternalID  *string          `bun:"external_id" json:"external_id"`
	ProviderID  *uuid.UUID       `bun:"provider_id" json:"provider_id"`
	CreatedBy   uuid.UUID        `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time       `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
	Parent      *Organization    `bun:"rel:belongs-to,join:sa_owner_id=id" json:"parent,omitempty"`
}

type Location struct {
	bun.BaseModel `bun:"table:claimius.location"`

	ID          uuid.UUID     `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID     `bun:"app_id" json:"-"`
	OwnerID     uuid.UUID     `bun:"sa_owner_id" json:"owner_id"`
	ParentID    *uuid.UUID    `bun:"sa_parent_id" json:"parent_id"`
	RootID      uuid.UUID     `bun:"sa_root_id" json:"root_id"`
	Name        *string       `bun:"name" json:"name"`
	Description *string       `bun:"description" json:"description"`
	Type        LocationType  `bun:"type,default:'room'" json:"type"`
	Longitude   *float64      `bun:"longitude" json:"longitude"`
	Latitude    *float64      `bun:"latitude" json:"latitude"`
	Level       int           `bun:"sa_level,default:0" json:"level"`
	CodeType    *string       `bun:"code_type" json:"code_type"`
	Code        *string       `bun:"code" json:"code"`
	ExternalID  *string       `bun:"external_id" json:"external_id"`
	ProviderID  *uuid.UUID    `bun:"provider_id" json:"provider_id"`
	CreatedBy   uuid.UUID     `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time    `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time     `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time     `bun:"sa_updated_at,default:now()" json:"updated_at"`
	Owner       *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
}

type Claim struct {
	bun.BaseModel `bun:"table:claimius.claim"`

	ID           uuid.UUID     `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID        uuid.UUID     `bun:"app_id" json:"-"`
	Name         string        `bun:"name" json:"name"`
	Description  *string       `bun:"description" json:"description"`
	AccessLevel  int           `bun:"sa_access_level" json:"access_level"`
	Inherits     bool          `bun:"inherits,default:true" json:"inherits"`
	IsDeny       bool          `bun:"is_deny,default:false" json:"is_deny"`
	Type         ClaimType     `bun:"type,default:'user'" json:"type"`
	OwnerID      uuid.UUID     `bun:"sa_owner_id" json:"owner_id"`
	RootID       uuid.UUID     `bun:"sa_root_id" json:"root_id"`
	CreatedBy    *uuid.UUID    `bun:"sa_created_by" json:"created_by"`
	DeletedAt    *time.Time    `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt    time.Time     `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt    time.Time     `bun:"sa_updated_at,default:now()" json:"updated_at"`
	ClaimObjects []ClaimObject `bun:"rel:has-many,join:id=claim_id" json:"claim_objects,omitempty"`
	ClaimUsers   []UserClaim   `bun:"rel:has-many,join:id=claim_id" json:"claim_users,omitempty"`
	Owner        *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
}

type UserClaim struct {
	bun.BaseModel `bun:"table:claimius.user_claim"`

	ID        uuid.UUID     `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID     uuid.UUID     `bun:"app_id" json:"-"`
	ClaimID   uuid.UUID     `bun:"claim_id" json:"claim_id"`
	UserID    uuid.UUID     `bun:"user_id" json:"user_id"`
	Reason    *string       `bun:"reason" json:"reason"`
	StartsAt  *time.Time    `bun:"starts_at" json:"starts_at"`
	EndsAt    *time.Time    `bun:"ends_at" json:"ends_at"`
	OwnerID   uuid.UUID     `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy uuid.UUID     `bun:"sa_created_by" json:"created_by"`
	DeletedAt *time.Time    `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt time.Time     `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt time.Time     `bun:"sa_updated_at,default:now()" json:"updated_at"`
	Owner     *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
	User      *SamnaUser    `bun:"rel:belongs-to,join:user_id=user_id" json:"user,omitempty"`
}

type ClaimObject struct {
	bun.BaseModel `bun:"table:claimius.claim_object"`

	ID         uuid.UUID     `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID      uuid.UUID     `bun:"app_id" json:"-"`
	ClaimID    uuid.UUID     `bun:"claim_id" json:"claim_id"`
	ObjectID   uuid.UUID     `bun:"object_id" json:"object_id"`
	ObjectType string        `bun:"object_type" json:"object_type"`
	Reason     *string       `bun:"reason" json:"reason"`
	Function   *string       `bun:"function" json:"function"`
	Level      int           `bun:"sa_level,default:0" json:"level"`
	Inherits   bool          `bun:"inherits,default:true" json:"inherits"`
	RefID      *uuid.UUID    `bun:"ref_id" json:"ref_id"`
	OwnerID    uuid.UUID     `bun:"sa_owner_id" json:"owner_id"`
	RootID     uuid.UUID     `bun:"sa_root_id" json:"root_id"`
	CreatedBy  *uuid.UUID    `bun:"sa_created_by" json:"created_by"`
	DeletedAt  *time.Time    `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt  time.Time     `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt  time.Time     `bun:"sa_updated_at,default:now()" json:"updated_at"`
	Owner      *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
}

type AccessClaim struct {
	UserObjectID string     `bun:"user_object_id" json:"user_object_id"`
	UserClaimID  *uuid.UUID `bun:"user_claim_id" json:"user_claim_id"`
	ClaimID      *uuid.UUID `bun:"claim_id" json:"claim_id"`
	AccessLevel  int        `bun:"sa_access_level" json:"access_level"`
	OwnerID      uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	RootID       uuid.UUID  `bun:"sa_root_id" json:"root_id"`
	LocationID   *uuid.UUID `bun:"sa_location_id" json:"location_id"`
	DirectGrant  bool       `bun:"direct_grant" json:"direct_grant"`
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

type GraphNode struct {
	ObjectID   uuid.UUID       `bun:"object_id" json:"object_id"`
	ObjectType string          `bun:"object_type" json:"object_type"`
	Label      string          `bun:"label" json:"label"`
	ParentID   *uuid.UUID      `bun:"parent_id" json:"parent_id"`
	Data       json.RawMessage `bun:"data,type:jsonb" json:"data"`
}

type SamnaClient struct {
	bun.BaseModel `bun:"table:claimius.samna_client"`

	ID          uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	Name        string     `bun:"name" json:"name"`
	Description *string    `bun:"description" json:"description"`
	AppID       uuid.UUID  `bun:"app_id" json:"-"`
	ClientID    uuid.UUID  `bun:"client_id,default:gen_random_uuid()" json:"client_id"`
	SecretHash  string     `bun:"secret_hash" json:"-"`
	Status      string     `bun:"status,default:'active'" json:"status"`
	ExpiresAt   *time.Time `bun:"expires_at" json:"expires_at"`
	OwnerID     uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy   *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type SamnaSecret struct {
	bun.BaseModel `bun:"table:claimius.samna_secret"`

	ID        uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID     uuid.UUID  `bun:"app_id" json:"-"`
	Key       string     `bun:"key" json:"key"`
	Value     string     `bun:"value" json:"value,omitempty"`
	OwnerID   uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	DeletedAt *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type SamnaApp struct {
	bun.BaseModel `bun:"table:claimius.samna_app"`

	ID            uuid.UUID        `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	Slug          string           `bun:"slug" json:"slug"`
	Name          string           `bun:"name" json:"name"`
	Description   string           `bun:"description,default:''" json:"description"`
	ContactEmail  *string          `bun:"contact_email" json:"contact_email"`
	AppImage      *string          `bun:"app_image" json:"app_image"`
	RedirectUri   string           `bun:"redirect_uri,default:''" json:"redirect_uri"`
	SyncUri       string           `bun:"sync_uri,default:''" json:"sync_uri"`
	ProviderIDs   []uuid.UUID      `bun:"provider_ids,array,default:'{}'" json:"provider_ids"`
	Status        string           `bun:"status,default:'active'" json:"status"`
	PrivateKey    string           `bun:"private_key" json:"-"`
	PrivateSeed   string           `bun:"private_seed" json:"-"`
	AppSecret     *string          `bun:"app_secret" json:"-"`
	SecretVersion int              `bun:"secret_version,default:1" json:"secret_version"`
	ClaimID       *uuid.UUID       `bun:"claim_id" json:"claim_id"`
	Style         *json.RawMessage `bun:"style,type:jsonb" json:"style"`
	OwnerID       uuid.UUID        `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy     uuid.UUID        `bun:"sa_created_by" json:"created_by"`
	DeletedAt     *time.Time       `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt     time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt     time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type ObjectField struct {
	bun.BaseModel `bun:"table:claimius.object_field"`

	ID          uuid.UUID        `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID        `bun:"app_id" json:"-"`
	ObjectType  string           `bun:"object_type" json:"object_type"`
	Name        string           `bun:"name" json:"name"`
	Description *string          `bun:"description" json:"description"`
	JsonSchema  *json.RawMessage `bun:"json_schema,type:jsonb" json:"json_schema"`
	Required    bool             `bun:"required,default:false" json:"required"`
	Value       *string          `bun:"value" json:"value"`
	OwnerID     uuid.UUID        `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy   *uuid.UUID       `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time       `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt   time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type UserField struct {
	bun.BaseModel `bun:"table:claimius.user_field"`

	ID        uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID     uuid.UUID  `bun:"app_id" json:"-"`
	FieldID   uuid.UUID  `bun:"field_id" json:"field_id"`
	UserID    uuid.UUID  `bun:"user_id" json:"user_id"`
	Value     *string    `bun:"value" json:"value"`
	OwnerID   uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	DeletedAt *time.Time `bun:"sa_deleted_at" json:"deleted_at"`
	CreatedAt time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type UserRelation struct {
	bun.BaseModel `bun:"table:claimius.user_relation" json:"-"`

	ID          uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID  `bun:"app_id" json:"-"`
	UserID      uuid.UUID  `bun:"user_id" json:"user_id"`
	Description *string    `bun:"description" json:"description"`
	ObjectType  string     `bun:"object_type" json:"object_type"`
	ObjectID    uuid.UUID  `bun:"object_id" json:"object_id"`
	Pinned      *int       `bun:"pinned" json:"pinned"`
	Priority    *int       `bun:"priority" json:"priority"`
	UsedAt      *time.Time `bun:"used_at,default:now()" json:"used_at"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at,omitempty"`
	CreatedAt   time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type Audit struct {
	bun.BaseModel `bun:"table:claimius.audit"`

	ID         uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID      *uuid.UUID `bun:"app_id" json:"-"`
	Operation  string     `bun:"operation" json:"operation"`
	Message    string     `bun:"message,default:''" json:"message"`
	Type       string     `bun:"type" json:"type"`
	ObjectType string     `bun:"object_type" json:"object_type"`
	ObjectID   uuid.UUID  `bun:"object_id" json:"object_id"`
	OwnerID    uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy  *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	CreatedAt  time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt  time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type UserObject struct {
	bun.BaseModel `bun:"table:claimius.user_object"`

	ID          string          `bun:"id,pk" json:"id"`
	AppID       uuid.UUID       `bun:"app_id" json:"-"`
	UserID      uuid.UUID       `bun:"user_id" json:"user_id"`
	ObjectID    uuid.UUID       `bun:"object_id" json:"object_id"`
	ObjectType  string          `bun:"object_type" json:"object_type"`
	AccessLevel int             `bun:"sa_access_level" json:"sa_access_level"`
	Grants      json.RawMessage `bun:"grants,type:jsonb,default:'[]'" json:"grants"`
	DirectGrant bool            `bun:"direct_grant,default:false" json:"direct_grant"`
	OwnerID     *uuid.UUID      `bun:"sa_owner_id" json:"sa_owner_id"`
	LocationID  *uuid.UUID      `bun:"sa_location_id" json:"sa_location_id"`
	RootID      *uuid.UUID      `bun:"sa_root_id" json:"sa_root_id"`
	Name        *string         `bun:"sa_name" json:"sa_name"`
	Description *string         `bun:"sa_description" json:"sa_description"`
	Link        *string         `bun:"sa_link" json:"sa_link"`
	CreatedAt   time.Time       `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time       `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type UserUsers struct {
	bun.BaseModel `bun:"table:claimius.user_users"`

	ID                 string    `bun:"id,pk" json:"id"`
	AppID              uuid.UUID `bun:"app_id" json:"-"`
	ViewerID           uuid.UUID `bun:"viewer_id" json:"viewer_id"`
	TargetUserID       uuid.UUID `bun:"target_user_id" json:"target_user_id"`
	FirstSharedAt      time.Time `bun:"first_shared_at,default:now()" json:"first_shared_at"`
	LastSharedAt       time.Time `bun:"last_shared_at,default:now()" json:"last_shared_at"`
	SharingObjectCount int       `bun:"sharing_object_count,default:1" json:"sharing_object_count"`
}

type ObjectUsers struct {
	bun.BaseModel `bun:"table:claimius.object_users"`

	ID         string    `bun:"id,pk" json:"id"`
	AppID      uuid.UUID `bun:"app_id" json:"-"`
	ObjectID   uuid.UUID `bun:"object_id" json:"object_id"`
	ObjectType string    `bun:"object_type" json:"object_type"`
	UserID     uuid.UUID `bun:"user_id" json:"user_id"`
	CreatedAt  time.Time `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt  time.Time `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type SyncEvent struct {
	bun.BaseModel `bun:"table:claimius.sync_event"`

	Seq       int64           `bun:"seq,pk" json:"seq"`
	Operation string          `bun:"operation" json:"operation"`
	EventType string          `bun:"event_type" json:"event_type"`
	Payload   json.RawMessage `bun:"payload,type:jsonb" json:"payload"`
	WrittenAt time.Time       `bun:"written_at,default:now()" json:"written_at"`
}

type DiscipleState struct {
	bun.BaseModel `bun:"table:claimius.disciple_state"`

	ID             uuid.UUID `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	LastAppliedSeq int64     `bun:"last_applied_seq,default:0" json:"last_applied_seq"`
	UpdatedAt      time.Time `bun:"sa_updated_at,default:now()" json:"updated_at"`
}
