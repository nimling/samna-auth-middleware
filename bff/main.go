package bff

import (
	"context"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/labstack/echo/v4"
	h "github.com/nimling/samna-auth-middleware/disciple/echo"
	sam "github.com/nimling/samna-auth-middleware/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/smw"
)

type Config struct {
	Port        string
	BasePath    string
	StaticDir   string
	ProxyTarget string
}

func ConfigFromEnv() Config {
	config := Config{
		Port:        os.Getenv("SAUTH_BFF_PORT"),
		BasePath:    os.Getenv("SAUTH_BFF_BASE_PATH"),
		StaticDir:   os.Getenv("SAUTH_BFF_STATIC_DIR"),
		ProxyTarget: os.Getenv("SAUTH_BFF_PROXY_TARGET"),
	}
	if config.Port == "" {
		config.Port = "4300"
	}
	if config.BasePath == "" {
		config.BasePath = "/api/sauth"
	}
	return config
}

func Serve(ctx context.Context, config Config) error {
	e := echo.New()

	if _, err := sdb.Init(e.Logger); err != nil {
		return err
	}

	auth := e.Group(config.BasePath)
	auth.GET("/login", h.GetLogin)
	auth.GET("/callback", h.GetCallback)
	auth.GET("/logout", h.GetLogout)

	gate := smw.UserAuthMiddleware(nil)
	siteGate := smw.UserAuthMiddleware(&smw.UserAuthOptions{Optional: true})

	me := auth.Group("/me", gate)
	me.GET("", h.GetMe)
	me.GET("/claim", h.GetMeClaims)

	subscription := auth.Group("/subscription", smw.RateLimiter(5, time.Hour))
	subscription.POST("/subscribe", PostSubscription(config))
	subscription.DELETE("/subscribe", DeleteSubscription)

	if config.ProxyTarget != "" {
		target, err := url.Parse(config.ProxyTarget)
		if err != nil {
			return err
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		e.GET("/*", echo.WrapHandler(proxy), siteGate)
	} else {
		staticDir := config.StaticDir
		e.GET("/*", func(c echo.Context) error {
			filePath := staticDir + c.Request().URL.Path
			if _, statErr := os.Stat(filePath); statErr == nil {
				return c.File(filePath)
			}
			return c.File(staticDir + "/index.html")
		}, siteGate)
	}

	sam.SyncStart(ctx)

	return e.Start(":" + config.Port)
}
