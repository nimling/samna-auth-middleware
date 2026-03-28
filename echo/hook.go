package echo

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/hook"
)

func RegisterWebhooks(group *echo.Group) {
	group.POST("/organization", PostWebhookOrganization)
	group.POST("/location", PostWebhookLocation)
	group.POST("/samna_user", PostWebhookSamnaUser)
	group.POST("/claim", PostWebhookClaim)
	group.POST("/user_claim", PostWebhookUserClaim)
	group.POST("/claim_object", PostWebhookClaimObject)
	group.GET("/health", GetWebhookHealth)
}

func verifyWebhook(ctx echo.Context) bool {
	slug := ctx.Request().Header.Get("SAuth-Client-Slug")
	secret := ctx.Request().Header.Get("SAuth-Client-Secret")
	return client.VerifyRequest(slug, secret)
}

func PostWebhookOrganization(ctx echo.Context) error {
	if !verifyWebhook(ctx) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}
	var org db.Organization
	if err := ctx.Bind(&org); err != nil {
		return ctx.String(http.StatusBadRequest, "Bad Request")
	}
	if err := hook.SyncOrganization(ctx.Request().Context(), &org); err != nil {
		ctx.Logger().Errorf("webhook sync organization: %v", err)
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}
	return ctx.String(http.StatusOK, "OK")
}

func PostWebhookLocation(ctx echo.Context) error {
	if !verifyWebhook(ctx) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}
	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return ctx.String(http.StatusBadRequest, "Bad Request")
	}
	if err := hook.SyncLocation(ctx.Request().Context(), &loc); err != nil {
		ctx.Logger().Errorf("webhook sync location: %v", err)
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}
	return ctx.String(http.StatusOK, "OK")
}

func PostWebhookSamnaUser(ctx echo.Context) error {
	if !verifyWebhook(ctx) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}
	var user db.SamnaUser
	if err := ctx.Bind(&user); err != nil {
		return ctx.String(http.StatusBadRequest, "Bad Request")
	}
	if err := hook.SyncSamnaUser(ctx.Request().Context(), &user); err != nil {
		ctx.Logger().Errorf("webhook sync samna_user: %v", err)
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}
	return ctx.String(http.StatusOK, "OK")
}

func PostWebhookClaim(ctx echo.Context) error {
	if !verifyWebhook(ctx) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}
	var claim db.Claim
	if err := ctx.Bind(&claim); err != nil {
		return ctx.String(http.StatusBadRequest, "Bad Request")
	}
	if err := hook.SyncClaim(ctx.Request().Context(), &claim); err != nil {
		ctx.Logger().Errorf("webhook sync claim: %v", err)
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}
	return ctx.String(http.StatusOK, "OK")
}

func PostWebhookUserClaim(ctx echo.Context) error {
	if !verifyWebhook(ctx) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}
	var uc db.UserClaim
	if err := ctx.Bind(&uc); err != nil {
		return ctx.String(http.StatusBadRequest, "Bad Request")
	}
	if err := hook.SyncUserClaim(ctx.Request().Context(), &uc); err != nil {
		ctx.Logger().Errorf("webhook sync user_claim: %v", err)
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}
	return ctx.String(http.StatusOK, "OK")
}

func PostWebhookClaimObject(ctx echo.Context) error {
	if !verifyWebhook(ctx) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}
	var co db.ClaimObject
	if err := ctx.Bind(&co); err != nil {
		return ctx.String(http.StatusBadRequest, "Bad Request")
	}
	if err := hook.SyncClaimObject(ctx.Request().Context(), &co); err != nil {
		ctx.Logger().Errorf("webhook sync claim_object: %v", err)
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}
	return ctx.String(http.StatusOK, "OK")
}

func GetWebhookHealth(ctx echo.Context) error {
	return ctx.String(http.StatusOK, "OK")
}
