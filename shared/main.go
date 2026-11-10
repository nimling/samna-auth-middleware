package shared

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/pkg/encryption"
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
	listen  *listen

	RouteToProphet bool
	Prophet        bool
	Retention      func(context.Context) Retention
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
	if db == d.DB && d.RouteToProphet {
		if _, r, fresh := client.WithRequestSeq(ctx); !fresh {
			if target := r.Get(); !target.IsZero() {
				if err := d.WaitForSeq(ctx, target, r.Timeout); err != nil {
					d.Logger.Warnf("routed write %s not yet applied, reading anyway: %v", target, err)
				}
			}
		}
	}
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
var ErrNoDiscipleState = errors.New("claimius.disciple_state has no row: the service must call claimius.upsert_disciple_state at setup")
var ErrSecretExpired = errors.New("secret expired")
var ErrRoutedSet = errors.New("a routed update carries a Set expression the prophet cannot apply: set the field on the model, or use NewDelete for a soft delete")
var ErrRoutedNoRecord = errors.New("a routed update or delete carries no record: populate the model's key or call ID, the record is never read from a Where expression")

func (d *DB) GetApp(ctx context.Context) (*SamnaApp, error) {
	if seed := utils.Cfg().DebugSeed; seed != "" && utils.GetDebugMode() != utils.DebugModeNone {
		app, err := d.GetAppEnv(ctx)
		if err != nil {
			return nil, err
		}
		app.PrivateSeed = seed
		return app, nil
	}
	if d.RouteToProphet {
		app, err := d.GetAppEnv(ctx)
		if err != nil {
			return nil, err
		}
		seed, err := client.GetAppSeed(ctx)
		if err != nil {
			return nil, err
		}
		app.PrivateSeed = seed.PrivateSeed
		app.OwnerID = seed.OwnerID
		return app, nil
	}
	var app SamnaApp
	if err := d.Read(ctx, func(ctx context.Context, db *bun.DB) error {
		return db.NewRaw(`SELECT * FROM claimius.get_app() LIMIT 1`).Scan(ctx, &app)
	}); err != nil {
		return nil, fmt.Errorf("no claimius app registered: %w", err)
	}
	seed, err := encryption.UnwrapAtRest(app.PrivateSeed)
	if err != nil {
		return nil, fmt.Errorf("open app seed: %w", err)
	}
	app.PrivateSeed = seed
	return &app, nil
}

var initiateApps atomic.Pointer[[]SamnaApp]

func GetInitiateApps(ctx context.Context) ([]SamnaApp, error) {
	if utils.GetDebugMode() != utils.DebugModeNone {
		apps := []SamnaApp{}
		for _, pair := range strings.Split(utils.Cfg().DebugInitiateApps, ",") {
			slug, origin, ok := strings.Cut(strings.TrimSpace(pair), "=")
			callback, err := url.Parse(utils.Cfg().RedirectURI)
			if !ok || slug == "" || origin == "" || err != nil {
				continue
			}
			query := callback.Query()
			query.Set("app", slug)
			callback.RawQuery = query.Encode()
			apps = append(apps, SamnaApp{Slug: slug, RedirectUris: []string{origin, callback.String()}})
		}
		return apps, nil
	}
	if apps := initiateApps.Load(); apps != nil {
		return *apps, nil
	}
	return PutInitiateApps(ctx)
}

func PutInitiateApps(ctx context.Context) ([]SamnaApp, error) {
	apps := []SamnaApp{}
	if err := client.Get(ctx, "app/initiate", &apps); err != nil {
		return nil, fmt.Errorf("get initiate apps from the prophet: %w", err)
	}
	initiateApps.Store(&apps)
	return apps, nil
}

func GetInitiateApp(ctx context.Context, slug string) (*SamnaApp, error) {
	apps, err := GetInitiateApps(ctx)
	if err != nil {
		return nil, err
	}
	for i := range apps {
		if apps[i].Slug == slug && apps[i].GetCallbackURI() != "" {
			return &apps[i], nil
		}
	}
	return nil, ErrNotFound
}

func GetOriginInitiateApp(ctx context.Context, origin string) (*SamnaApp, error) {
	apps, err := GetInitiateApps(ctx)
	if err != nil {
		return nil, err
	}
	for i := range apps {
		if apps[i].GetCallbackURI() != "" && slices.Contains(apps[i].GetOrigins(), origin) {
			return &apps[i], nil
		}
	}
	return nil, ErrNotFound
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
	db := &DB{DB: openPool(dsn), Logger: logger, ConnStr: dsn, listen: &listen{subs: make(map[string]map[chan *pq.Notification]struct{})}}
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
		(*ObjectAsset)(nil),
		(*AppCost)(nil),
		(*UserObject)(nil),
		(*UserUsers)(nil),
		(*Notification)(nil),
		(*SyncEvent)(nil),
		(*DiscipleState)(nil),
		(*UserIdentity)(nil),
	}
	db.RegisterModel(models...)
	if db.Replica != nil {
		db.Replica.RegisterModel(models...)
	}

	resolveOIDC(logger)

	return db, nil
}

