package sam

import (
	"github.com/labstack/echo/v4"
	h "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

// RegisterEndpoints mounts the full prophet API on the passed group. It carries
// the user, organization, location, claim, asset, app, sync, client, secret,
// search, code, field and guest routes that apps and browsers reach the prophet
// through. A write answers with the sync sequence of its change. A write for an
// app another prophet decides is refused with the prophet that decides it.
func RegisterEndpoints(g *echo.Group) {
	g.Use(smw.SyncSeq, smw.AppHome)

	user := g.Group("/user")
	user.GET("", h.GetUsers)
	user.GET("/:id", h.GetUser)
	user.GET("/:id/asset", h.GetUserAssets)
	user.GET("/:id/claim", h.GetUserClaims)
	user.GET("/:id/user", h.GetUserConnections)
	user.GET("/:id/audit", h.GetUserAudit)
	user.GET("/:id/relation", h.GetUserRelations)
	user.GET("/:id/organization", h.GetUserOrganizations)
	user.GET("/:id/location", h.GetUserLocations)
	user.POST("", h.PostUser)
	user.PUT("/:id", h.PutUser)
	user.PATCH("/:id", h.PutUser)
	user.DELETE("/:id", h.DeleteUser)
	user.POST("/token", h.PostUserToken)
	user.POST("/token/validate", h.PostUserTokenValidate)
	user.POST("/external", h.PostUserExternal)

	relation := g.Group("/relation")
	relation.GET("", h.GetRelations)
	relation.GET("/:id", h.GetRelation)
	relation.POST("", h.PostRelation)
	relation.PUT("/:id", h.PutRelation)
	relation.PATCH("/:id", h.PutRelation)
	relation.DELETE("/:id", h.DeleteRelation)

	org := g.Group("/organization")
	org.GET("", h.GetOrganizations)
	org.GET("/graph", h.GetOrganizationGraph)
	org.GET("/:id", h.GetOrganization)
	org.GET("/:id/asset", h.GetOrganizationAssets)
	org.GET("/:id/claim", h.GetOrganizationClaims)
	org.POST("", h.PostOrganization)
	org.PUT("/:id", h.PutOrganization)
	org.PATCH("/:id", h.PutOrganization)
	org.DELETE("/:id", h.DeleteOrganization)
	org.GET("/:id/audit", h.GetOrganizationAudit)

	loc := g.Group("/location")
	loc.GET("", h.GetLocations)
	loc.GET("/:id", h.GetLocation)
	loc.GET("/:id/asset", h.GetLocationAssets)
	loc.GET("/:id/claim", h.GetLocationClaims)
	loc.POST("", h.PostLocation)
	loc.PUT("/:id", h.PutLocation)
	loc.PATCH("/:id", h.PutLocation)
	loc.DELETE("/:id", h.DeleteLocation)
	loc.GET("/:id/audit", h.GetLocationAudit)

	claim := g.Group("/claim")
	claim.GET("", h.GetClaims)
	claim.GET("/graph", h.GetClaimGraph)
	claim.GET("/:id", h.GetClaim)
	claim.POST("", h.PostClaim)
	claim.PUT("/:id", h.PutClaim)
	claim.PATCH("/:id", h.PutClaim)
	claim.DELETE("/:id", h.DeleteClaim)
	claim.POST("/:claim_id/object", h.PostClaimObject)
	claim.PATCH("/:claim_id/object/:id", h.PatchClaimObject)
	claim.DELETE("/:claim_id/object/:id", h.DeleteClaimObject)
	claim.POST("/:claim_id/user", h.PostClaimUser)
	claim.PATCH("/:claim_id/user/:id", h.PatchClaimUser)
	claim.DELETE("/:claim_id/user/:id", h.DeleteClaimUser)
	claim.GET("/:id/audit", h.GetClaimAudit)

	asset := g.Group("/asset")
	asset.GET("", h.GetAssets)
	asset.GET("/:id", h.GetAsset)
	asset.GET("/:id/data", h.GetAssetData)
	asset.POST("", h.PostAsset)
	asset.POST("/:id", h.PostAssetUpload)
	asset.PUT("/:id", h.PutAsset)
	asset.DELETE("/:id", h.DeleteAsset)

	app := g.Group("/app")
	app.GET("", h.GetSamApp)

	guest := g.Group("/guest")
	guest.POST("", h.PostGuestToken)

	sync := g.Group("/sync")
	sync.GET("/snapshot", h.GetSyncSnapshot)
	sync.GET("/:cursor", h.GetSyncEvents)

	client := g.Group("/samna_client")
	client.GET("", h.GetClients)
	client.GET("/:id", h.GetClient)
	client.POST("", h.PostClient)
	client.PUT("/:id", h.PutClient)
	client.PATCH("/:id", h.PutClient)
	client.DELETE("/:id", h.DeleteClient)
	client.POST("/:id/rotate", h.PostClientRotate)
	client.POST("/:id/claim", h.PostClientClaim)
	client.DELETE("/:id/claim/:claim_id", h.DeleteClientClaim)

	secret := g.Group("/secret")
	secret.GET("", h.GetSecrets)
	secret.GET("/:id", h.GetSecret)
	secret.POST("", h.PostSecret)
	secret.PUT("/:id", h.PutSecret)
	secret.PATCH("/:id", h.PutSecret)
	secret.DELETE("/:id", h.DeleteSecret)

	g.GET("/search", h.GetSearch)
	g.POST("/search", h.PostSearch)

	code := g.Group("/code")
	code.GET("", h.GetCodes)
	code.GET("/:code", h.GetCode)
	code.POST("", h.PostCode)
	code.PUT("/:id", h.PutCode)
	code.DELETE("/:id", h.DeleteCode)

	field := g.Group("/field")
	field.GET("", h.GetObjectFields)
	field.GET("/:id", h.GetObjectField)
	field.POST("", h.PostObjectField)
	field.PUT("/:id", h.PutObjectField)
	field.DELETE("/:id", h.DeleteObjectField)
	field.GET("/:id/value", h.GetUserFields)
	field.POST("/:id/value", h.PostUserField)
	field.PUT("/:id/value/:value_id", h.PutUserField)
	field.DELETE("/:id/value/:value_id", h.DeleteUserField)

	notif := g.Group("/notification")
	notif.GET("", h.GetNotifications)
	notif.PUT("/:id/read", h.PutNotificationRead)
	notif.PUT("/:id/closed", h.PutNotificationClosed)
	notif.DELETE("/:id", h.DeleteNotification)

	consent := g.Group("/consent")
	consent.GET("", h.GetConsent)
	consent.POST("", h.PostConsent)

	stat := g.Group("/analytics")
	stat.POST("", h.PostAnalytics)
	stat.POST("/capture", h.PostAnalyticsCapture)
	stat.GET("/capture", h.GetAnalyticsCapture)
	stat.GET("/heat", h.GetAnalyticsHeat)
	stat.GET("/summary", h.GetAnalyticsSummary)
	stat.GET("/app", h.GetAnalyticsApps)
	stat.GET("/event", h.GetAnalyticsEvents)
	stat.GET("/session", h.GetAnalyticsSessions)
}

// RegisterCapture mounts consent and capture for the prophet's own portal. A
// disciple reaches the same handlers on the sam channel carrying its app
// credentials, so it never mounts this. The prophet answers for itself as the
// system app named by SAUTH_APP_SLUG.
func RegisterCapture(g *echo.Group) {
	analytics := g.Group("/analytics")
	analytics.GET("/consent", h.GetConsent)
	analytics.POST("/consent", h.PostConsent)
	analytics.POST("", h.PostAnalytics)
	analytics.POST("/capture", h.PostAnalyticsCapture)
}

// RegisterCaptureRead mounts the capture read for the prophet's own portal. It
// answers for the signed in reader, across every producer app their analytics
// grant covers.
func RegisterCaptureRead(g *echo.Group) {
	analytics := g.Group("/analytics")
	analytics.GET("/app", h.GetAnalyticsApps)
	analytics.GET("/event", h.GetAnalyticsEvents)
	analytics.GET("/session", h.GetAnalyticsSessions)
	analytics.GET("/capture", h.GetAnalyticsCapture)
	analytics.GET("/heat", h.GetAnalyticsHeat)
	analytics.GET("/summary", h.GetAnalyticsSummary)
}
