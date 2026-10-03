package echo

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/nimbody"
	"github.com/uptrace/bun"
)

type StyleColors struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
}

type ObjectStyle struct {
	Colors StyleColors `json:"colors"`
}

type SamnaUser struct {
	ID            uuid.UUID                  `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	UserID        uuid.UUID                  `bun:"user_id,notnull,type:uuid,default:gen_random_uuid()" json:"user_id"`
	AppID         uuid.UUID                  `bun:"app_id,notnull,type:uuid" json:"app_id"`
	FirstName     string                     `bun:"first_name" json:"first_name"`
	LastName      string                     `bun:"last_name" json:"last_name"`
	UserName      *string                    `bun:"user_name" json:"user_name,omitempty"`
	Phone         *string                    `bun:"phone" json:"phone,omitempty"`
	Email         string                     `bun:"email" json:"email,omitempty"`
	ExternalID    *string                    `bun:"external_id" json:"external_id,omitempty"`
	Status        string                     `bun:"status" json:"status"`
	Type          string                     `bun:"type,notnull,default:'user'" json:"type"`
	Config        *json.RawMessage           `bun:"config,type:jsonb" json:"config,omitempty"`
	DeletedAt     *time.Time                 `bun:"sa_deleted_at,default:null" json:"deleted_at,omitzero"`
	CreatedAt     time.Time                  `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time                  `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	App           *SamnaApp                  `bun:"rel:belongs-to,join:app_id=id" json:"app,omitempty"`
	Fields        []*UserField               `bun:"rel:has-many,join:user_id=user_id" json:"fields,omitempty"`
	Identity      map[string]json.RawMessage `bun:"-" json:"identity,omitempty"`
	Access        ClaimAccess                `bun:"access,scanonly" json:"access,omitempty"`
	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.samna_user" json:"-"`
}

type UserToken struct {
	ID            uuid.UUID       `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	AppID         uuid.UUID       `bun:"app_id,notnull,type:uuid" json:"app_id"`
	UserID        uuid.UUID       `bun:"user_id,notnull,type:uuid" json:"user_id"`
	IDClaim       string          `bun:"id_claim,notnull,default:'sub'" json:"id_claim"`
	ExternalID    string          `bun:"external_id,notnull" json:"external_id"`
	ClaimID       *uuid.UUID      `bun:"claim_id,type:uuid" json:"claim_id,omitempty"`
	Data          json.RawMessage `bun:"data,type:jsonb" json:"data,omitempty"`
	Type          string          `bun:"type,notnull,default:'token'" json:"type"`
	OwnerID       uuid.UUID       `bun:"sa_owner_id,notnull,type:uuid" json:"owner_id"`
	CreatedBy     *uuid.UUID      `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	DeletedAt     *time.Time      `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt     time.Time       `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt     time.Time       `bun:"sa_updated_at,default:now()" json:"updated_at"`
	bun.BaseModel `bun:"table:claimius.user_token" json:"-"`
}

type Asset struct {
	ID            uuid.UUID  `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID         uuid.UUID  `bun:"app_id,notnull,type:uuid" json:"app_id"`
	Name          string     `bun:"column:name,notnull" json:"name"`
	Description   *string    `bun:"column:description,notnull" json:"description"`
	MimeType      string     `bun:"column:mime_type,notnull" json:"mime_type"`
	OwnerID       uuid.UUID  `bun:"sa_owner_id,notnull,type:uuid" json:"owner_id"`
	CreatedBy     uuid.UUID  `bun:"sa_created_by,notnull,type:uuid" json:"created_by"`
	Status        string     `bun:"column:status" json:"status"`
	Type          *string    `bun:"column:type" json:"type,omitempty"`
	Index         int        `bun:"column:index" json:"index"`
	BlobURL       *string    `bun:"blob_url,default:null" json:"-"`
	AssetURL      string     `bun:"-" json:"asset_url,omitempty"`
	DeletedAt     *time.Time `bun:"sa_deleted_at,default:null" json:"deleted_at,omitzero"`
	CreatedAt     time.Time  `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time  `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:public.asset" json:"-"`
}

