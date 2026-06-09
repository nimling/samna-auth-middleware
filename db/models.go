package db

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/nimbody"
	"github.com/uptrace/bun"
)

type ObjectType string

func StripObjectSchema(qualified string) string {
	if dot := strings.IndexByte(qualified, '.'); dot >= 0 {
		return qualified[dot+1:]
	}
	return qualified
}

func QualifyObjectTypeWith(s string, defaultSchema string) string {
	if s == "" || strings.IndexByte(s, '.') >= 0 {
		return s
	}
	return defaultSchema + "." + s
}

func (o ObjectType) MarshalJSON() ([]byte, error) {
	return json.Marshal(StripObjectSchema(string(o)))
}

func (o ObjectType) Value() (driver.Value, error) {
	if o == "" {
		return nil, nil
	}
	return string(o), nil
}

func (o *ObjectType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	qualified, err := ResolveObjectType(s)
	if err != nil {
		return err
	}
	*o = qualified
	return nil
}

func QualifyObjectType(s string) string {
	return QualifyObjectTypeWith(s, "claimius")
}

var bareObjectTypes = map[string]ObjectType{
	StripObjectSchema(OBJECT_TYPE_USER):         OBJECT_TYPE_USER,
	StripObjectSchema(OBJECT_TYPE_ORGANIZATION): OBJECT_TYPE_ORGANIZATION,
	StripObjectSchema(OBJECT_TYPE_LOCATION):     OBJECT_TYPE_LOCATION,
	StripObjectSchema(OBJECT_TYPE_CLAIM):        OBJECT_TYPE_CLAIM,
	StripObjectSchema(OBJECT_TYPE_USER_CLAIM):   OBJECT_TYPE_USER_CLAIM,
	StripObjectSchema(OBJECT_TYPE_CLAIM_OBJECT): OBJECT_TYPE_CLAIM_OBJECT,
	StripObjectSchema(OBJECT_TYPE_SAMNA_APP):    OBJECT_TYPE_SAMNA_APP,
	StripObjectSchema(OBJECT_TYPE_SAMNA_CLIENT): OBJECT_TYPE_SAMNA_CLIENT,
	StripObjectSchema(OBJECT_TYPE_SAMNA_SECRET): OBJECT_TYPE_SAMNA_SECRET,
}

// ResolveObjectType returns a qualified ObjectType. Bare claimius types resolve to claimius.X. Unknown bare names default to public.X. Already qualified strings pass through.
func ResolveObjectType(s string) (ObjectType, error) {
	if s == "" {
		return "", fmt.Errorf("object_type is required")
	}
	if q, ok := bareObjectTypes[s]; ok {
		return q, nil
	}
	if strings.IndexByte(s, '.') >= 0 {
		return ObjectType(s), nil
	}
	return ObjectType("public." + s), nil
}

type ClaimAccess int

