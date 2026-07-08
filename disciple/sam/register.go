package sam

import (
	"github.com/labstack/echo/v4"
	h "github.com/nimling/samna-auth-middleware/disciple/echo"
)

// RegisterEndpoints mounts the disciple browser facing handlers on the passed
// group: the login, callback and logout flow and the me routes. A consumer that
// wants finer control can skip this and mount the handlers from the echo package
// one by one instead.
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

	notif := g.Group("/notification")
	notif.GET("", h.GetNotifications)
	notif.PUT("/:id/read", h.PutNotificationRead)
	notif.PUT("/:id/closed", h.PutNotificationClosed)
	notif.DELETE("/:id", h.DeleteNotification)
}