type ObjectAsset struct {
	ID            uuid.UUID  `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID         uuid.UUID  `bun:"app_id,notnull,type:uuid" json:"app_id"`
	AssetID       uuid.UUID  `bun:"asset_id,type:uuid,notnull" json:"asset_id"`
	ObjectID      uuid.UUID  `bun:"object_id,type:uuid,notnull" json:"object_id"`
	ObjectType    ObjectType `bun:"object_type,notnull" json:"object_type"`
	Index         int        `bun:"column:index,notnull" json:"index"`
	CreatedBy     uuid.UUID  `bun:"sa_created_by,type:uuid,notnull" json:"created_by"`
	OwnerID       uuid.UUID  `bun:"sa_owner_id,type:uuid,notnull" json:"owner_id"`
	DeletedAt     *time.Time `bun:"sa_deleted_at,default:null" json:"deleted_at,omitzero"`
	CreatedAt     time.Time  `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time  `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	Asset         *Asset     `bun:"rel:belongs-to,join:asset_id=id" json:"asset,omitempty"`
	bun.BaseModel `bun:"table:public.object_asset" json:"-"`
}

type Code struct {
	ID            uuid.UUID   `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID         uuid.UUID   `bun:"app_id,notnull,type:uuid" json:"app_id"`
	Value         string      `bun:"code,notnull" json:"code"`
	Count         int         `bun:"count,notnull,default:1" json:"count"`
	RedeemedCount int         `bun:"redeemed_count,notnull,default:0" json:"redeemed_count"`
	StartsAt      *time.Time  `bun:"starts_at" json:"starts_at,omitempty"`
	EndsAt        *time.Time  `bun:"ends_at" json:"ends_at,omitempty"`
	ExpiresAt     *time.Time  `bun:"expires_at" json:"expires_at,omitempty"`
	RedeemedAt    *time.Time  `bun:"redeemed_at" json:"redeemed_at,omitempty"`
	UserID        *uuid.UUID  `bun:"user_id,type:uuid" json:"user_id,omitempty"`
	ClaimID       *uuid.UUID  `bun:"claim_id,type:uuid" json:"claim_id,omitempty"`
	ReferenceType *string     `bun:"reference_type" json:"reference_type,omitempty"`
	ReferenceID   *uuid.UUID  `bun:"reference_id,type:uuid" json:"reference_id,omitempty"`
	OwnerID       uuid.UUID   `bun:"sa_owner_id,notnull,type:uuid" json:"owner_id"`
	CreatedBy     uuid.UUID   `bun:"sa_created_by,notnull,type:uuid" json:"created_by"`
	DeletedAt     *time.Time  `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt     time.Time   `bun:"sa_created_at,default:current_timestamp,notnull" json:"created_at"`
	UpdatedAt     time.Time   `bun:"sa_updated_at,default:current_timestamp,notnull" json:"updated_at"`
	Access        ClaimAccess `bun:"access,scanonly" json:"access,omitempty"`
	bun.BaseModel `bun:"table:public.code" json:"-"`
}

type UserField struct {
	ID            uuid.UUID    `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID         uuid.UUID    `bun:"app_id,notnull,type:uuid" json:"app_id"`
	FieldID       uuid.UUID    `bun:"field_id,type:uuid,notnull" json:"field_id"`
	UserID        uuid.UUID    `bun:"user_id,type:uuid,notnull" json:"user_id"`
	Value         *string      `bun:"value" json:"value,omitempty"`
	OwnerID       *uuid.UUID   `bun:"sa_owner_id,type:uuid" json:"owner_id,omitempty"`
	CreatedBy     *uuid.UUID   `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	DeletedAt     *time.Time   `bun:"sa_deleted_at,default:null" json:"deleted_at,omitzero"`
	CreatedAt     time.Time    `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time    `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	Field         *ObjectField `bun:"rel:belongs-to,join:field_id=id" json:"field,omitempty"`
	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.user_field" json:"-"`
}

type ObjectField struct {
	ID            uuid.UUID        `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID         uuid.UUID        `bun:"app_id,type:uuid,notnull" json:"app_id"`
	ObjectType    string           `bun:"object_type,notnull" json:"object_type"`
	Name          string           `bun:"name,notnull" json:"name"`
	Description   *string          `bun:"description" json:"description,omitempty"`
	JsonSchema    *json.RawMessage `bun:"json_schema,type:jsonb" json:"json_schema,omitempty"`
	Required      bool             `bun:"required,notnull,default:false" json:"required"`
	Value         *string          `bun:"value" json:"value,omitempty"`
	OwnerID       uuid.UUID        `bun:"sa_owner_id,notnull,type:uuid" json:"owner_id"`
	CreatedBy     *uuid.UUID       `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	DeletedAt     *time.Time       `bun:"sa_deleted_at,default:null" json:"deleted_at,omitzero"`
	CreatedAt     time.Time        `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time        `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.object_field" json:"-"`
}

