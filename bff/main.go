package bff

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	h "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/notification"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/nimling/samna-auth-middleware/v2/uptime"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const (
	SessionPath          = "/session"
	HeaderXForwardedHost = "X-Forwarded-Host"
	shutdownTimeout      = 10 * time.Second
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
	siteGate := smw.UserAuthMiddleware(nil)

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

	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRun()
	if err := sam.Init(runCtx); err != nil {
		return err
	}
	e.Server.RegisterOnShutdown(notification.EndStreams)

	served := make(chan error, 1)
	go func() {
		if err := e.Start(":" + config.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			served <- err
		}
		close(served)
	}()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		e.Logger.Errorf("shutdown: %v", err)
	}
	cancelRun()
	<-sam.Done()
	return sdb.Instance().Close()
}

func GetSession(ctx echo.Context) error {
	target, err := url.Parse(utils.Cfg().Endpoint)
	if err != nil || target.Host == "" {
		ctx.Logger().Errorf("session handoff: SAUTH_ENDPOINT is not a url: %v", err)
		return ctx.String(http.StatusBadGateway, "session handoff failed")
	}
	scheme := ctx.Scheme()
	next := ""
	if nextCookie, nextErr := ctx.Cookie(client.LoginNextCookie); nextErr == nil && strings.HasPrefix(nextCookie.Value, "/") && !strings.HasPrefix(nextCookie.Value, "//") {
		next = nextCookie.Value
		utils.ClearCookie(ctx, client.LoginNextCookie)
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.SetXForwarded()
			r.Out.Header.Set(echo.HeaderXForwardedProto, scheme)
		},
		ModifyResponse: func(res *http.Response) error {
			if next != "" && res.StatusCode >= http.StatusMultipleChoices && res.StatusCode < http.StatusBadRequest && res.Header.Get(echo.HeaderLocation) == "/" {
				res.Header.Set(echo.HeaderLocation, next)
			}
			return nil
		},
	}
	proxy.ServeHTTP(ctx.Response(), ctx.Request())
	return nil
}
