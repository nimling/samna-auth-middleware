package sdb

import "github.com/nimling/samna-auth-middleware/v2/shared"

type (
	AccessClaim              = shared.AccessClaim
	AccessRequest            = shared.AccessRequest
	AccessUser               = shared.AccessUser
	AppCost                  = shared.AppCost
	AppCostBucket            = shared.AppCostBucket
	AppCostInput             = shared.AppCostInput
	AppCostItem              = shared.AppCostItem
	AppCostQuery             = shared.AppCostQuery
	Audit                    = shared.Audit
	BindingFilters           = shared.BindingFilters
	Claim                    = shared.Claim
	ClaimAccess              = shared.ClaimAccess
	ClaimObject              = shared.ClaimObject
	ClaimType                = shared.ClaimType
	Code                     = shared.Code
	CodeAccess               = shared.CodeAccess
	CodeCreated              = shared.CodeCreated
	CodeKind                 = shared.CodeKind
	ProfileItem              = shared.ProfileItem
	CreateClaimParams        = shared.CreateClaimParams
	CreateVirtualClaimParams = shared.CreateVirtualClaimParams
	Creator                  = shared.Creator
	GuestMode                = shared.GuestMode
	Descendant               = shared.Descendant
	GrantFilters             = shared.GrantFilters
	GraphNode                = shared.GraphNode
	GraphWindow              = shared.GraphWindow
	Location                 = shared.Location
	LocationType             = shared.LocationType
	ObjectAsset              = shared.ObjectAsset
	ObjectType               = shared.ObjectType
	ObjectUsageResult        = shared.ObjectUsageResult
	Organization             = shared.Organization
	OrganizationType         = shared.OrganizationType
	ReCreateForOptions       = shared.ReCreateForOptions
	SamnaSecret              = shared.SamnaSecret
	SamnaUser                = shared.SamnaUser
	SamnaUserExternal        = shared.SamnaUserExternal
	SamnaUserToken           = shared.SamnaUserToken
	UserClaim                = shared.UserClaim
	UserRelation             = shared.UserRelation
	UserRelationFilters      = shared.UserRelationFilters
	UserIdentity             = shared.UserIdentity
)

const (
	ACCESS_ALL                = shared.ACCESS_ALL
	ACCESS_DENY               = shared.ACCESS_DENY
	ACCESS_EXECUTE            = shared.ACCESS_EXECUTE
	ACCESS_MEMBER             = shared.ACCESS_MEMBER
	ACCESS_NONE               = shared.ACCESS_NONE
	ACCESS_OWNER              = shared.ACCESS_OWNER
	ACCESS_READ               = shared.ACCESS_READ
	ACCESS_WRITE              = shared.ACCESS_WRITE
	CLAIM_TYPE_SERVICE        = shared.CLAIM_TYPE_SERVICE
	CLAIM_TYPE_SYSTEM         = shared.CLAIM_TYPE_SYSTEM
	CLAIM_TYPE_USER           = shared.CLAIM_TYPE_USER
	CLAIM_TYPE_VIRTUAL        = shared.CLAIM_TYPE_VIRTUAL
	CODE_KIND_GUEST           = shared.CODE_KIND_GUEST
	CODE_KIND_INVITE          = shared.CODE_KIND_INVITE
	GUEST_MODE_CODE           = shared.GUEST_MODE_CODE
	GUEST_MODE_OFF            = shared.GUEST_MODE_OFF
	GUEST_MODE_OPEN           = shared.GUEST_MODE_OPEN
	OBJECT_TYPE_CODE          = shared.OBJECT_TYPE_CODE
	OBJECT_TYPE_CLAIM         = shared.OBJECT_TYPE_CLAIM
	OBJECT_TYPE_CLAIM_OBJECT  = shared.OBJECT_TYPE_CLAIM_OBJECT
	OBJECT_TYPE_LOCATION      = shared.OBJECT_TYPE_LOCATION
	OBJECT_TYPE_APP_COST      = shared.OBJECT_TYPE_APP_COST
	COST_SCHEMA_ID            = shared.COST_SCHEMA_ID
	COST_KIND_USAGE           = shared.COST_KIND_USAGE
	COST_KIND_FEE             = shared.COST_KIND_FEE
	COST_KIND_PERIOD          = shared.COST_KIND_PERIOD
	COST_KIND_CREDIT          = shared.COST_KIND_CREDIT
	COST_STATUS_ESTIMATED     = shared.COST_STATUS_ESTIMATED
	COST_STATUS_SETTLED       = shared.COST_STATUS_SETTLED
	COST_STATUS_INVOICED      = shared.COST_STATUS_INVOICED
	COST_GRANULARITY_HOUR     = shared.COST_GRANULARITY_HOUR
	COST_GRANULARITY_DAY      = shared.COST_GRANULARITY_DAY
	COST_GRANULARITY_MONTH    = shared.COST_GRANULARITY_MONTH
	OBJECT_TYPE_OBJECT_ASSET  = shared.OBJECT_TYPE_OBJECT_ASSET
	OBJECT_TYPE_ORGANIZATION  = shared.OBJECT_TYPE_ORGANIZATION
	OBJECT_TYPE_SAMNA_SECRET  = shared.OBJECT_TYPE_SAMNA_SECRET
	OBJECT_TYPE_USER          = shared.OBJECT_TYPE_USER
	OBJECT_TYPE_USER_CLAIM    = shared.OBJECT_TYPE_USER_CLAIM
	OBJECT_TYPE_USER_RELATION = shared.OBJECT_TYPE_USER_RELATION
	ORGANIZATION_TYPE_CHILD   = shared.ORGANIZATION_TYPE_CHILD
	ORGANIZATION_TYPE_COMPANY = shared.ORGANIZATION_TYPE_COMPANY
	ORGANIZATION_TYPE_SYSTEM  = shared.ORGANIZATION_TYPE_SYSTEM
	USER_TYPE_SYSTEM          = shared.USER_TYPE_SYSTEM
)

var (
	AuditWhere            = shared.AuditWhere
	ClaimWhere            = shared.ClaimWhere
	AnonymousUserID       = shared.AnonymousUserID
	ErrBadCode            = shared.ErrBadCode
	ErrBadAppCost         = shared.ErrBadAppCost
	CostSchema            = shared.CostSchema
	ErrCodeInvalid        = shared.ErrCodeInvalid
	ErrNoAccess           = shared.ErrNoAccess
	ErrNotFound           = shared.ErrNotFound
	ErrOrganizationParent = shared.ErrOrganizationParent
	ErrRootConflict       = shared.ErrRootConflict
	ErrUserAlreadyExists  = shared.ErrUserAlreadyExists
	ObjectWhere           = shared.ObjectWhere
	RelationClaimObjects  = shared.RelationClaimObjects
	RelationClaimUsers    = shared.RelationClaimUsers
	RelationOwner         = shared.RelationOwner
	ResolveObjectType     = shared.ResolveObjectType
	StripObjectSchema     = shared.StripObjectSchema
	UserWhere             = shared.UserWhere
)
