package db

import (
	"context"
	"database/sql"
	"errors"
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

var ErrSyncUnreachable = errors.New("sync unreachable: app inactive on samna_auth")

// GetApp returns the deployment's app row. On a disciple this is the upstream
// prophet app this disciple mirrors (claimius.get_disciple_app). On a pure
// prophet this falls back to claimius.get_prophet_app. Errors when neither is
// registered.
func (d *DB) GetApp(ctx context.Context) (*SamnaApp, error) {
	var app SamnaApp
	if err := d.NewRaw(`SELECT * FROM claimius.get_disciple_app() LIMIT 1`).Scan(ctx, &app); err == nil {
		return &app, nil
	}
	if err := d.NewRaw(`SELECT * FROM claimius.get_prophet_app() LIMIT 1`).Scan(ctx, &app); err != nil {
		return nil, fmt.Errorf("no claimius app registered: %w", err)
	}
	return &app, nil
}

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
		(*SamnaSecret)(nil),
		(*SamnaApp)(nil),
		(*ObjectField)(nil),
		(*UserField)(nil),
		(*UserRelation)(nil),
		(*UserObject)(nil),
		(*UserUsers)(nil),
		(*ObjectUsers)(nil),
		(*SyncEvent)(nil),
		(*DiscipleState)(nil),
	)

	if IsDebugMode() {
		logger.Info("Debug mode active, skipping initial sync")
		return
	}

	if syncFunc == nil {
		logger.Warn("No sync function registered, skipping initial sync")
		return
	}

	maxRetries := 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Infof("Starting initial sync (attempt %d/%d)", attempt, maxRetries)
		if err := syncFunc(context.Background()); err != nil {
			if errors.Is(err, ErrSyncUnreachable) {
				logger.Warnf("Initial sync skipped: %v", err)
				return
			}
			logger.Errorf("Initial sync attempt %d failed: %v", attempt, err)
			if attempt == maxRetries {
				panic(fmt.Sprintf("initial sync failed after %d attempts: %v", maxRetries, err))
			}
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}
		logger.Info("Initial sync completed")
		break
	}
}

func IsDebugMode() bool {
	return os.Getenv("SAUTH_DEBUG_USER_ID") != "" || (os.Getenv("SAUTH_DEBUG_CLIENT_ID") != "" && os.Getenv("SAUTH_DEBUG_CLIENT_SECRET") != "")
}

func Instance() *DB {
	return instance
}

func ModelType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
