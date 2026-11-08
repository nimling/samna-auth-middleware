package sdb

import "github.com/nimling/samna-auth-middleware/v2/shared"

type AccessClaim = shared.AccessClaim
type ClaimAccess = shared.ClaimAccess

const (
	ACCESS_NONE  = shared.ACCESS_NONE
	ACCESS_OWNER = shared.ACCESS_OWNER
	ACCESS_READ  = shared.ACCESS_READ
	ACCESS_WRITE = shared.ACCESS_WRITE
)

var (
	ClaimWhere  = shared.ClaimWhere
	ErrNoAccess = shared.ErrNoAccess
	ObjectWhere = shared.ObjectWhere
	UserWhere   = shared.UserWhere
)
