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
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

type subscribeRequest struct {
	Email string `json:"email"`
	Feed  string `json:"feed"`
	Name  string `json:"name"`
}

type subscribeForward struct {
	Email string `json:"email"`
	Feed  string `json:"feed,omitempty"`
	Name  string `json:"name,omitempty"`
	RSS   string `json:"rss,omitempty"`
}

var feedClient = &http.Client{Timeout: 10 * time.Second}

func subscriptionForward(ctx echo.Context) (*subscribeForward, error) {
	if ctx.Param("app_slug") != utils.Cfg().AppSlug {
		return nil, ctx.String(http.StatusNotFound, "unknown app")
	}
	var body subscribeRequest
	if err := ctx.Bind(&body); err != nil {
		return nil, ctx.String(http.StatusBadRequest, "Bad request")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(body.Email))
	if err != nil {
		return nil, ctx.String(http.StatusBadRequest, "email is not a valid address")
	}
	if body.Feed == "" && body.Name == "" {
		return nil, ctx.String(http.StatusBadRequest, "feed or name is required")
	}
	if body.Feed != "" {
		parsed, parseErr := url.Parse(body.Feed)
		if parseErr != nil || parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") || strings.Contains(parsed.Path, "..") {
			return nil, ctx.String(http.StatusBadRequest, "feed must be a site absolute path or url")
		}
	}
	return &subscribeForward{Email: address.Address, Feed: body.Feed, Name: body.Name}, nil
}

func feedPath(feed string) string {
	parsed, err := url.Parse(feed)
	if err != nil {
		return feed
	}
	return parsed.Path
}

func subscriptionTarget() string {
	return fmt.Sprintf("/api/app/%s/subscription/subscribe", utils.Cfg().AppSlug)
}

func PostSubscription(config Config) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		forward, err := subscriptionForward(ctx)
		if forward == nil {
			return err
		}
		if forward.Feed != "" {
			feed, readErr := readFeed(config, feedPath(forward.Feed))
			if readErr != nil {
				ctx.Logger().Errorf("subscribe read feed: %v", readErr)
			} else {
				forward.RSS = string(feed)
			}
		}
		reqCtx := client.WithClientCredentials(ctx.Request().Context(), client.ClientCredentials{
			ID:     utils.Cfg().ClientID,
			Secret: utils.Cfg().ClientSecret,
		})
		if err := client.Post(reqCtx, subscriptionTarget(), forward, nil); err != nil {
			ctx.Logger().Errorf("subscribe forward: %v", err)
			return ctx.String(http.StatusBadGateway, "subscription failed")
		}
		return ctx.NoContent(http.StatusNoContent)
	}
}

func DeleteSubscription(ctx echo.Context) error {
	forward, err := subscriptionForward(ctx)
	if forward == nil {
		return err
	}
	reqCtx := client.WithClientCredentials(ctx.Request().Context(), client.ClientCredentials{
		ID:     utils.Cfg().ClientID,
		Secret: utils.Cfg().ClientSecret,
	})
	if err := client.Delete(reqCtx, subscriptionTarget(), forward, nil); err != nil {
		ctx.Logger().Errorf("unsubscribe forward: %v", err)
		return ctx.String(http.StatusBadGateway, "subscription failed")
	}
	return ctx.NoContent(http.StatusNoContent)
}

func readFeed(config Config, feedPath string) ([]byte, error) {
	if config.ProxyTarget != "" {
		res, err := feedClient.Get(strings.TrimRight(config.ProxyTarget, "/") + feedPath)
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
