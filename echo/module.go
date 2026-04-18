package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetModules(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetModules(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetModules: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganizationModules(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetOrganizationModules(ctx.Request().Context(), orgID)
	if err != nil {
		ctx.Logger().Errorf("GetOrganizationModules: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
