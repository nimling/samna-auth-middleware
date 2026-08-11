package bff

import (
	"context"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	h "github.com/nimling/samna-auth-middleware/disciple/echo"
	sam "github.com/nimling/samna-auth-middleware/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/smw"
	"github.com/nimling/samna-auth-middleware/utils"
)

type Config struct {
	Port              string
	BasePath          string
	StaticDir         string
	ProxyTarget       string
	GuardedPaths      []string
	SubscribeClientID string
	SubscribeSecret   string
}

func ConfigFromEnv() Config {
	config := Config{
		Port:              os.Getenv("SAUTH_BFF_PORT"),
		BasePath:          os.Getenv("SAUTH_BFF_BASE_PATH"),
		StaticDir:         os.Getenv("SAUTH_BFF_STATIC_DIR"),
		ProxyTarget:       os.Getenv("SAUTH_BFF_PROXY_TARGET"),
		GuardedPaths:      splitPaths(os.Getenv("SAUTH_BFF_GUARDED_PATHS")),
		SubscribeClientID: os.Getenv("SAUTH_BFF_SUBSCRIBE_CLIENT_ID"),
		SubscribeSecret:   os.Getenv("SAUTH_BFF_SUBSCRIBE_SECRET"),
	}
	if config.Port == "" {
		config.Port = "4300"
	}
	if config.BasePath == "" {
		config.BasePath = "/api/sauth"
	}
	if len(config.GuardedPaths) == 0 {
		config.GuardedPaths = []string{"/_guarded"}
	}
	return config
}

func splitPaths(raw string) []string {
	paths := []string{}
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

func (c Config) PathGuarded(path string) bool {
	for _, prefix := range c.GuardedPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// postSubscribe adds an address to a feed under the app's subscribe client
// credentials, so a reader with no account subscribes without the secret ever
// reaching the browser. The prophet applies the per app per IP limit the
// client's own claim carries.
func (c Config) postSubscribe(ctx echo.Context) error {
	email := strings.TrimSpace(ctx.QueryParam("email"))
	if email == "" {
		return ctx.String(http.StatusBadRequest, "email is required")
	}

	feed, err := io.ReadAll(io.LimitReader(ctx.Request().Body, 1<<20))
	if err != nil {
		return ctx.String(http.StatusBadRequest, "unreadable feed")
	}

	err = client.PostSubscriptionFeed(
		ctx.Request().Context(),
		utils.Cfg().AppSlug,
		c.SubscribeClientID,
		c.SubscribeSecret,
		email,
		string(feed),
	)
	if err != nil {
		ctx.Logger().Errorf("bff subscribe: %v", err)
		return ctx.String(http.StatusBadGateway, "subscription failed")
	}

	return ctx.NoContent(http.StatusNoContent)
}

// siteGate serves the site under the optional gate so a visitor with no session
// reads public content, and holds every guarded path behind the strict gate.
func (c Config) siteGate(strict echo.MiddlewareFunc, optional echo.MiddlewareFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		guarded := strict(next)
		public := optional(next)
		return func(ctx echo.Context) error {
			if c.PathGuarded(ctx.Request().URL.Path) {
				return guarded(ctx)
			}
			return public(ctx)
		}
	}
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
	siteGate := config.siteGate(gate, smw.UserAuthMiddleware(&smw.UserAuthOptions{Optional: true}))

	me := auth.Group("/me", gate)
	me.GET("", h.GetMe)

	if config.SubscribeClientID != "" && config.SubscribeSecret != "" {
		auth.POST("/subscribe", config.postSubscribe)
	}

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
