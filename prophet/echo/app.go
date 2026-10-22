package echo

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/encryption"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

func GetSamApp(ctx echo.Context) error {
	app, ok := ctx.Get("app").(SamnaApp)
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "Missing app context")
	}

	seed, err := encryption.UnwrapAtRest(app.PrivateSeed)
	if err != nil {
		ctx.Logger().Errorf("GetSamApp open seed of app %s: %v", app.Slug, err)
		return respond.Error(ctx, http.StatusInternalServerError, "App seed unavailable")
	}

	app.PrivateKey = ""
	app.PrivateSeed = seed
	app.AppSecret = nil

	ctx.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return respond.JSON(ctx, http.StatusOK, app, 1)
}

func GetSamInitiateApps(ctx echo.Context) error {
	app, ok := ctx.Get("app").(SamnaApp)
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "Missing app context")
	}

	apps := []SamnaApp{}
	err := sdb.Instance().NewSelect().
		Model(&apps).
		ExcludeColumn("private_key", "private_seed", "app_secret", "sa_deleted_at").
		Where("sa_root_id = ?", app.ID).
		Where("id <> ?", app.ID).
		Where("sa_deleted_at IS NULL").
		Order("slug").
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetSamInitiateApps of app %s: %v", app.Slug, err)
		return respond.Error(ctx, http.StatusInternalServerError, "App list query failed")
	}

	return respond.JSON(ctx, http.StatusOK, apps, len(apps))
}
