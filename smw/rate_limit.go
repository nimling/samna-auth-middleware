package smw

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

type rateWindow struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu        sync.Mutex
	windows   map[string]*rateWindow
	limit     int
	period    time.Duration
	lastSweep time.Time
}

func RateLimiter(limit int, period time.Duration) echo.MiddlewareFunc {
	l := &rateLimiter{
		windows: map[string]*rateWindow{},
		limit:   limit,
		period:  period,
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if !l.allow(ctx.RealIP()) {
				ctx.Response().Header().Set("Retry-After", strconv.Itoa(int(l.period.Seconds())))
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

	if now.Sub(l.lastSweep) >= l.period {
		for k, w := range l.windows {
			if now.After(w.reset) {
				delete(l.windows, k)
			}
		}
		l.lastSweep = now
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
