package smw

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/nimtel"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

func TelemetryMiddleware() echo.MiddlewareFunc {
	return otelecho.Middleware(utils.Cfg().AppSlug)
}

func StampOrganization(ctx echo.Context, rootID, ownerID uuid.UUID) {
	req := ctx.Request()
	ctx.SetRequest(req.WithContext(nimtel.Organization(req.Context(), rootID, ownerID)))
}
