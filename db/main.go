package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type DB struct {
	*bun.DB
}

var instance *DB
var syncFunc func(ctx context.Context) error

func RegisterSyncFunc(fn func(ctx context.Context) error) {
	syncFunc = fn
}

func Init(logger echo.Logger) {
	pgHost, exists := os.LookupEnv("PGHOST")
	if !exists {
		logger.Fatal("PGHOST not set")
		return
	}

	pgDatabase, exists := os.LookupEnv("PGDATABASE")
	if !exists {
		logger.Fatal("PGDATABASE not set")
		return
	}

	pgPort, exists := os.LookupEnv("PGPORT")
	if !exists {
		logger.Fatal("PGPORT not set")
		return
	}

	pgUser, exists := os.LookupEnv("PGUSER")
	if !exists {
		logger.Fatal("PGUSER not set")
		return
	}

	pgPass, exists := os.LookupEnv("PGPASSWORD")
	if !exists {
		logger.Fatal("PGPASSWORD not set")
		return
	}

	sslMode := os.Getenv("PGSSLMODE")
	if sslMode == "" {
		sslMode = "verify-full"
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", pgUser, pgPass, pgHost, pgPort, pgDatabase, sslMode)
	redactedDSN := fmt.Sprintf("postgres://%s:*********@%s:%s/%s?sslmode=%s", pgUser, pgHost, pgPort, pgDatabase, sslMode)

	logger.Info("DSN:", redactedDSN)
	sqlDB := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	bunDB := bun.NewDB(sqlDB, pgdialect.New())

	if bunDB == nil {
		logger.Fatal("Failed to initialize db")
		return
	}

	instance = &DB{bunDB}

	instance.SetMaxOpenConns(25)
	instance.SetMaxIdleConns(25)
	instance.SetConnMaxLifetime(5 * time.Minute)

	if err := instance.Ping(); err != nil {
		logger.Fatal(err)
		return
	}

	instance.RegisterModel(
		(*SamnaUser)(nil),
		(*Organization)(nil),
		(*Location)(nil),
		(*Claim)(nil),
		(*UserClaim)(nil),
		(*ClaimObject)(nil),
		(*SamnaClient)(nil),
	)

	if IsDebugMode() {
		logger.Info("Debug mode active, skipping initial sync")
		return
	}

	if syncFunc == nil {
		logger.Warn("No sync function registered, skipping initial sync")
		return
	}

	logger.Info("Starting initial sync")
	if err := syncFunc(context.Background()); err != nil {
		logger.Errorf("Initial sync failed: %v", err)
	} else {
		logger.Info("Initial sync completed")
	}
}

func IsDebugMode() bool {
	return os.Getenv("SAUTH_DEBUG_USER_ID") != ""
}

func Instance() *DB {
	return instance
}

func ModelType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
