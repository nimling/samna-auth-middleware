package main

import (
	"net/http"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	em "github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/nimling/samna-auth-middleware/middleware"
)

func main() {
	_ = godotenv.Load(".env")

	// Configure echo
	e := echo.New()
	e.Logger.SetLevel(log.DEBUG)

	// Configure CORS
	e.Use(em.CORSWithConfig(em.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{echo.GET, echo.PUT, echo.POST, echo.DELETE},
		AllowHeaders: []string{"Authorization", "Content-Type", "Accept", "User-Agent"}, // TODO:: From env?
	}))

	e.Use(em.Logger())
	e.Use(em.Recover())

	e.GET("/login", middleware.RedirectLogin)

	// Reverse Proxy for all other routes
	e.Any("*", func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "All good in the hood, fine, sublime")
	}, middleware.AuthMiddlewareEnv())

	// Start server
	if err := e.Start(":3202"); err != nil {
		e.Logger.Fatal(err)
	}
}