const (
	ACCESS_NONE    ClaimAccess = 0
	ACCESS_OWNER   ClaimAccess = 1 << 0
	ACCESS_WRITE   ClaimAccess = 1 << 1
	ACCESS_READ    ClaimAccess = 1 << 2
	ACCESS_EXECUTE ClaimAccess = 1 << 3
	ACCESS_DENY    ClaimAccess = 1 << 4
	ACCESS_MEMBER  ClaimAccess = 1 << 5
	ACCESS_ALL     ClaimAccess = ACCESS_OWNER | ACCESS_WRITE | ACCESS_READ | ACCESS_EXECUTE
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

type UserType string

const (
	USER_TYPE_USER   UserType = "user"
	USER_TYPE_CLIENT UserType = "client"
	USER_TYPE_SYSTEM UserType = "system"
	USER_TYPE_GUEST  UserType = "guest"
)

type UserStatus string

const (
	USER_STATUS_ACTIVE    UserStatus = "active"
	USER_STATUS_PENDING   UserStatus = "pending"
	USER_STATUS_SUSPENDED UserStatus = "suspended"
	USER_STATUS_INACTIVE  UserStatus = "inactive"
)

type SamnaUser struct {
	bun.BaseModel `bun:"table:claimius.samna_user"`
	nimbody.Body  `bun:"-" json:"-"`

	ID        uuid.UUID        `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	UserID    uuid.UUID        `bun:"user_id,default:gen_random_uuid()" json:"user_id"`
	AppID     uuid.UUID        `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	FirstName *string          `bun:"first_name" json:"first_name,omitempty"`
	LastName  *string          `bun:"last_name" json:"last_name,omitempty"`
	UserName  *string          `bun:"user_name" json:"user_name,omitempty"`
	UserImage *string          `bun:"user_image" json:"user_image,omitempty"`
	Phone      *string          `bun:"phone" json:"phone,omitempty"`
	Email      string           `bun:"email" json:"email"`
	ExternalID *string          `bun:"external_id" json:"external_id,omitempty"`
	Status     *string          `bun:"status,default:'active'" json:"status,omitempty"`
	Type       *string          `bun:"type,default:'user'" json:"-"`
	Config    *json.RawMessage `bun:"config" json:"config,omitzero"`
	DeletedAt *time.Time       `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
	OwnerID   *uuid.UUID       `bun:"-" json:"owner_id,omitempty"`
	ClaimID   *uuid.UUID       `bun:"-" json:"claim_id,omitempty"`
}

type SamnaUserToken struct {
	nimbody.Body `json:"-"`
	Token        string     `json:"token"`
	IDClaim      *string    `json:"id_claim,omitempty"`
	User         *SamnaUser `json:"user,omitempty"`
	ClaimID      *uuid.UUID `json:"claim_id"`
	AppID        *uuid.UUID `json:"app_id"`
	Reason       *string    `json:"reason,omitempty"`
	OwnerID      *uuid.UUID `json:"owner_id,omitempty"`
}

type SamnaUserExternal struct {
	nimbody.Body `json:"-"`
	IDClaim      *string    `json:"id_claim,omitempty"`
	User         *SamnaUser `json:"user,omitempty"`
	ClaimID      *uuid.UUID `json:"claim_id"`
	AppID        *uuid.UUID `json:"app_id"`
	Reason       *string    `json:"reason,omitempty"`
	OwnerID      *uuid.UUID `json:"owner_id,omitempty"`
}

type UserToken struct {
	bun.BaseModel `bun:"table:claimius.user_token"`

	ID         uuid.UUID       `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID      uuid.UUID       `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"app_id"`
	UserID     uuid.UUID       `bun:"user_id,notnull,type:uuid" json:"user_id"`
	IDClaim    string          `bun:"id_claim,notnull,default:'sub'" json:"id_claim"`
	ExternalID string          `bun:"external_id,notnull" json:"external_id"`
	ClaimID    *uuid.UUID      `bun:"claim_id,type:uuid" json:"claim_id,omitempty"`
	Data       json.RawMessage `bun:"data,type:jsonb" json:"data,omitempty"`
	Type       string          `bun:"type,notnull,default:'token'" json:"type"`
	OwnerID    uuid.UUID       `bun:"sa_owner_id,notnull,type:uuid" json:"owner_id"`
	CreatedBy  *uuid.UUID      `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	DeletedAt  *time.Time      `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt  time.Time       `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt  time.Time       `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type Organization struct {
	bun.BaseModel `bun:"table:claimius.organization"`
	nimbody.Body  `bun:"-" json:"-"`

	ID          uuid.UUID        `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID        `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	Name        string           `bun:"name" json:"name"`
	Description *string          `bun:"description" json:"description"`
	Type        OrganizationType `bun:"type" json:"type"`
	OwnerID     uuid.UUID        `bun:"sa_owner_id" json:"owner_id"`
	RootID      uuid.UUID        `bun:"sa_root_id" json:"root_id"`
	LocationID  *uuid.UUID       `bun:"sa_location_id" json:"location_id,omitzero"`
	Level       int              `bun:"sa_level,default:0" json:"level"`
	ExternalID  *string          `bun:"external_id" json:"external_id,omitempty"`
	ProviderID  *uuid.UUID       `bun:"provider_id" json:"-"`
	CreatedBy   uuid.UUID        `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time       `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt   time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
	Parent      *Organization    `bun:"rel:belongs-to,join:sa_owner_id=id" json:"parent,omitempty"`
}

type Location struct {
	bun.BaseModel `bun:"table:claimius.location"`
	nimbody.Body  `bun:"-" json:"-"`

	ID          uuid.UUID     `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID     `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
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
	ExternalID  *string       `bun:"external_id" json:"external_id,omitempty"`
	ProviderID  *uuid.UUID    `bun:"provider_id" json:"-"`
	CreatedBy   uuid.UUID     `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time    `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt   time.Time     `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time     `bun:"sa_updated_at,default:now()" json:"updated_at"`
	Owner       *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
}

type Claim struct {
	bun.BaseModel `bun:"table:claimius.claim"`
	nimbody.Body  `bun:"-" json:"-"`

	ID           uuid.UUID     `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID        uuid.UUID     `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	Name         string        `bun:"name" json:"name"`
	Description  *string       `bun:"description" json:"description"`
	Access       int           `bun:"sa_access" json:"access"`
	Inherits     bool          `bun:"inherits,default:true" json:"inherits"`
	Type         ClaimType     `bun:"type,default:'user'" json:"type"`
	OwnerID      uuid.UUID     `bun:"sa_owner_id" json:"owner_id"`
	RootID       uuid.UUID     `bun:"sa_root_id" json:"root_id"`
	CreatedBy    *uuid.UUID    `bun:"sa_created_by" json:"created_by"`
	DeletedAt    *time.Time    `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt    time.Time     `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt    time.Time     `bun:"sa_updated_at,default:now()" json:"updated_at"`
	ClaimObjects []ClaimObject `bun:"rel:has-many,join:id=claim_id" json:"claim_objects,omitempty"`
	ClaimUsers   []UserClaim   `bun:"rel:has-many,join:id=claim_id" json:"claim_users,omitempty"`
	Owner        *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`

	sessionCtx   context.Context
	sessionActor *AccessClaim
	sessionDB    *DB
}

type CreateClaimParams struct {
	Name        string
	Description *string
	Access      ClaimAccess
	Inherits    *bool
	Type        ClaimType
}

type ReCreateForOptions struct {
	Name      *string
	CopyUsers bool
}

func ObjectTypeOf(model any) (ObjectType, error) {
	t := reflect.TypeOf(model)
	if t == nil {
		return "", fmt.Errorf("model is nil")
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return "", fmt.Errorf("model must be a struct or pointer to struct")
	}
	bm, ok := t.FieldByName("BaseModel")
	if !ok {
		return "", fmt.Errorf("model has no embedded bun.BaseModel")
	}
	for _, part := range strings.Split(bm.Tag.Get("bun"), ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "table:") {
			return ResolveObjectType(strings.TrimPrefix(part, "table:"))
		}
	}
	return "", fmt.Errorf("bun.BaseModel has no table tag")
}

func ModelID(model any) (uuid.UUID, error) {
	v := reflect.ValueOf(model)
	if !v.IsValid() {
		return uuid.Nil, fmt.Errorf("model is nil")
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return uuid.Nil, fmt.Errorf("model is nil")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return uuid.Nil, fmt.Errorf("model must be a struct or pointer to struct")
	}
	f := v.FieldByName("ID")
	if !f.IsValid() {
		return uuid.Nil, fmt.Errorf("model has no ID field")
	}
	id, ok := f.Interface().(uuid.UUID)
	if !ok {
		return uuid.Nil, fmt.Errorf("model.ID is not uuid.UUID")
	}
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("model.ID is zero")
	}
	return id, nil
}