type Organization struct {
	ID          uuid.UUID   `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	Name        string      `bun:"name,notnull" json:"name"`
	Description *string     `bun:"description" json:"description,omitempty"`
	OwnerID     *uuid.UUID  `bun:"sa_owner_id,type:uuid" json:"owner_id,omitempty"`
	RootID      *uuid.UUID  `bun:"sa_root_id,type:uuid" json:"root_id"`
	LocationID  *uuid.UUID  `bun:"sa_location_id,type:uuid" json:"location_id,omitempty"`
	Type        *string     `bun:"type" json:"type,omitempty"`
	PrivateSeed string      `bun:"private_seed" json:"-"`
	Level       int         `bun:"sa_level,notnull,default:0" json:"level"`
	ExternalID  *string     `bun:"external_id" json:"external_id,omitempty"`
	ProviderID  *string     `bun:"provider_id,type:uuid" json:"-"`
	CreatedBy   *uuid.UUID  `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	DeletedAt   *time.Time  `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt   time.Time   `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt   time.Time   `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	Assets      []*Asset    `bun:"-" json:"assets,omitempty"`
	RootName    string      `bun:"root_name,scanonly" json:"root_name,omitempty"`
	ParentName  string      `bun:"parent_name,scanonly" json:"parent_name,omitempty"`
	AssetID     *uuid.UUID  `bun:"asset_id,scanonly" json:"asset_id,omitempty"`
	AssetURL    *string     `bun:"asset_url,scanonly" json:"asset_url,omitempty"`
	Access      ClaimAccess `bun:"access,scanonly" json:"access,omitempty"`

	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.organization" json:"-"`
}

type Location struct {
	ID          uuid.UUID   `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	OwnerID     uuid.UUID   `bun:"sa_owner_id,type:uuid,notnull" json:"owner_id"`
	RootID      *uuid.UUID  `bun:"sa_root_id,type:uuid" json:"root_id"`
	Name        string      `bun:"name,notnull" json:"name"`
	Description *string     `bun:"description" json:"description,omitempty"`
	ParentID    *uuid.UUID  `bun:"sa_parent_id,type:uuid" json:"parent_id"`
	Type        string      `bun:"type,notnull,default:'room'" json:"type"`
	Longitude   *float64    `bun:"longitude" json:"longitude,omitempty"`
	Latitude    *float64    `bun:"latitude" json:"latitude,omitempty"`
	Level       int         `bun:"sa_level,notnull,default:0" json:"level"`
	CodeType    *string     `bun:"code_type" json:"code_type,omitempty"`
	Code        *string     `bun:"code" json:"code,omitempty"`
	ExternalID  *string     `bun:"external_id" json:"external_id,omitempty"`
	ProviderID  *string     `bun:"provider_id" json:"-"`
	CreatedBy   *uuid.UUID  `bun:"sa_created_by,type:uuid" json:"created_by"`
	CreatedAt   time.Time   `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt   time.Time   `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	DeletedAt   *time.Time  `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	Assets      []*Asset    `bun:"-" json:"assets,omitempty"`
	OwnerName   string      `bun:"owner_name,scanonly" json:"owner_name,omitempty"`
	ParentName  string      `bun:"parent_name,scanonly" json:"parent_name,omitempty"`
	AssetID     *uuid.UUID  `bun:"asset_id,scanonly" json:"asset_id,omitempty"`
	AssetURL    *string     `bun:"asset_url,scanonly" json:"asset_url,omitempty"`
	Access      ClaimAccess `bun:"access,scanonly" json:"access,omitempty"`

	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.location" json:"-"`
}

