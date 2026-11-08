package main

import (
	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	smw "github.com/nimling/samna-auth-middleware/v2/smw"
)

func main() {
	e := echo.New()

	if _, err := sdb.Init(e.Logger); err != nil {
		e.Logger.Fatal(err)
	}

	api := e.Group("/sam", smw.AppMiddleware)
	sam.RegisterEndpoints(api)

	protected := e.Group("/api", smw.UserAuthMiddleware(nil))
	protected.GET("/ping", func(ctx echo.Context) error {
		return ctx.String(200, "pong")
	})

	e.Logger.Fatal(e.Start(":8080"))
}
