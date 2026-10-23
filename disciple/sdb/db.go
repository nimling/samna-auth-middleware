package sdb

import (
	"context"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
)

type DB = shared.DB

var Instance = shared.Instance

func Init(logger echo.Logger, opts ...shared.Options) (*DB, error) {
	d, err := shared.Connect(logger)
	if err != nil {
		return nil, err
	}
	d.RouteToProphet = shared.GetDebugMode() == shared.DebugModeNone
	if len(opts) > 0 {
		d.Options = opts[0]
	}
	return d, nil
}

func GetApp(ctx context.Context) (*shared.SamnaApp, error) { return Instance().GetAppEnv(ctx) }

func Write(ctx context.Context, idb bun.IDB, operation, auditType, objectType string, objectID, ownerID uuid.UUID, createdBy *uuid.UUID, message string) error {
	return shared.WriteAudit(ctx, idb, nil, operation, auditType, objectType, objectID, ownerID, createdBy, message)
}
