package sdb

import "github.com/nimling/samna-auth-middleware/shared"

type AccessClaim = shared.AccessClaim
type ClaimAccess = shared.ClaimAccess

const (
	ACCESS_NONE   = shared.ACCESS_NONE
	ACCESS_OWNER  = shared.ACCESS_OWNER
	ACCESS_READ   = shared.ACCESS_READ
	ACCESS_WRITE  = shared.ACCESS_WRITE
	DebugModeNone = shared.DebugModeNone
)

var (
	ClaimWhere   = shared.ClaimWhere
	ErrNoAccess  = shared.ErrNoAccess
	GetDebugMode = shared.GetDebugMode
	ObjectWhere  = shared.ObjectWhere
	UserWhere    = shared.UserWhere
)