type Claim struct {
	ID          uuid.UUID   `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID       uuid.UUID   `bun:"app_id,notnull,type:uuid" json:"app_id"`
	Name        string      `bun:"name,notnull" json:"name"`
	Description *string     `bun:"description" json:"description,omitempty"`
	Access      ClaimAccess `bun:"sa_access,notnull" json:"access"`
	Inherits    bool        `bun:"inherits,notnull,default:true" json:"inherits"`
	Type        ClaimType   `bun:"type,notnull,default:'system'" json:"type"`
	CreatedBy   *uuid.UUID  `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	OwnerID     uuid.UUID   `bun:"sa_owner_id,type:uuid,notnull" json:"owner_id"`
	RootID      uuid.UUID   `bun:"sa_root_id,type:uuid,notnull" json:"root_id"`
	DeletedAt   *time.Time  `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt   time.Time   `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt   time.Time   `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	Txid        uint64      `bun:"txid,scanonly" json:"-"`

	ClaimObjects    []ClaimObject `bun:"rel:has-many,join:id=claim_id" json:"claim_objects,omitempty"`
	ClaimUsers      []UserClaim   `bun:"rel:has-many,join:id=claim_id" json:"claim_users,omitempty"`
	OwnerName       string        `bun:"owner_name,scanonly" json:"owner_name,omitempty"`
	RootName        string        `bun:"root_name,scanonly" json:"root_name,omitempty"`
	EffectiveAccess ClaimAccess   `bun:"access,scanonly" json:"effective_access,omitempty"`

	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.claim" json:"-"`
}

type UserClaim struct {
	ID            uuid.UUID  `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID         uuid.UUID  `bun:"app_id,notnull,type:uuid" json:"app_id"`
	ClaimID       uuid.UUID  `bun:"claim_id,type:uuid,notnull" json:"claim_id"`
	UserID        uuid.UUID  `bun:"user_id,type:uuid,notnull" json:"user_id"`
	Reason        *string    `bun:"reason" json:"reason,omitempty"`
	StartsAt      *time.Time `bun:"starts_at" json:"starts_at"`
	EndsAt        *time.Time `bun:"ends_at" json:"ends_at"`
	CreatedBy     *uuid.UUID `bun:"sa_created_by,type:uuid" json:"created_by"`
	OwnerID       uuid.UUID  `bun:"sa_owner_id,type:uuid,notnull" json:"owner_id"`
	DeletedAt     *time.Time `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt     time.Time  `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time  `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	Txid          uint64     `bun:"txid,scanonly" json:"-"`
	FirstName     string     `bun:"first_name,scanonly" json:"first_name,omitempty"`
	LastName      string     `bun:"last_name,scanonly" json:"last_name,omitempty"`
	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.user_claim" json:"-"`
}

type ClaimObject struct {
	ID                uuid.UUID        `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID             uuid.UUID        `bun:"app_id,notnull,type:uuid" json:"app_id"`
	Reason            *string          `bun:"reason" json:"reason,omitempty"`
	Scope             *json.RawMessage `bun:"scope,type:jsonb" json:"scope,omitempty"`
	ClaimID           uuid.UUID        `bun:"claim_id,type:uuid,notnull" json:"claim_id"`
	ObjectID          *uuid.UUID       `bun:"object_id,type:uuid" json:"object_id"`
	ObjectType        *ObjectType      `bun:"object_type" json:"object_type"`
	Access            *int             `bun:"sa_access" json:"access"`
	Inherits          bool             `bun:"inherits,notnull,default:true" json:"inherits"`
	RefID             *uuid.UUID       `bun:"ref_id,type:uuid" json:"ref_id,omitempty"`
	RootID            uuid.UUID        `bun:"sa_root_id,type:uuid,notnull" json:"root_id"`
	CreatedBy         *uuid.UUID       `bun:"sa_created_by,type:uuid" json:"created_by"`
	OwnerID           uuid.UUID        `bun:"sa_owner_id,type:uuid,notnull" json:"owner_id"`
	DeletedAt         *time.Time       `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt         time.Time        `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt         time.Time        `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	Txid              uint64           `bun:"txid,scanonly" json:"-"`
	ObjectName        string           `bun:"object_name,scanonly" json:"object_name,omitempty"`
	ObjectDescription *string          `bun:"object_description,scanonly" json:"object_description,omitempty"`
	nimbody.Body      `bun:"-" json:"-"`
	bun.BaseModel     `bun:"table:claimius.claim_object" json:"-"`
}