func (d *DB) Close() error {
	d.Usage.close()
	if d.Replica != nil {
		if err := d.Replica.Close(); err != nil {
			return err
		}
	}
	return d.DB.Close()
}

func openPool(dsn string) *bun.DB {
	sqlDB := sql.OpenDB(codeHashConnector{pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithReadTimeout(120*time.Second),
		pgdriver.WithWriteTimeout(120*time.Second),
		pgdriver.WithDialTimeout(30*time.Second),
	)})
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(2 * time.Minute)
	sqlDB.SetConnMaxIdleTime(15 * time.Second)
	return bun.NewDB(sqlDB, pgdialect.New())
}

func resolveOIDC(logger echo.Logger) {
	if utils.GetDebugMode() != utils.DebugModeNone {
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

func ModelType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

func (d *DB) GetSeq(ctx context.Context) (client.SyncCursor, error) {
	var row DiscipleState
	err := d.NewSelect().Model(&row).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return client.SyncCursor{}, ErrNoDiscipleState
	}
	if err != nil {
		return client.SyncCursor{}, err
	}
	return row.Cursor(), nil
}

func (d *DB) WaitForSeq(ctx context.Context, target client.SyncCursor, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		seq, err := d.GetSeq(ctx)
		if err != nil && ctx.Err() == nil {
			return err
		}
		if err == nil && !seq.Less(target) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for seq %s: %w", target, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func LeadSync(ctx context.Context, db *DB, key string, interval time.Duration, work func(context.Context)) {
	for {
		conn, err := takeSyncLock(ctx, db, key)
		if err != nil && ctx.Err() == nil {
			db.Logger.Errorf("disciple sync: take sync lock: %v", err)
		}
		if conn != nil {
			holdSyncLock(ctx, conn, interval, work)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func takeSyncLock(ctx context.Context, db *DB, key string) (*bun.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var held bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtextextended(?, 0))", key).Scan(&held); err != nil {
		discardConn(&conn)
		return nil, err
	}
	if !held {
		discardConn(&conn)
		return nil, nil
	}
	return &conn, nil
}

func holdSyncLock(ctx context.Context, conn *bun.Conn, interval time.Duration, work func(context.Context)) {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work(workCtx)
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			discardConn(conn)
			return
		case <-ticker.C:
			checkCtx, stop := context.WithTimeout(ctx, interval)
			_, err := conn.ExecContext(checkCtx, "SELECT 1")
			stop()
			if err != nil {
				cancel()
				<-done
				discardConn(conn)
				return
			}
		}
	}
}

func discardConn(conn *bun.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

type codeHashConnector struct {
	driver.Connector
}

func (c codeHashConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &codeHashConn{Conn: conn.(*pgdriver.Conn)}, nil
}

type codeHashConn struct {
	*pgdriver.Conn
	hash string
}

var codeHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (c *codeHashConn) carry(ctx context.Context) error {
	hash := CodeHashFrom(ctx)
	if !codeHashPattern.MatchString(hash) {
		hash = ""
	}
	if hash == c.hash {
		return nil
	}
	if _, err := c.Conn.ExecContext(ctx, "SELECT set_config('claimius.samk', '"+hash+"', false)", nil); err != nil {
		return err
	}
	c.hash = hash
	return nil
}

func (c *codeHashConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.carry(ctx); err != nil {
		return nil, err
	}
	return c.Conn.ExecContext(ctx, query, args)
}

func (c *codeHashConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.carry(ctx); err != nil {
		return nil, err
	}
	return c.Conn.QueryContext(ctx, query, args)
}

func (c *codeHashConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.Conn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &codeHashStmt{Stmt: stmt, conn: c}, nil
}

type codeHashStmt struct {
	driver.Stmt
	conn *codeHashConn
}

func (s *codeHashStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.conn.carry(ctx); err != nil {
		return nil, err
	}
	return s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
}

func (s *codeHashStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := s.conn.carry(ctx); err != nil {
		return nil, err
	}
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
}

func (c *codeHashConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := c.carry(ctx); err != nil {
		return nil, err
	}
	return c.Conn.BeginTx(ctx, opts)
}
