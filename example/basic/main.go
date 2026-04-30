package main

import (
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
	"github.com/nimling/samna-auth-middleware/sync"
)

func main() {
	godotenv.Load()

	e := echo.New()

	db.RegisterSyncFunc(sync.PullSnapshot)
	db.Init(e.Logger)

	e.Use(middleware.UserAuthMiddleware(nil))

	e.GET("/whoami", func(ctx echo.Context) error {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		return ctx.JSON(200, map[string]any{"user_id": userID})
	})

	e.Logger.Fatal(e.Start(":8080"))
}