type UserClaim struct {
	bun.BaseModel `bun:"table:claimius.user_claim"`
	nimbody.Body  `bun:"-" json:"-"`

	ID        uuid.UUID     `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID     uuid.UUID     `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	ClaimID   uuid.UUID     `bun:"claim_id" json:"claim_id"`
	UserID    uuid.UUID     `bun:"user_id" json:"user_id"`
	Reason    *string       `bun:"reason" json:"reason"`
	StartsAt  *time.Time    `bun:"starts_at" json:"starts_at"`
	EndsAt    *time.Time    `bun:"ends_at" json:"ends_at"`
	OwnerID   uuid.UUID     `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy uuid.UUID     `bun:"sa_created_by" json:"created_by"`
	DeletedAt *time.Time    `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt time.Time     `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt time.Time     `bun:"sa_updated_at,default:now()" json:"updated_at"`
	Owner     *Organization `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
	User      *SamnaUser    `bun:"rel:belongs-to,join:user_id=user_id" json:"user,omitempty"`
}

type ClaimObject struct {
	bun.BaseModel `bun:"table:claimius.claim_object"`
	nimbody.Body  `bun:"-" json:"-"`

	ID         uuid.UUID        `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID      uuid.UUID        `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	ClaimID    uuid.UUID        `bun:"claim_id" json:"claim_id"`
	ObjectID   *uuid.UUID       `bun:"object_id" json:"object_id"`
	ObjectType *ObjectType      `bun:"object_type" json:"object_type"`
	Reason     *string          `bun:"reason" json:"reason"`
	Scope      *json.RawMessage `bun:"scope,type:jsonb" json:"scope"`
	Access     *int             `bun:"sa_access,nullzero" json:"access,omitempty"`
	Inherits   bool             `bun:"inherits,default:true" json:"inherits"`
	RefID      *uuid.UUID       `bun:"ref_id" json:"ref_id"`
	OwnerID    uuid.UUID        `bun:"sa_owner_id" json:"owner_id"`
	RootID     uuid.UUID        `bun:"sa_root_id" json:"root_id"`
	CreatedBy  *uuid.UUID       `bun:"sa_created_by" json:"created_by"`
	DeletedAt  *time.Time       `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt  time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt  time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
	Owner      *Organization    `bun:"rel:belongs-to,join:sa_owner_id=id" json:"owner,omitempty"`
}

type AccessClaim struct {
	UserObjectID uuid.UUID        `bun:"user_object_id,scanonly" json:"user_object_id"`
	UserClaimID  *uuid.UUID       `bun:"user_claim_id,scanonly" json:"user_claim_id"`
	ClaimID      *uuid.UUID       `bun:"claim_id,scanonly" json:"claim_id"`
	ClaimName    *string          `bun:"claim_name,scanonly" json:"claim_name,omitempty"`
	Access       int              `bun:"sa_access,scanonly" json:"access"`
	Scope        *json.RawMessage `bun:"scope,type:jsonb,scanonly" json:"scope"`
	OwnerID      uuid.UUID        `bun:"sa_owner_id,scanonly" json:"owner_id"`
	RootID       uuid.UUID        `bun:"sa_root_id,scanonly" json:"root_id"`
	LocationID   *uuid.UUID       `bun:"sa_location_id,scanonly" json:"location_id"`
	DirectGrant  bool             `bun:"direct_grant,scanonly" json:"direct_grant"`
}

type AccessUser struct {
	TargetUserID uuid.UUID  `bun:"target_user_id,scanonly" json:"target_user_id"`
	Access       int        `bun:"access,scanonly" json:"access"`
	UserClaimID  *uuid.UUID `bun:"user_claim_id,scanonly" json:"user_claim_id"`
	ClaimID      *uuid.UUID `bun:"claim_id,scanonly" json:"claim_id"`
	ClaimName    *string    `bun:"claim_name,scanonly" json:"claim_name,omitempty"`
	OwnerID      *uuid.UUID `bun:"owner_id,scanonly" json:"owner_id"`
	RootID       *uuid.UUID `bun:"root_id,scanonly" json:"root_id"`
	LocationID   *uuid.UUID `bun:"location_id,scanonly" json:"location_id"`
	DirectGrant  bool       `bun:"direct_grant,scanonly" json:"direct_grant"`
}

type Asset struct {
	bun.BaseModel `bun:"table:asset" json:"-"`
	nimbody.Body  `bun:"-" json:"-"`

	ID          uuid.UUID  `bun:"id,pk" json:"id"`
	Name        string     `bun:"column:name" json:"name"`
	Description *string    `bun:"column:description" json:"description"`
	MimeType    string     `bun:"column:mime_type" json:"mime_type"`
	UserID      uuid.UUID  `bun:"column:user_id" json:"user_id"`
	Status      string     `bun:"column:status" json:"status"`
	Index       int        `bun:"column:index" json:"index"`
	BlobURL     *string    `bun:"blob_url,default:null" json:"-"`
	AssetURL    string     `bun:"-" json:"asset_url,omitempty"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt   time.Time  `bun:"sa_created_at" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at" json:"updated_at"`
}

