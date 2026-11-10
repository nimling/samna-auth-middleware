package echo

import "github.com/nimling/samna-auth-middleware/v2/shared"

type ClaimAccess = shared.ClaimAccess
type ClaimType string
type OrganizationType string
type LocationType string
type AssetStatus string
type ProviderStatus string
type AppStatus string

const (
	USER_STATUS_ACTIVE    = "active"
	USER_STATUS_PENDING   = "pending"
	USER_STATUS_SUSPENDED = "suspended"
	USER_STATUS_INACTIVE  = "inactive"

	USER_TYPE_USER      = "user"
	USER_TYPE_CLIENT    = "client"
	USER_TYPE_SYSTEM    = "system"
	USER_TYPE_GUEST     = "guest"
	USER_TYPE_ANONYMOUS = "anonymous"

	ASSET_UPLOAD_PENDING  AssetStatus = "pending"
	ASSET_UPLOAD_COMPLETE AssetStatus = "complete"

	ASSET_OBJECT_TYPE_ORGANIZATION = "organization"
	ASSET_OBJECT_TYPE_LOCATION     = "location"

	PROVIDER_STATUS_DRAFT     ProviderStatus = "draft"
	PROVIDER_STATUS_ACTIVE    ProviderStatus = "active"
	PROVIDER_STATUS_INACTIVE  ProviderStatus = "inactive"
	PROVIDER_STATUS_ERRONEOUS ProviderStatus = "erroneous"

	APP_STATUS_DRAFT     AppStatus = "draft"
	APP_STATUS_ACTIVE    AppStatus = "active"
	APP_STATUS_INACTIVE  AppStatus = "inactive"
	APP_STATUS_ERRONEOUS AppStatus = "erroneous"

	SYNC_MODE_IDLE    = "idle"
	SYNC_MODE_ACTIVE  = "active"
	SYNC_MODE_BACKOFF = "backoff"
	SYNC_MODE_PING    = "ping"
)

const (
	ACCESS_NONE    ClaimAccess = 0
	ACCESS_OWNER   ClaimAccess = 1 << 0
	ACCESS_WRITE   ClaimAccess = 1 << 1
	ACCESS_READ    ClaimAccess = 1 << 2
	ACCESS_EXECUTE ClaimAccess = 1 << 3
	ACCESS_DENY    ClaimAccess = 1 << 4
	ACCESS_MEMBER  ClaimAccess = 1 << 5
	ACCESS_ALL     ClaimAccess = ACCESS_OWNER | ACCESS_WRITE | ACCESS_READ | ACCESS_EXECUTE

	CLAIM_TYPE_SYSTEM  ClaimType = "system"
	CLAIM_TYPE_USER    ClaimType = "user"
	CLAIM_TYPE_SERVICE ClaimType = "service"
	CLAIM_TYPE_VIRTUAL ClaimType = "virtual"

	OBJECT_TYPE_USER         ObjectType = "claimius.samna_user"
	OBJECT_TYPE_ORGANIZATION ObjectType = "claimius.organization"
	OBJECT_TYPE_LOCATION     ObjectType = "claimius.location"
	OBJECT_TYPE_CLIENT       ObjectType = "claimius.samna_client"
	OBJECT_TYPE_APP          ObjectType = "claimius.samna_app"
	OBJECT_TYPE_CLAIM        ObjectType = "claimius.claim"
	OBJECT_TYPE_SECRET       ObjectType = "claimius.samna_secret"
	OBJECT_TYPE_AUDIT        ObjectType = "claimius.audit"
	OBJECT_TYPE_CODE         ObjectType = "public.code"
	OBJECT_TYPE_ASSET        ObjectType = "public.asset"
	OBJECT_TYPE_PROVIDER     ObjectType = "public.provider"
	OBJECT_TYPE_MODULE       ObjectType = "public.module"

	ORGANIZATION_TYPE_SYSTEM  OrganizationType = "system"
	ORGANIZATION_TYPE_COMPANY OrganizationType = "company"
	ORGANIZATION_TYPE_ENTITY  OrganizationType = "entity"

	LOCATION_TYPE_BUILDING LocationType = "building"
	LOCATION_TYPE_FLOOR    LocationType = "floor"
	LOCATION_TYPE_ROOM     LocationType = "room"
)
