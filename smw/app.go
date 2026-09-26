package smw

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/encryption"
	pecho "github.com/nimling/samna-auth-middleware/prophet/echo"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
)

// AppMiddleware is internal. It guards the endpoints apps use to reach the
// prophet, reading the app slug and secret from the request and validating them
// against the stored app row before the handler runs.
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
		err := sdb.Instance().NewSelect().
			Model(&app).
			ExcludeColumn("sa_deleted_at").
			Where("slug = ?", appSlug).
			Where("sa_deleted_at IS NULL").
			Limit(1).
			Scan(ctx.Request().Context())
		if err != nil {
			ctx.Logger().Infof("App not found for slug=%s: %v", appSlug, err)
			return ctx.NoContent(http.StatusUnauthorized)
		}

		if app.Status != pecho.APP_STATUS_ACTIVE {
			ctx.Logger().Infof("App not active for slug=%s status=%s", appSlug, app.Status)
			return ctx.NoContent(http.StatusNotFound)
		}

		masterKey, err := encryption.ImportMasterKeyPair(app.PrivateKey, app.PrivateSeed)
		if err != nil {
			ctx.Logger().Infof("Failed to import master key for slug=%s: %v", appSlug, err)
			return ctx.NoContent(http.StatusUnauthorized)
		}

		var ownerOrg shared.Organization
		if err := sdb.Instance().NewSelect().
			Model(&ownerOrg).
			Column("private_seed").
			Where("id = ?", app.OwnerID).
			Where("sa_deleted_at IS NULL").
			Limit(1).
			Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Infof("Owner org not found for slug=%s: %v", appSlug, err)
			return ctx.NoContent(http.StatusUnauthorized)
		}
		orgSeed, err := hex.DecodeString(ownerOrg.PrivateSeed)
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
