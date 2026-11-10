package smw

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/analytics"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const (
	rateLimitDefault  = 60
	ratePeriodDefault = time.Minute
)

type rateWindow struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu        sync.Mutex
	windows   map[string]*rateWindow
	lastSweep time.Time
}

var rateLimit = &rateLimiter{windows: map[string]*rateWindow{}}

func rateSettings() (int, time.Duration) {
	c := utils.Cfg()
	limit, period := rateLimitDefault, ratePeriodDefault
	if n, err := strconv.Atoi(c.RateLimit); err == nil && n > 0 {
		limit = n
	}
	if d, err := time.ParseDuration(c.RatePeriod); err == nil && d > 0 {
		period = d
	}
	return limit, period
}

func rateKey(ctx echo.Context) string {
	key := ctx.RealIP()
	if cookie, err := ctx.Cookie(analytics.AnonymousCookie); err == nil && cookie.Value != "" {
		key += "|" + cookie.Value
	}
	return key
}

func respondRateLimited(ctx echo.Context, period time.Duration) error {
	ctx.Response().Header().Set("Retry-After", strconv.Itoa(int(period.Seconds())))
	return ctx.String(http.StatusTooManyRequests, "Too Many Requests")
}

func RateLimit(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		limit, period := rateSettings()
		if !rateLimit.allow(rateKey(ctx), limit, period) {
			return respondRateLimited(ctx, period)
		}
		return next(ctx)
	}
}

func RateLimiter(limit int, period time.Duration) echo.MiddlewareFunc {
	l := &rateLimiter{windows: map[string]*rateWindow{}}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if !l.allow(ctx.RealIP(), limit, period) {
				return respondRateLimited(ctx, period)
			}
			return next(ctx)
		}
	}
}

func (l *rateLimiter) allow(key string, limit int, period time.Duration) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= period {
		for k, w := range l.windows {
			if now.After(w.reset) {
				delete(l.windows, k)
			}
		}
		l.lastSweep = now
	}

	w, ok := l.windows[key]
	if !ok || now.After(w.reset) {
		l.windows[key] = &rateWindow{count: 1, reset: now.Add(period)}
		return true
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}
