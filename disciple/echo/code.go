package echo

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func GetCode(ctx echo.Context) error {
	access, ok := ctx.Get(smw.CodeKey).(*shared.CodeAccess)
	if !ok || access == nil {
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	return respondJSON(ctx, http.StatusOK, access.Code, 1)
}
