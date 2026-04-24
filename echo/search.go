package echo

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetSearch(ctx echo.Context) error {
	query := ctx.QueryParam("q")
	if query == "" {
		return respondError(ctx, http.StatusBadRequest, "q is required")
	}

	var objectTypes []string
	if ot := ctx.QueryParam("object_types"); ot != "" {
		objectTypes = strings.Split(ot, ",")
	}

	limit := 20
	if l := ctx.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	offset := 0
	if o := ctx.QueryParam("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		results, err := db.Instance().Search(ctx.Request().Context(), *debugUserID, query, objectTypes, db.ACCESS_LEVEL_GUEST, limit, offset)
		if err != nil {
			ctx.Logger().Errorf("GetSearch: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, results, 0)
	}

	body, err := client.GetSearch(ctx.Request().Context(), query, objectTypes, limit, offset)
	if err != nil {
		ctx.Logger().Errorf("GetSearch: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
