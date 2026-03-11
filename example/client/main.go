package main

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	em "github.com/labstack/echo/v4/middleware"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/middleware"
)

func metaFromContext(ctx echo.Context) client.RequestMetadata {
	accessToken, _ := ctx.Get("access_token").(string)
	return client.RequestMetadata{
		AccessToken: accessToken,
		RequestID:   ctx.Response().Header().Get(echo.HeaderXRequestID),
		UserIP:      ctx.RealIP(),
	}
}

func main() {
	_ = godotenv.Load("../../.env")

	e := echo.New()
	e.Use(em.Logger())
	e.Use(em.Recover())

	authMiddleware := middleware.AuthMiddlewareEnv()

	e.GET("/users", func(ctx echo.Context) error {
		meta := metaFromContext(ctx)
		body, err := client.GetUsers(ctx.Request().Context(), meta)
		if err != nil {
			return ctx.String(http.StatusInternalServerError, "Failed to get users")
		}
		defer body.Close()
		return ctx.Stream(http.StatusOK, "application/json", body)
	}, authMiddleware)

	e.GET("/user/:id", func(ctx echo.Context) error {
		userID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid user ID")
		}

		meta := metaFromContext(ctx)
		body, err := client.GetUser(ctx.Request().Context(), meta, userID)
		if err != nil {
			return ctx.String(http.StatusNotFound, "User not found")
		}
		defer body.Close()
		return ctx.Stream(http.StatusOK, "application/json", body)
	}, authMiddleware)

	e.GET("/organizations", func(ctx echo.Context) error {
		meta := metaFromContext(ctx)
		body, err := client.GetOrganizations(ctx.Request().Context(), meta)
		if err != nil {
			return ctx.String(http.StatusInternalServerError, "Failed to get organizations")
		}
		defer body.Close()
		return ctx.Stream(http.StatusOK, "application/json", body)
	}, authMiddleware)

	e.POST("/organization", func(ctx echo.Context) error {
		meta := metaFromContext(ctx)
		body, err := client.PostOrganization(ctx.Request().Context(), meta, nil)
		if err != nil {
			return ctx.String(http.StatusInternalServerError, "Failed to create organization")
		}
		defer body.Close()
		return ctx.Stream(http.StatusCreated, "application/json", body)
	}, authMiddleware)

	e.GET("/locations", func(ctx echo.Context) error {
		meta := metaFromContext(ctx)
		body, err := client.GetLocations(ctx.Request().Context(), meta)
		if err != nil {
			return ctx.String(http.StatusInternalServerError, "Failed to get locations")
		}
		defer body.Close()
		return ctx.Stream(http.StatusOK, "application/json", body)
	}, authMiddleware)

	e.POST("/location", func(ctx echo.Context) error {
		meta := metaFromContext(ctx)
		body, err := client.PostLocation(ctx.Request().Context(), meta, nil)
		if err != nil {
			return ctx.String(http.StatusInternalServerError, "Failed to create location")
		}
		defer body.Close()
		return ctx.Stream(http.StatusCreated, "application/json", body)
	}, authMiddleware)

	e.GET("/claims", func(ctx echo.Context) error {
		meta := metaFromContext(ctx)
		body, err := client.GetClaimGraph(ctx.Request().Context(), meta)
		if err != nil {
			return ctx.String(http.StatusInternalServerError, "Failed to get claims")
		}
		defer body.Close()
		return ctx.Stream(http.StatusOK, "application/json", body)
	}, authMiddleware)

	fmt.Println("Client library example running on :8080")
	e.Logger.Fatal(e.Start(":8080"))
}
