package sdb

import "github.com/nimling/samna-auth-middleware/v2/shared"

type (
	AccessClaim              = shared.AccessClaim
	AccessRequest            = shared.AccessRequest
	AccessUser               = shared.AccessUser
	Audit                    = shared.Audit
	Claim                    = shared.Claim
	ClaimAccess              = shared.ClaimAccess
	ClaimObject              = shared.ClaimObject
	ClaimType                = shared.ClaimType
	CreateClaimParams        = shared.CreateClaimParams
	CreateVirtualClaimParams = shared.CreateVirtualClaimParams
	Creator                  = shared.Creator
	GraphNode                = shared.GraphNode
	GraphWindow              = shared.GraphWindow
	Location                 = shared.Location
	LocationType             = shared.LocationType
	ObjectType               = shared.ObjectType
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
	OBJECT_TYPE_CLAIM         = shared.OBJECT_TYPE_CLAIM
	OBJECT_TYPE_CLAIM_OBJECT  = shared.OBJECT_TYPE_CLAIM_OBJECT
	OBJECT_TYPE_LOCATION      = shared.OBJECT_TYPE_LOCATION
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
	AuditWhere           = shared.AuditWhere
	ClaimWhere           = shared.ClaimWhere
	ErrNoAccess          = shared.ErrNoAccess
	ErrNotFound          = shared.ErrNotFound
	ErrRootConflict      = shared.ErrRootConflict
	ErrUserAlreadyExists = shared.ErrUserAlreadyExists
	ObjectWhere          = shared.ObjectWhere
	RelationClaimObjects = shared.RelationClaimObjects
	RelationClaimUsers   = shared.RelationClaimUsers
	RelationOwner        = shared.RelationOwner
	ResolveObjectType    = shared.ResolveObjectType
	StripObjectSchema    = shared.StripObjectSchema
	UserWhere            = shared.UserWhere
)
