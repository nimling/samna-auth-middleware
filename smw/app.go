package smw

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/encryption"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun"
)

func AppMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		ctx.Set("dry_run", ctx.Request().Header.Get("X-Is-Dry-Run") == "true")

		appSlug := ctx.Request().Header.Get("SAuth-App-Slug")
		appSecret := ctx.Request().Header.Get("SAuth-App-Secret")

		if appSlug == "" || appSecret == "" {
			basicSlug, basicSecret, err := parseBasicAuth(ctx.Request())
			if err == nil {
				appSlug = basicSlug
				appSecret = basicSecret
				ctx.Logger().Infof("App credentials from Basic Auth for slug=%s", appSlug)
			}
		}

		if appSlug == "" || appSecret == "" {
			if cookie, err := ctx.Cookie("sauth_app_slug"); err == nil && cookie.Value != "" {
				appSlug = cookie.Value
			}
			if cookie, err := ctx.Cookie("sauth_app_secret"); err == nil && cookie.Value != "" {
				appSecret = cookie.Value
			}
			if appSlug != "" && appSecret != "" {
				ctx.Logger().Infof("App credentials from cookies for slug=%s", appSlug)
			}
		}

		if appSlug == "" || appSecret == "" {
			ctx.Logger().Infof("App credentials missing in request")
			return ctx.NoContent(http.StatusUnauthorized)
		}

		if utils.IsSystemApp(appSlug) {
			appSlug = utils.Cfg().AppSlug
		}
		ctx.Set("app_slug", appSlug)
		ctx.Set("app_secret", appSecret)

		var app pecho.SamnaApp
		err := sdb.Instance().Read(ctx.Request().Context(), func(rctx context.Context, db *bun.DB) error {
			return db.NewSelect().
				Model(&app).
				ExcludeColumn("sa_deleted_at").
				Where("slug = ?", appSlug).
				Where("sa_deleted_at IS NULL").
				Limit(1).
				Scan(rctx)
		})
		if err != nil {
			ctx.Logger().Infof("App not found for slug=%s: %v", appSlug, err)
			return ctx.NoContent(http.StatusUnauthorized)
		}

		if app.Status != pecho.APP_STATUS_ACTIVE {
			ctx.Logger().Infof("App not active for slug=%s status=%s", appSlug, app.Status)
			return ctx.NoContent(http.StatusNotFound)
		}

		masterKey, err := getAppKey(app)
		if err != nil {
			ctx.Logger().Infof("Failed to import master key for slug=%s: %v", appSlug, err)
			return ctx.NoContent(http.StatusUnauthorized)
		}

		var ownerOrg shared.Organization
		if err := sdb.Instance().Read(ctx.Request().Context(), func(rctx context.Context, db *bun.DB) error {
			return db.NewSelect().
				Model(&ownerOrg).
				Column("private_seed").
				Where("id = ?", app.OwnerID).
				Where("sa_deleted_at IS NULL").
				Limit(1).
				Scan(rctx)
		}); err != nil {
			ctx.Logger().Infof("Owner org not found for slug=%s: %v", appSlug, err)
			return ctx.NoContent(http.StatusUnauthorized)
		}
		orgSeedHex, err := encryption.UnwrapAtRest(ownerOrg.PrivateSeed)
		if err != nil {
			ctx.Logger().Infof("Owner org seed not readable for slug=%s: %v", appSlug, err)
			return ctx.NoContent(http.StatusUnauthorized)
		}
		orgSeed, err := hex.DecodeString(orgSeedHex)
		if err != nil {
			ctx.Logger().Infof("Invalid owner org seed for slug=%s: %v", appSlug, err)
			return ctx.NoContent(http.StatusUnauthorized)
		}

		storedSecret := ""
		if app.AppSecret != nil {
			storedSecret = *app.AppSecret
		}
		if !encryption.ValidateAppSecret(orgSeed, appSecret, storedSecret) {
			ctx.Logger().Infof("Invalid app secret for slug=%s", appSlug)
			return ctx.NoContent(http.StatusUnauthorized)
		}

		ctx.Set("app", app)
		ctx.Set("app_id", app.ID)
		ctx.Set("app_key", *masterKey)
		ctx.Set("app_sync_uri", app.SyncURI)
		ctx.Set("app_internal", utils.IsSystemApp(app.Slug))

		appRedirectURI, err := url.Parse(app.GetPrimaryOrigin())
		if err != nil {
			ctx.Logger().Infof("Failed to parse app redirect uri: %s", err)
			return ctx.NoContent(http.StatusInternalServerError)
		}

		ctx.Set("app_redirect_uri", appRedirectURI)

		return next(ctx)
	}
}

func parseBasicAuth(r *http.Request) (string, string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Basic ") {
		return "", "", fmt.Errorf("authorization header missing or not Basic")
	}

	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
	if err != nil {
		return "", "", fmt.Errorf("decode base64: %v", err)
	}

	parts := strings.SplitN(string(payload), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid basic auth format")
	}

	return parts[0], parts[1], nil
}

var appKeys sync.Map

type appKey struct {
	privateKey  string
	privateSeed string
	key         *encryption.MasterKeyPair
}

func getAppKey(app pecho.SamnaApp) (*encryption.MasterKeyPair, error) {
	if v, ok := appKeys.Load(app.ID); ok {
		cached := v.(appKey)
		if cached.privateKey == app.PrivateKey && cached.privateSeed == app.PrivateSeed {
			return cached.key, nil
		}
	}
	key, err := encryption.ImportMasterKeyPair(app.PrivateKey, app.PrivateSeed)
	if err != nil {
		return nil, err
	}
	appKeys.Store(app.ID, appKey{privateKey: app.PrivateKey, privateSeed: app.PrivateSeed, key: key})
	return key, nil
}
