package echo_test

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
	"github.com/nimling/samna-auth-middleware/sync"
)

var (
	testEcho    *echo.Echo
	debugUserID uuid.UUID
	rootOrgID   uuid.UUID
)

func InitTestEnv() error {
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")

	required := []string{
		"PGHOST", "PGDATABASE", "PGPORT", "PGUSER", "PGPASSWORD",
		"SAUTH_ENDPOINT", "SAUTH_APP_SLUG", "SAUTH_APP_SECRET",
		"SAUTH_REDIRECT_URI", "SAUTH_PAGE_REDIRECT",
	}
	for _, key := range required {
		if os.Getenv(key) == "" {
			return fmt.Errorf("%s not set", key)
		}
	}

	if os.Getenv("SAUTH_DEBUG_USER_ID") == "" {
		os.Setenv("SAUTH_DEBUG_USER_ID", uuid.New().String())
	}
	parsed, err := uuid.Parse(os.Getenv("SAUTH_DEBUG_USER_ID"))
	if err != nil {
		return fmt.Errorf("SAUTH_DEBUG_USER_ID not a uuid: %w", err)
	}
	debugUserID = parsed
	return nil
}

func InitTestEcho() *echo.Echo {
	e := echo.New()
	e.Logger = loggerAdapter{log.New("echo-test")}

	db.RegisterSyncFunc(sync.PullSnapshot)
	db.Init(e.Logger)

	middleware.Auth(nil)

	return e
}

func ResolveRootOrg(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.Instance().NewRaw(`
		SELECT id FROM claimius.organization
		WHERE app_id = claimius.get_disciple_app_id()
		  AND sa_level = 0
		  AND sa_deleted_at IS NULL
		ORDER BY sa_created_at ASC
		LIMIT 1
	`).Scan(ctx, &id)
	return id, err
}

type loggerAdapter struct {
	*log.Logger
}

func (l loggerAdapter) Output() io.Writer                         { return os.Stderr }
func (l loggerAdapter) SetOutput(w io.Writer)                     {}
func (l loggerAdapter) Prefix() string                            { return "" }
func (l loggerAdapter) SetPrefix(p string)                        {}
func (l loggerAdapter) Level() log.Lvl                            { return log.WARN }
func (l loggerAdapter) SetLevel(v log.Lvl)                        {}
func (l loggerAdapter) SetHeader(h string)                        {}
func (l loggerAdapter) Print(i ...interface{})                    {}
func (l loggerAdapter) Printf(format string, args ...interface{}) {}
func (l loggerAdapter) Printj(j log.JSON)                         {}
func (l loggerAdapter) Debug(i ...interface{})                    {}
func (l loggerAdapter) Debugf(format string, args ...interface{}) {}
func (l loggerAdapter) Debugj(j log.JSON)                         {}
func (l loggerAdapter) Info(i ...interface{})                     {}
func (l loggerAdapter) Infof(format string, args ...interface{})  {}
func (l loggerAdapter) Infoj(j log.JSON)                          {}
func (l loggerAdapter) Warn(i ...interface{})                     {}
func (l loggerAdapter) Warnf(format string, args ...interface{})  {}
func (l loggerAdapter) Warnj(j log.JSON)                          {}
func (l loggerAdapter) Error(i ...interface{})                    { fmt.Fprintln(os.Stderr, i...) }
func (l loggerAdapter) Errorf(format string, args ...interface{}) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
func (l loggerAdapter) Errorj(j log.JSON)                         {}
func (l loggerAdapter) Fatal(i ...interface{})                    { panic(fmt.Sprint(i...)) }
func (l loggerAdapter) Fatalf(format string, args ...interface{}) { panic(fmt.Sprintf(format, args...)) }
func (l loggerAdapter) Fatalj(j log.JSON)                         {}
func (l loggerAdapter) Panic(i ...interface{})                    { panic(fmt.Sprint(i...)) }
func (l loggerAdapter) Panicf(format string, args ...interface{}) { panic(fmt.Sprintf(format, args...)) }
func (l loggerAdapter) Panicj(j log.JSON)                         {}

var _ echo.Logger = loggerAdapter{}
