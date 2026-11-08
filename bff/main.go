package bff

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/labstack/echo/v4"
	h "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/nimling/samna-auth-middleware/v2/uptime"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const (
	SessionPath          = "/session"
	HeaderXForwardedHost = "X-Forwarded-Host"
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
		config.BasePath = "/api/auth"
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
	auth.GET(SessionPath, GetSession)
	auth.GET("/logout", h.GetLogout)

	sam.RegisterStatus(e.Group(path.Join(path.Dir(config.BasePath), path.Base(uptime.StatusBasePath))))

	gate := smw.UserAuthMiddleware(nil)
	siteGate := smw.UserAuthMiddleware(&smw.UserAuthOptions{Optional: true})

	me := auth.Group("/me", gate)
	me.GET("", h.GetMe)
	me.GET("/claim", h.GetMeClaims)

	subscription := auth.Group("/app/:app_slug/subscription", smw.RateLimiter(5, time.Hour))
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

func GetSession(ctx echo.Context) error {
	target, err := url.Parse(utils.Cfg().Endpoint)
	if err != nil || target.Host == "" {
		ctx.Logger().Errorf("session handoff: SAUTH_ENDPOINT is not a url: %v", err)
		return ctx.String(http.StatusBadGateway, "session handoff failed")
	}
	scheme := ctx.Scheme()
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.SetXForwarded()
			r.Out.Header.Set(echo.HeaderXForwardedProto, scheme)
		},
	}
	proxy.ServeHTTP(ctx.Response(), ctx.Request())
	return nil
}
