package shared

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/utils"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type DB struct {
	*bun.DB
	Logger  echo.Logger
	ConnStr string
}

// GetConnectionString returns the DSN the pool was opened with. The
// notification listener opens its own dedicated pq connection with it.
func (d *DB) GetConnectionString() string {
	return d.ConnStr
}

var ErrSyncUnreachable = errors.New("sync unreachable: app inactive on samna_auth")
var ErrSecretExpired = errors.New("secret expired")

// GetApp reads the system app row from claimius.get_app. The prophet
// holds the full row including key and seed material.
func (d *DB) GetApp(ctx context.Context) (*SamnaApp, error) {
	var app SamnaApp
	if err := d.NewRaw(`SELECT * FROM claimius.get_app() LIMIT 1`).Scan(ctx, &app); err != nil {
		return nil, fmt.Errorf("no claimius app registered: %w", err)
	}
	return &app, nil
}

// GetAppEnv builds the disciple's app identity from the loaded config. A
// disciple never signs or decrypts, so it holds no key or seed material.
func (d *DB) GetAppEnv(ctx context.Context) (*SamnaApp, error) {
	slug := utils.Cfg().AppSlug
	if slug == "" {
		return nil, errors.New("SAUTH_APP_SLUG not set")
	}
	secret := utils.Cfg().AppSecret
	return &SamnaApp{Slug: slug, AppSecret: &secret}, nil
}

func Connect(logger echo.Logger) (*DB, error) {
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

	db := &DB{DB: bunDB, Logger: logger, ConnStr: dsn}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(2 * time.Minute)
	db.SetConnMaxIdleTime(15 * time.Second)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	db.RegisterModel(
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
		(*Notification)(nil),
		(*SyncEvent)(nil),
		(*DiscipleState)(nil),
		(*UserToken)(nil),
	)

	resolveOIDC(logger)

	return db, nil
}

func resolveOIDC(logger echo.Logger) {
	if GetDebugMode() != DebugModeNone {
		return
	}
	c := utils.Cfg()
	if c.Endpoint == "" || c.AppSlug == "" {
		logger.Warn("oidc resolution skipped: endpoint or app slug missing")
		utils.SetAuthDisabled()
		return
	}
	configURL := c.Endpoint + "/api/app/" + c.AppSlug + "/.well-known/openid-configuration"
	oidc, err := client.GetOpenIDConfiguration(context.Background(), configURL)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			logger.Warnf("oidc unavailable for slug %q; auth disabled", c.AppSlug)
		} else {
			logger.Warnf("oidc resolution failed: %v", err)
		}
		utils.SetAuthDisabled()
		return
	}
	tokenURL, err := url.Parse(oidc.TokenEndpoint)
	if err != nil {
		logger.Warnf("oidc token endpoint parse: %v", err)
		utils.SetAuthDisabled()
		return
	}
	jwksURL, err := url.Parse(oidc.JwksUri)
	if err != nil {
		logger.Warnf("oidc jwks uri parse: %v", err)
		utils.SetAuthDisabled()
		return
	}
	utils.SetOIDC(oidc.Issuer, *tokenURL, *jwksURL)
	logger.Infof("oidc issuer=%s token=%s jwks=%s", oidc.Issuer, oidc.TokenEndpoint, oidc.JwksUri)
}

type DebugMode string

const (
	DebugModeNone    DebugMode = "none"
	DebugModeUser    DebugMode = "user"
	DebugModeClients DebugMode = "clients"
)

func GetDebugMode() DebugMode {
	if utils.Cfg().DebugUserID != "" {
		return DebugModeUser
	}
	if utils.Cfg().DebugClients != "" {
		return DebugModeClients
	}
	return DebugModeNone
}

func ModelType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
