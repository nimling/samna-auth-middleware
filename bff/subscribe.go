package bff

import (
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/utils"
)

type subscribeRequest struct {
	Email string `json:"email"`
	Feed  string `json:"feed"`
}

// subscribeHandler adds an address to the subscription list of the feed this
// bff serves. The browser sends only the address and the feed path; the bff
// reads the feed from its own site, static or proxied, and forwards it to the
// prophet's client subscription route on the app credentials it already holds.
func subscribeHandler(config Config) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		var body subscribeRequest
		if err := ctx.Bind(&body); err != nil {
			return ctx.String(http.StatusBadRequest, "Bad request")
		}
		address, err := mail.ParseAddress(strings.TrimSpace(body.Email))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "email is not a valid address")
		}
		feedPath := body.Feed
		if feedPath == "" {
			feedPath = "/rss.xml"
		}
		if !strings.HasPrefix(feedPath, "/") || strings.Contains(feedPath, "..") {
			return ctx.String(http.StatusBadRequest, "feed must be a site absolute path")
		}

		feed, err := readFeed(config, feedPath)
		if err != nil {
			ctx.Logger().Errorf("subscribe read feed: %v", err)
			return ctx.String(http.StatusNotFound, "feed not found")
		}

		target := fmt.Sprintf("/api/app/%s/subscription/client?email=%s",
			utils.Cfg().AppSlug, url.QueryEscape(address.Address))
		reqCtx := client.WithContentType(ctx.Request().Context(), "application/rss+xml")
		if err := client.Post(reqCtx, target, feed, nil); err != nil {
			ctx.Logger().Errorf("subscribe forward: %v", err)
			return ctx.String(http.StatusBadGateway, "subscription failed")
		}
		return ctx.NoContent(http.StatusNoContent)
	}
}

func readFeed(config Config, feedPath string) ([]byte, error) {
	if config.ProxyTarget != "" {
		res, err := http.Get(strings.TrimRight(config.ProxyTarget, "/") + feedPath)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("feed responded %d", res.StatusCode)
		}
		return io.ReadAll(res.Body)
	}
	clean := filepath.Clean(feedPath)
	return os.ReadFile(filepath.Join(config.StaticDir, clean))
}
