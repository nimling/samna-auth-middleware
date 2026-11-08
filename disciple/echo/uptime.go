package echo

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/uptime"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func statusTarget() string {
	return fmt.Sprintf("/api/app/%s/status", utils.Cfg().AppSlug)
}

func GetUptime(ctx echo.Context) error {
	heartbeat := uptime.GetHeartbeat()
	if heartbeat == nil {
		return respondError(ctx, http.StatusServiceUnavailable, "No heartbeat reported yet")
	}
	return respondJSON(ctx, http.StatusOK, heartbeat, 1)
}

func GetUptimeStatus(ctx echo.Context) error {
	var status uptime.MonitorStatus
	if err := client.Get(ctx.Request().Context(), statusTarget(), &status); err != nil {
		ctx.Logger().Errorf("GetUptimeStatus: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Status service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, status, 1)
}