type SamnaClient struct {
	ID            uuid.UUID   `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	Name          string      `bun:"name,notnull" json:"name"`
	Description   *string     `bun:"description" json:"description,omitempty"`
	AppID         uuid.UUID   `bun:"app_id,type:uuid,notnull" json:"app_id"`
	ClientID      uuid.UUID   `bun:"client_id,type:uuid,notnull,default:gen_random_uuid()" json:"client_id"`
	SecretHash    string      `bun:"secret_hash,notnull" json:"-" sauth:"secret_hash"`
	Secret        *string     `bun:"-" json:"secret,omitempty"`
	Status        string      `bun:"status,notnull,default:'active'" json:"status"`
	ExpiresAt     *time.Time  `bun:"expires_at" json:"expires_at,omitempty"`
	OwnerID       uuid.UUID   `bun:"sa_owner_id,type:uuid,notnull" json:"owner_id"`
	CreatedBy     *uuid.UUID  `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	DeletedAt     *time.Time  `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt     time.Time   `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time   `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	Access        ClaimAccess `bun:"access,scanonly" json:"access,omitempty"`
	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.samna_client" json:"-"`
}

type SamnaSecret struct {
	ID            uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID         uuid.UUID  `bun:"app_id,type:uuid,notnull" json:"app_id"`
	Key           string     `bun:"key,notnull" json:"key"`
	Value         string     `bun:"value,notnull" json:"value,omitempty"`
	ExpiresAt     *time.Time `bun:"expires_at" json:"expires_at,omitempty"`
	OwnerID       uuid.UUID  `bun:"sa_owner_id,type:uuid,notnull" json:"owner_id"`
	CreatedBy     *uuid.UUID `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	DeletedAt     *time.Time `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt     time.Time  `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time  `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.samna_secret" json:"-"`
}

type SamnaApp struct {
	ID            uuid.UUID    `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	Slug          string       `bun:"slug,notnull" json:"slug"`
	Name          string       `bun:"name,notnull" json:"name"`
	Description   string       `bun:"description" json:"description,omitempty"`
	ContactEmail  *string      `bun:"contact_email" json:"contact_email,omitempty"`
	AppImage      *string      `bun:"app_image" json:"app_image,omitempty"`
	RedirectURIs  []string     `bun:"redirect_uris,array,type:text[]" json:"redirect_uris"`
	SyncURI       string       `bun:"sync_uri,notnull" json:"sync_uri,omitempty"`
	ProviderIDs   []uuid.UUID  `bun:"provider_ids,array,type:uuid[]" json:"provider_ids,omitempty"`
	Status        AppStatus    `bun:"status,notnull,default:'active'" json:"status"`
	PrivateKey    string       `bun:"private_key,notnull" json:"private_key,omitempty"`
	PrivateSeed   string       `bun:"private_seed,notnull" json:"private_seed,omitempty"`
	AppSecret     *string      `bun:"app_secret" json:"app_secret,omitempty"`
	HomeProphet   *string      `bun:"home_prophet" json:"home_prophet,omitempty"`
	SecretVersion int          `bun:"secret_version,notnull,default:1" json:"-"`
	ClaimID       *uuid.UUID   `bun:"claim_id,type:uuid" json:"claim_id,omitempty"`
	Style         *ObjectStyle `bun:"style,type:jsonb" json:"style,omitempty"`
	OwnerID       uuid.UUID    `bun:"sa_owner_id,notnull,type:uuid" json:"owner_id"`
	CreatedBy     uuid.UUID    `bun:"sa_created_by,notnull,type:uuid" json:"created_by"`
	DeletedAt     *time.Time   `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt     time.Time    `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time    `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`
	Access        ClaimAccess  `bun:"access,scanonly" json:"access,omitempty"`
	bun.BaseModel `bun:"table:claimius.samna_app" json:"-"`
}

// GetPrimaryOrigin returns the first registered redirect URI, the origin the app
// is reached on. Every other entry is an additional callback the authorize
// endpoint accepts.
func (a SamnaApp) GetPrimaryOrigin() string {
	if len(a.RedirectURIs) == 0 {
		return ""
	}
	return a.RedirectURIs[0]
}

type UserRelation struct {
	ID          uuid.UUID  `bun:"id,pk,notnull,type:uuid,default:gen_random_uuid()" json:"id"`
	UserID      uuid.UUID  `bun:"user_id,notnull,type:uuid" json:"user_id"`
	AppID       *uuid.UUID `bun:"app_id,type:uuid" json:"app_id,omitempty"`
	Description *string    `bun:"description" json:"description,omitempty"`
	ObjectType  string     `bun:"object_type,notnull" json:"object_type"`
	ObjectID    uuid.UUID  `bun:"object_id,notnull,type:uuid" json:"object_id"`
	Pinned      int        `bun:"pinned" json:"pinned"`
	Priority    int        `bun:"priority" json:"priority"`
	UsedAt      *time.Time `bun:"used_at,default:current_timestamp" json:"used_at,omitempty"`
	ParentID    *uuid.UUID `bun:"parent_id,type:uuid" json:"parent_id,omitempty"`
	SiblingID   *uuid.UUID `bun:"sibling_id,type:uuid" json:"sibling_id,omitempty"`
	DeletedAt   *time.Time `bun:"sa_deleted_at,default:null" json:"deleted_at,omitzero"`
	CreatedAt   time.Time  `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt   time.Time  `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`

	nimbody.Body  `bun:"-" json:"-"`
	bun.BaseModel `bun:"table:claimius.user_relation" json:"-"`
}

