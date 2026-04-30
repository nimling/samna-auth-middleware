//go:build example

package main

import (
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/db"
	saecho "github.com/nimling/samna-auth-middleware/echo"
	"github.com/nimling/samna-auth-middleware/middleware"
	"github.com/nimling/samna-auth-middleware/sync"
)

func main() {
	godotenv.Load()

	e := echo.New()

	db.RegisterSyncFunc(sync.PullSnapshot)
	db.Init(e.Logger)

	auth := middleware.UserAuthMiddleware(nil)

	e.GET("/login", saecho.GetLogin)
	e.GET("/login/:provider", saecho.GetLogin)
	e.GET("/logout", saecho.GetLogout)

	api := e.Group("/api", auth)

	me := api.Group("/me")
	me.GET("", saecho.GetMe)
	me.PUT("", saecho.PutMe)
	me.GET("/claims", saecho.GetMeClaims)
	me.GET("/relations", saecho.GetMeRelations)
	me.GET("/modules", saecho.GetMeModules)
	me.GET("/organizations", saecho.GetMeOrganizations)
	me.GET("/locations", saecho.GetMeLocations)
	me.GET("/apps", saecho.GetMeApps)
	me.GET("/apps/:appId", saecho.GetMeApp)
	me.POST("/assets", saecho.PostMeAsset)
	me.DELETE("/assets/:assetId", saecho.DeleteMeAsset)

	users := api.Group("/users")
	users.GET("", saecho.GetUsers)
	users.GET("/:userId", saecho.GetUser)
	users.POST("/:userId", saecho.PostUser)
	users.PUT("/:userId", saecho.PutUser)
	users.DELETE("/:userId", saecho.DeleteUser)
	users.GET("/:userId/claims", saecho.GetUserClaims)
	users.GET("/:userId/relations", saecho.GetUserRelations)
	users.GET("/:userId/apps", saecho.GetUserApps)
	users.GET("/:userId/apps/:appId", saecho.GetUserApp)
	users.PUT("/:userId/apps/:appId", saecho.PutUserApp)

	orgs := api.Group("/organizations")
	orgs.GET("", saecho.GetOrganizations)
	orgs.GET("/graph", saecho.GetOrganizationGraph)
	orgs.GET("/:orgId", saecho.GetOrganization)
	orgs.POST("", saecho.PostOrganization)
	orgs.PUT("/:orgId", saecho.PutOrganization)
	orgs.DELETE("/:orgId", saecho.DeleteOrganization)
	orgs.GET("/:orgId/claims", saecho.GetOrganizationClaims)
	orgs.GET("/:orgId/modules", saecho.GetOrganizationModules)

	locations := api.Group("/locations")
	locations.GET("", saecho.GetLocations)
	locations.GET("/:locationId", saecho.GetLocation)
	locations.POST("", saecho.PostLocation)
	locations.PUT("/:locationId", saecho.PutLocation)
	locations.DELETE("/:locationId", saecho.DeleteLocation)
	locations.GET("/:locationId/claims", saecho.GetLocationClaims)

	claims := api.Group("/claims")
	claims.GET("", saecho.GetClaims)
	claims.GET("/graph", saecho.GetClaimGraph)
	claims.GET("/:claimId", saecho.GetClaim)
	claims.POST("", saecho.PostClaim)
	claims.PUT("/:claimId", saecho.PutClaim)
	claims.DELETE("/:claimId", saecho.DeleteClaim)
	claims.GET("/:claimId/audit", saecho.GetClaimAudit)
	claims.POST("/:claimId/objects", saecho.PostClaimObject)
	claims.DELETE("/:claimId/objects/:objectId", saecho.DeleteClaimObject)
	claims.POST("/:claimId/users", saecho.PostClaimUser)
	claims.DELETE("/:claimId/users/:userId", saecho.DeleteClaimUser)

	assets := api.Group("/assets")
	assets.GET("", saecho.GetAssets)
	assets.GET("/:assetId", saecho.GetAsset)
	assets.GET("/:assetId/data", saecho.GetAssetData)
	assets.POST("", saecho.PostAsset)
	assets.POST("/:assetId/upload", saecho.PostAssetUpload)
	assets.PUT("/:assetId", saecho.PutAsset)
	assets.DELETE("/:assetId", saecho.DeleteAsset)

	relations := api.Group("/relations")
	relations.GET("", saecho.GetRelations)
	relations.GET("/:relationId", saecho.GetRelation)
	relations.POST("", saecho.PostRelation)
	relations.PUT("/:relationId", saecho.PutRelation)
	relations.DELETE("/:relationId", saecho.DeleteRelation)

	api.GET("/search", saecho.GetSearch)
	api.GET("/modules", saecho.GetModules)

	fields := api.Group("/fields")
	fields.GET("", saecho.GetObjectFields)
	fields.GET("/:fieldId", saecho.GetObjectField)
	fields.POST("", saecho.PostObjectField)
	fields.PUT("/:fieldId", saecho.PutObjectField)
	fields.DELETE("/:fieldId", saecho.DeleteObjectField)
	fields.GET("/:fieldId/value", saecho.GetUserFields)
	fields.POST("/:fieldId/value", saecho.PostUserField)
	fields.PUT("/:fieldId/value/:valueId", saecho.PutUserField)
	fields.DELETE("/:fieldId/value/:valueId", saecho.DeleteUserField)

	syncGroup := e.Group("/sync")
	syncGroup.POST("", saecho.PostSync)

	e.Logger.Fatal(e.Start(":8080"))
}
