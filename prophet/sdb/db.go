package sdb

import (
	"context"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/shared"
)

type DB = shared.DB

var instance *DB

func Init(logger echo.Logger) (*DB, error) {
	d, err := shared.Connect(logger)
	if err != nil {
		return nil, err
	}
	instance = d
	return d, nil
}

func Instance() *DB { return instance }

func GetApp(ctx context.Context) (*shared.SamnaApp, error) { return instance.GetApp(ctx) }

func Write(operation, auditType, objectType string, objectID, ownerID uuid.UUID, createdBy *uuid.UUID, message string) {
	shared.WriteAudit(instance, operation, auditType, objectType, objectID, ownerID, createdBy, message)
}