type GraphNode struct {
	ObjectID   uuid.UUID       `bun:"object_id" json:"object_id"`
	ObjectType ObjectType      `bun:"object_type" json:"object_type"`
	Label      string          `bun:"label" json:"label"`
	ParentID   *uuid.UUID      `bun:"parent_id" json:"parent_id"`
	Data       json.RawMessage `bun:"data,type:jsonb" json:"data"`
}

type SamnaClient struct {
	bun.BaseModel `bun:"table:claimius.samna_client"`

	ID          uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	Name        string     `bun:"name" json:"name"`
	Description *string    `bun:"description" json:"description"`
	AppID       uuid.UUID  `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	ClientID    uuid.UUID  `bun:"client_id,default:gen_random_uuid()" json:"client_id"`
	SecretHash  string     `bun:"secret_hash" json:"-"`
	Secret      *string    `bun:"-" json:"secret,omitempty"`
	Status      string     `bun:"status,default:'active'" json:"status"`
	ExpiresAt   *time.Time `bun:"expires_at" json:"expires_at"`
	OwnerID     uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy   *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt   time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type SamnaSecret struct {
	bun.BaseModel `bun:"table:claimius.samna_secret"`
	nimbody.Body  `bun:"-" json:"-"`

	ID        uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID     uuid.UUID  `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	Key       string     `bun:"key" json:"key"`
	Value     string     `bun:"value" json:"value,omitempty"`
	ExpiresAt *time.Time `bun:"expires_at" json:"expires_at"`
	OwnerID   uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	DeletedAt *time.Time `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
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
	DeletedAt     *time.Time       `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt     time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt     time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type ObjectField struct {
	bun.BaseModel `bun:"table:claimius.object_field"`
	nimbody.Body  `bun:"-" json:"-"`

	ID          uuid.UUID        `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID        `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	ObjectType  ObjectType       `bun:"object_type" json:"object_type"`
	Name        string           `bun:"name" json:"name"`
	Description *string          `bun:"description" json:"description"`
	JsonSchema  *json.RawMessage `bun:"json_schema,type:jsonb" json:"json_schema"`
	Required    bool             `bun:"required,default:false" json:"required"`
	Value       *string          `bun:"value" json:"value"`
	OwnerID     uuid.UUID        `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy   *uuid.UUID       `bun:"sa_created_by" json:"created_by"`
	DeletedAt   *time.Time       `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt   time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type UserField struct {
	bun.BaseModel `bun:"table:claimius.user_field"`
	nimbody.Body  `bun:"-" json:"-"`

	ID        uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID     uuid.UUID  `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	FieldID   uuid.UUID  `bun:"field_id" json:"field_id"`
	UserID    uuid.UUID  `bun:"user_id" json:"user_id"`
	Value     *string    `bun:"value" json:"value"`
	OwnerID   uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	DeletedAt *time.Time `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type UserRelation struct {
	bun.BaseModel `bun:"table:claimius.user_relation" json:"-"`
	nimbody.Body  `bun:"-" json:"-"`

	ID          uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID  `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	UserID      uuid.UUID  `bun:"user_id" json:"user_id"`
	Description *string    `bun:"description" json:"description"`
	ObjectType  ObjectType `bun:"object_type" json:"object_type"`
	ObjectID    uuid.UUID  `bun:"object_id" json:"object_id"`
	Pinned      *int       `bun:"pinned" json:"pinned"`
	Priority    *int       `bun:"priority" json:"priority"`
	ParentID    *uuid.UUID `bun:"parent_id" json:"parent_id"`
	SiblingID   *uuid.UUID `bun:"sibling_id" json:"sibling_id"`
	UsedAt      *time.Time `bun:"used_at,default:now()" json:"used_at"`
	DeletedAt   *time.Time `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt   time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type Audit struct {
	bun.BaseModel `bun:"table:claimius.audit"`

	ID         uuid.UUID  `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID      *uuid.UUID `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	Operation  string     `bun:"operation" json:"operation"`
	Message    string     `bun:"message,default:''" json:"message"`
	Type       string     `bun:"type" json:"type"`
	ObjectType ObjectType `bun:"object_type" json:"object_type"`
	ObjectID   uuid.UUID  `bun:"object_id" json:"object_id"`
	OwnerID    uuid.UUID  `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy  *uuid.UUID `bun:"sa_created_by" json:"created_by"`
	CreatedAt  time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt  time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type UserObject struct {
	bun.BaseModel `bun:"table:claimius.user_object"`

	ID          uuid.UUID        `bun:"id,pk" json:"id"`
	AppID       uuid.UUID        `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	UserID      uuid.UUID        `bun:"user_id" json:"user_id"`
	ObjectID    uuid.UUID        `bun:"object_id" json:"object_id"`
	ObjectType  ObjectType       `bun:"object_type" json:"object_type"`
	Access      int              `bun:"sa_access" json:"access"`
	Scope       *json.RawMessage `bun:"scope,type:jsonb" json:"scope"`
	Grants      json.RawMessage  `bun:"grants,type:jsonb,default:'[]'" json:"grants"`
	DirectGrant bool             `bun:"direct_grant,default:false" json:"direct_grant"`
	OwnerID     *uuid.UUID       `bun:"sa_owner_id" json:"owner_id"`
	LocationID  *uuid.UUID       `bun:"sa_location_id" json:"location_id"`
	RootID      *uuid.UUID       `bun:"sa_root_id" json:"root_id"`
	Name        *string          `bun:"sa_name" json:"name"`
	Description *string          `bun:"sa_description" json:"description"`
	Link        *string          `bun:"sa_link" json:"link"`
	CreatedAt   time.Time        `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt   time.Time        `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type UserUsers struct {
	bun.BaseModel `bun:"table:claimius.user_users"`

	ID                 uuid.UUID `bun:"id,pk" json:"id"`
	AppID              uuid.UUID `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	ViewerID           uuid.UUID `bun:"viewer_id" json:"viewer_id"`
	TargetUserID       uuid.UUID `bun:"target_user_id" json:"target_user_id"`
	FirstSharedAt      time.Time `bun:"first_shared_at,default:now()" json:"first_shared_at"`
	LastSharedAt       time.Time `bun:"last_shared_at,default:now()" json:"last_shared_at"`
	SharingObjectCount int       `bun:"sharing_object_count,default:1" json:"sharing_object_count"`
}

type ObjectUsers struct {
	bun.BaseModel `bun:"table:claimius.object_users"`

	ID         uuid.UUID  `bun:"id,pk" json:"id"`
	AppID      uuid.UUID  `bun:"app_id,notnull,type:uuid,default:claimius.get_disciple_app_id()" json:"-" sauth:"app_id"`
	ObjectID   uuid.UUID  `bun:"object_id" json:"object_id"`
	ObjectType ObjectType `bun:"object_type" json:"object_type"`
	UserID     uuid.UUID  `bun:"user_id" json:"user_id"`
	CreatedAt  time.Time  `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt  time.Time  `bun:"sa_updated_at,default:now()" json:"updated_at"`
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

type SearchResult struct {
	ObjectID    uuid.UUID        `bun:"object_id" json:"object_id"`
	ObjectType  ObjectType       `bun:"object_type" json:"object_type"`
	UserClaimID *uuid.UUID       `bun:"user_claim_id" json:"user_claim_id"`
	Name        *string          `bun:"name" json:"name"`
	Description *string          `bun:"description" json:"description"`
	Link        *string          `bun:"link" json:"link"`
	Access      int              `bun:"sa_access" json:"access"`
	Scope       *json.RawMessage `bun:"scope,type:jsonb" json:"scope"`
	OwnerID     *uuid.UUID       `bun:"owner_id" json:"owner_id"`
	LocationID  *uuid.UUID       `bun:"location_id" json:"location_id"`
	DirectGrant bool             `bun:"is_direct_grant" json:"direct_grant"`
	Rank        float32          `bun:"rank" json:"rank"`
}
