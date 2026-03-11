package main

import (
	"net/http"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	em "github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	saecho "github.com/nimling/samna-auth-middleware/echo"
	"github.com/nimling/samna-auth-middleware/middleware"
)

func main() {
	_ = godotenv.Load("../../.env")

	e := echo.New()
	e.Logger.SetLevel(log.DEBUG)

	e.Use(em.CORSWithConfig(em.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{echo.GET, echo.PUT, echo.POST, echo.DELETE},
		AllowHeaders: []string{"Authorization", "Content-Type", "Accept", "User-Agent"},
	}))

	e.Use(em.Logger())
	e.Use(em.Recover())

	e.GET("/login", saecho.GetLogin)
	e.GET("/login/:provider", saecho.GetLogin)

	e.Any("*", func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "All good in the hood, fine, sublime")
	}, middleware.AuthMiddlewareEnv())

	if err := e.Start(":3202"); err != nil {
		e.Logger.Fatal(err)
	}
}
