package smw

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

type rateWindow struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
	limit   int
	period  time.Duration
}

// RateLimiter caps how many requests one client address may send to a route
// within period. It is the guard for anonymous routes an app exposes to the
// browser, where no credential identifies the caller.
func RateLimiter(limit int, period time.Duration) echo.MiddlewareFunc {
	l := &rateLimiter{
		windows: map[string]*rateWindow{},
		limit:   limit,
		period:  period,
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if !l.allow(ctx.RealIP()) {
				return ctx.String(http.StatusTooManyRequests, "Too Many Requests")
			}
			return next(ctx)
		}
	}
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	for k, w := range l.windows {
		if now.After(w.reset) {
			delete(l.windows, k)
		}
	}

	w, ok := l.windows[key]
	if !ok || now.After(w.reset) {
		l.windows[key] = &rateWindow{count: 1, reset: now.Add(l.period)}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}
