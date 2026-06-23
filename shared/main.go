package shared

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
	Logger echo.Logger
}

var instance *DB
var syncFunc func(ctx context.Context) error

var ErrSyncUnreachable = errors.New("sync unreachable: app inactive on samna_auth")
var ErrSecretExpired = errors.New("secret expired")

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

func Init(logger echo.Logger) (*DB, error) {
	pgHost, exists := os.LookupEnv("PGHOST")
	if !exists {
		return nil, errors.New("PGHOST not set")
	}

	pgDatabase, exists := os.LookupEnv("PGDATABASE")
	if !exists {
		return nil, errors.New("PGDATABASE not set")
	}

	pgPort, exists := os.LookupEnv("PGPORT")
	if !exists {
		return nil, errors.New("PGPORT not set")
	}

	pgUser, exists := os.LookupEnv("PGUSER")
	if !exists {
		return nil, errors.New("PGUSER not set")
	}

	pgPass, exists := os.LookupEnv("PGPASSWORD")
	if !exists {
		return nil, errors.New("PGPASSWORD not set")
	}

	sslMode := os.Getenv("PGSSLMODE")
	if sslMode == "" {
		sslMode = "verify-full"
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", pgUser, pgPass, pgHost, pgPort, pgDatabase, sslMode)
	redactedDSN := fmt.Sprintf("postgres://%s:*********@%s:%s/%s?sslmode=%s", pgUser, pgHost, pgPort, pgDatabase, sslMode)

	logger.Info("DSN:", redactedDSN)
	sqlDB := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithReadTimeout(120*time.Second),
		pgdriver.WithWriteTimeout(120*time.Second),
		pgdriver.WithDialTimeout(30*time.Second),
	))
	bunDB := bun.NewDB(sqlDB, pgdialect.New())

	if bunDB == nil {
		return nil, errors.New("failed to initialize db")
	}

	instance = &DB{DB: bunDB, Logger: logger}

	instance.SetMaxOpenConns(25)
	instance.SetMaxIdleConns(2)
	instance.SetConnMaxLifetime(2 * time.Minute)
	instance.SetConnMaxIdleTime(15 * time.Second)

	if err := instance.Ping(); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
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
		(*UserToken)(nil),
	)

	isProphet = false
	var prophetProbe SamnaApp
	if probeErr := instance.NewRaw(`SELECT * FROM claimius.get_prophet_app() LIMIT 1`).Scan(context.Background(), &prophetProbe); probeErr == nil {
		isProphet = true
	}

	if GetDebugMode() != DebugModeNone {
		logger.Info("Debug mode active, skipping initial sync")
		return instance, nil
	}

	if syncFunc == nil {
		logger.Warn("No sync function registered, skipping initial sync")
		return instance, nil
	}

	maxRetries := 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Infof("Starting initial sync (attempt %d/%d)", attempt, maxRetries)
		if err := syncFunc(context.Background()); err != nil {
			if errors.Is(err, ErrSyncUnreachable) {
				logger.Warnf("Initial sync skipped: %v", err)
				return instance, nil
			}
			logger.Errorf("Initial sync attempt %d failed: %v", attempt, err)
			if attempt == maxRetries {
				return nil, fmt.Errorf("initial sync failed after %d attempts: %w", maxRetries, err)
			}
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}
		logger.Info("Initial sync completed")
		break
	}

	return instance, nil
}

type DebugMode string

const (
	DebugModeNone    DebugMode = "none"
	DebugModeUser    DebugMode = "user"
	DebugModeClients DebugMode = "clients"
)

func GetDebugMode() DebugMode {
	if os.Getenv("SAUTH_DEBUG_USER_ID") != "" {
		return DebugModeUser
	}
	if os.Getenv("SAUTH_DEBUG_CLIENTS") != "" {
		return DebugModeClients
	}
	return DebugModeNone
}

var isProphet bool

func IsProphet() bool {
	return isProphet
}

func writesLocally() bool {
	return GetDebugMode() != DebugModeNone || isProphet
}

func Instance() *DB {
	return instance
}

func ModelType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
