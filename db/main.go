package db

import (
	"github.com/nimling/samna-auth-middleware/models"
	"github.com/uptrace/bun"
)

type DB struct {
	*bun.DB
}

var instance *DB

func Init(bunDB *bun.DB) {
	instance = &DB{bunDB}

	bunDB.RegisterModel(
		(*models.SamnaUser)(nil),
		(*models.Organization)(nil),
		(*models.Location)(nil),
		(*models.Claim)(nil),
		(*models.UserClaim)(nil),
		(*models.ClaimObject)(nil),
	)
}

func Instance() *DB {
	return instance
}
