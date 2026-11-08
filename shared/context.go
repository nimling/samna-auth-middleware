package shared

import (
	"context"

	"github.com/labstack/echo/v4"
)

type ctxKeyType struct{}

var dbCtxKey = ctxKeyType{}

const ctxKey = "sa_db"

// Inject carries the *DB on both the echo context and the request's
// context.Context so handlers read it with FromContext and service code reads it
// with FromCtx, instead of a global. A prophet or disciple wires it once at
// startup with the DB its Init returned.
func Inject(db *DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(ctxKey, db)
			c.SetRequest(c.Request().WithContext(context.WithValue(c.Request().Context(), dbCtxKey, db)))
			return next(c)
		}
	}
}

// FromContext returns the request's DB from an echo context.
func FromContext(c echo.Context) *DB {
	if db, ok := c.Get(ctxKey).(*DB); ok {
		return db
	}
	return FromCtx(c.Request().Context())
}

// FromCtx returns the request's DB from a context.Context.
func FromCtx(ctx context.Context) *DB {
	db, _ := ctx.Value(dbCtxKey).(*DB)
	return db
}
