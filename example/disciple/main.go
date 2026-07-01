package main

import (
	"context"

	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	smw "github.com/nimling/samna-auth-middleware/smw"
)

func main() {
	e := echo.New()

	if _, err := sdb.Init(e.Logger); err != nil {
		e.Logger.Fatal(err)
	}

	api := e.Group("", smw.UserAuthMiddleware(nil))
	sam.RegisterEndpoints(api)

	sam.SyncStart(context.Background())

	e.Logger.Fatal(e.Start(":8080"))
}