type Audit struct {
	ID         uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID      uuid.UUID  `bun:"app_id,type:uuid" json:"app_id"`
	Operation  string     `bun:"operation,notnull" json:"operation"`
	Message    string     `bun:"message" json:"message"`
	Type       string     `bun:"type,notnull" json:"type"`
	ObjectType string     `bun:"object_type,notnull" json:"object_type"`
	ObjectID   uuid.UUID  `bun:"object_id,type:uuid,notnull" json:"object_id"`
	CreatedBy  *uuid.UUID `bun:"sa_created_by,type:uuid" json:"created_by,omitempty"`
	OwnerID    uuid.UUID  `bun:"sa_owner_id,type:uuid,notnull" json:"owner_id"`
	CreatedAt  time.Time  `bun:"sa_created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt  time.Time  `bun:"sa_updated_at,notnull,default:current_timestamp" json:"updated_at"`

	bun.BaseModel `bun:"table:claimius.audit" json:"-"`
}

type SyncEvent struct {
	Txid          int64           `bun:"txid,scanonly" json:"txid"`
	Seq           int64           `bun:"seq,pk" json:"seq"`
	Operation     string          `bun:"operation" json:"operation"`
	EventType     string          `bun:"event_type" json:"event_type"`
	Payload       json.RawMessage `bun:"payload,type:jsonb" json:"payload"`
	WrittenAt     time.Time       `bun:"written_at" json:"written_at"`
	bun.BaseModel `bun:"table:claimius.sync_event" json:"-"`
}

type ProphetState struct {
	ID             uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AppID          uuid.UUID  `bun:"app_id,type:uuid,notnull" json:"app_id"`
	LastAppliedSeq int64      `bun:"last_applied_seq" json:"last_applied_seq"`
	SystemAppSlug  *string    `bun:"system_app_slug" json:"system_app_slug,omitempty"`
	SyncMode       string     `bun:"sync_mode,notnull,default:'idle'" json:"sync_mode"`
	SyncFailures   int        `bun:"sync_failures,notnull,default:0" json:"sync_failures"`
	NextAttemptAt  *time.Time `bun:"next_attempt_at" json:"next_attempt_at,omitempty"`
	LastContactAt  *time.Time `bun:"last_contact_at" json:"last_contact_at,omitempty"`
	TrimmedSeq     int64      `bun:"trimmed_seq,notnull,default:0" json:"trimmed_seq"`
	UpdatedAt      time.Time  `bun:"sa_updated_at" json:"updated_at"`
	bun.BaseModel  `bun:"table:claimius.prophet_state" json:"-"`
}

type GraphNode struct {
	ObjectID   uuid.UUID   `bun:"object_id" json:"object_id"`
	ObjectType string      `bun:"object_type" json:"object_type"`
	Label      string      `bun:"label" json:"label"`
	ParentID   *uuid.UUID  `bun:"parent_id" json:"parent_id,omitempty"`
	Data       interface{} `bun:"data,type:jsonb" json:"data"`
}
