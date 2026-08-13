package echo

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

// GetAnalyticsScript serves the self contained browser recorder so a page with
// no build step can load it from one script tag:
//
//	<script src="/analytics/script.js" data-endpoint=""></script>
//
// The file is the analytics.global.js the typescript package ships. Point
// SAUTH_ANALYTICS_SCRIPT at it to enable this route; without it the route
// reports that the consumer serves the bundle itself.
func GetAnalyticsScript(ctx echo.Context) error {
	path := os.Getenv("SAUTH_ANALYTICS_SCRIPT")
	if path == "" {
		return ctx.NoContent(http.StatusNotFound)
	}
	if _, err := os.Stat(path); err != nil {
		ctx.Logger().Errorf("GetAnalyticsScript: %v", err)
		return ctx.NoContent(http.StatusNotFound)
	}
	ctx.Response().Header().Set(echo.HeaderContentType, "text/javascript; charset=utf-8")
	ctx.Response().Header().Set("Cache-Control", "public, max-age=300")
	return ctx.File(path)
}
