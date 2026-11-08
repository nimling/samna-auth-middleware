package shared

import (
	"context"

	"github.com/labstack/echo/v4"
)

type ctxKeyType struct{}

var dbCtxKey = ctxKeyType{}

const ctxKey = "sa_db"

func Inject(db *DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(ctxKey, db)
			c.SetRequest(c.Request().WithContext(context.WithValue(c.Request().Context(), dbCtxKey, db)))
			return next(c)
		}
	}
}

func FromContext(c echo.Context) *DB {
	if db, ok := c.Get(ctxKey).(*DB); ok {
		return db
	}
	return FromCtx(c.Request().Context())
}

func FromCtx(ctx context.Context) *DB {
	db, _ := ctx.Value(dbCtxKey).(*DB)
	return db
}
