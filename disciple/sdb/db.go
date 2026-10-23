package sdb

import (
	"context"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
)

type AssetResponse struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

type AssetCache interface {
	Get(ctx context.Context, linkID uuid.UUID, conditional http.Header) (*AssetResponse, error)
	Put(ctx context.Context, linkID uuid.UUID, header http.Header, body io.Reader) error
}

type Options struct {
	AssetSource func(ctx context.Context, assetID uuid.UUID) (*AssetResponse, error)
	AssetCache  AssetCache
}

type DB struct {
	*shared.DB
	Options
}

var instance *DB

func Init(logger echo.Logger, opts ...Options) (*DB, error) {
	d, err := shared.Connect(logger)
	if err != nil {
		return nil, err
	}
	d.RouteToProphet = shared.GetDebugMode() == shared.DebugModeNone
	instance = &DB{DB: d}
	if len(opts) > 0 {
		instance.Options = opts[0]
	}
	return instance, nil
}

func Instance() *DB { return instance }

func GetApp(ctx context.Context) (*shared.SamnaApp, error) { return instance.DB.GetAppEnv(ctx) }

func Write(ctx context.Context, idb bun.IDB, operation, auditType, objectType string, objectID, ownerID uuid.UUID, createdBy *uuid.UUID, message string) error {
	return shared.WriteAudit(ctx, idb, nil, operation, auditType, objectType, objectID, ownerID, createdBy, message)
}
