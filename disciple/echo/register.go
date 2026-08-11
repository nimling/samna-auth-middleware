package echo

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

// RegisterAnalytics mounts consent, capture and the browser bundle in one call.
// Mount it outside any group that requires a session: an anonymous visitor has
// to reach consent before they have one, and capture has to work for them too.
//
//	saecho.RegisterAnalytics(e.Group(""))
//
// The disciple needs no analytics.Init. It holds no capture tables and forwards
// every read and write to the prophet on the app key.
func RegisterAnalytics(g *echo.Group) {
	g.GET("/consent", GetConsent)
	g.POST("/consent", PostConsent)
	g.POST("/analytics", PostAnalytics)
	g.GET("/analytics.js", GetAnalyticsScript)
}

// RegisterAnalyticsRead mounts the capture read routes. These answer with what
// was recorded about other people, so unlike RegisterAnalytics they belong on a
// group that requires a session, behind whatever page grant the consumer gates
// its admin surface with.
//
//	saecho.RegisterAnalyticsRead(api.Group("/analytics"))
func RegisterAnalyticsRead(g *echo.Group) {
	g.GET("/event", GetAnalyticsEvents)
	g.GET("/session", GetAnalyticsSessions)
}

// GetAnalyticsScript serves the self contained browser recorder so a page with
// no build step can load it from one script tag:
//
//	<script src="/analytics.js" data-endpoint=""></script>
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
