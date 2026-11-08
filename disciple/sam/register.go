package sam

import (
	"github.com/labstack/echo/v4"
	h "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
	"github.com/nimling/samna-auth-middleware/v2/uptime"
)

func RegisterEndpoints(g *echo.Group) {
	g.GET("/login", h.GetLogin)
	g.GET("/callback", h.GetCallback)
	g.GET("/logout", h.GetLogout)

	me := g.Group("/me")
	me.GET("", h.GetMe)
	me.PUT("", h.PutMe)
	me.PATCH("", h.PatchMe)
	me.GET("/claim", h.GetMeClaims)
	me.GET("/relation", h.GetMeRelations)
	me.GET("/organization", h.GetMeOrganizations)
	me.GET("/location", h.GetMeLocations)
	me.GET("/asset", h.GetMeAssets)
	me.GET("/audit", h.GetMeAudit)

	g.GET("/asset/:id/data", h.GetAssetData)

	notif := g.Group("/notification")
	notif.GET("", h.GetNotifications)
	notif.PUT("/:id/read", h.PutNotificationRead)
	notif.PUT("/:id/closed", h.PutNotificationClosed)
	notif.DELETE("/:id", h.DeleteNotification)

	RegisterCapture(g)
}

func RegisterStatus(g *echo.Group) {
	g.GET(uptime.HealthPath, h.GetHealth)
}

func RegisterCapture(g *echo.Group) {
	analytics := g.Group("/analytics")
	analytics.GET("/consent", h.GetConsent)
	analytics.POST("/consent", h.PostConsent)
	analytics.POST("", h.PostAnalytics)
	analytics.POST("/capture", h.PostAnalyticsCapture)
}

func RegisterCaptureRead(g *echo.Group) {
	analytics := g.Group("/analytics")
	analytics.GET("/app", h.GetAnalyticsApps)
	analytics.GET("/event", h.GetAnalyticsEvents)
	analytics.GET("/session", h.GetAnalyticsSessions)
	analytics.GET("/capture", h.GetAnalyticsCapture)
	analytics.GET("/heat", h.GetAnalyticsHeat)
	analytics.GET("/summary", h.GetAnalyticsSummary)
}
