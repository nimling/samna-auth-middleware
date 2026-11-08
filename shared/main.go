package shared

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type DB struct {
	*bun.DB
	Replica *bun.DB
	Logger  echo.Logger
	ConnStr string
	Usage   *ObjectUsage
	stmts   sync.Map

	RouteToProphet bool
}

type stmtKey struct {
	db    *bun.DB
	query string
}

func (d *DB) Read(ctx context.Context, fn func(ctx context.Context, db *bun.DB) error) error {
	if d.Replica == nil {
		return fn(ctx, d.DB)
	}
	err := fn(ctx, d.Replica)
	var pgErr interface{ Field(byte) string }
	if errors.As(err, &pgErr) && pgErr.Field('C') == "25006" {
		return fn(ctx, d.DB)
	}
	return err
}

func (d *DB) prepared(ctx context.Context, db *bun.DB, dest any, query string, args ...any) error {
	key := stmtKey{db: db, query: query}
	cached, ok := d.stmts.Load(key)
	if !ok {
		stmt, err := db.DB.PrepareContext(ctx, query)
		if err != nil {
			return err
		}
		var loaded bool
		if cached, loaded = d.stmts.LoadOrStore(key, stmt); loaded {
			stmt.Close()
		}
	}
	rows, err := cached.(*sql.Stmt).QueryContext(ctx, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	return db.ScanRows(ctx, rows, dest)
}

func (d *DB) GetConnectionString() string {
	return d.ConnStr
}

var ErrSyncUnreachable = errors.New("sync unreachable: app inactive on samna_auth")
var ErrSecretExpired = errors.New("secret expired")
var ErrRoutedSet = errors.New("a routed update carries a Set expression the prophet cannot apply: set the field on the model, or use NewDelete for a soft delete")
var ErrRoutedNoRecord = errors.New("a routed update or delete carries no record: populate the model's key or call ID, the record is never read from a Where expression")

func (d *DB) GetApp(ctx context.Context) (*SamnaApp, error) {
	if seed := utils.Cfg().DebugSeed; seed != "" && GetDebugMode() != DebugModeNone {
		app, err := d.GetAppEnv(ctx)
		if err != nil {
			return nil, err
		}
		app.PrivateSeed = seed
		return app, nil
	}
	var app SamnaApp
	if err := d.Read(ctx, func(ctx context.Context, db *bun.DB) error {
		return db.NewRaw(`SELECT * FROM claimius.get_app() LIMIT 1`).Scan(ctx, &app)
	}); err != nil {
		return nil, fmt.Errorf("no claimius app registered: %w", err)
	}
	return &app, nil
}

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
	db := &DB{DB: openPool(dsn), Logger: logger, ConnStr: dsn}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	if replicaDSN := utils.Cfg().ReplicaDSN; replicaDSN != "" {
		db.Replica = openPool(replicaDSN)
		if err := db.Replica.Ping(); err != nil {
			return nil, fmt.Errorf("ping replica: %w", err)
		}
		logger.Info("reads route to the replica")
	}

	db.Usage = startObjectUsage(db.postObjectUsage, func(err error) { logger.Warnf("usage flush: %v", err) }, usageFlushInterval, usageFlushKeys)

	models := []any{
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
		(*Notification)(nil),
		(*SyncEvent)(nil),
		(*DiscipleState)(nil),
		(*UserToken)(nil),
	}
	db.RegisterModel(models...)
	if db.Replica != nil {
		db.Replica.RegisterModel(models...)
	}

	resolveOIDC(logger)

	return db, nil
}

func openPool(dsn string) *bun.DB {
	sqlDB := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithReadTimeout(120*time.Second),
		pgdriver.WithWriteTimeout(120*time.Second),
		pgdriver.WithDialTimeout(30*time.Second),
	))
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(2 * time.Minute)
	sqlDB.SetConnMaxIdleTime(15 * time.Second)
	return bun.NewDB(sqlDB, pgdialect.New())
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
