package smw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/smw"
)

func rateRequest(t *testing.T, handler echo.HandlerFunc, mw echo.MiddlewareFunc, ip string) int {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", ip)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	if err := mw(handler)(ctx); err != nil {
		e.HTTPErrorHandler(err, ctx)
	}
	return rec.Code
}

func TestRateLimiter_CapsPerAddress(t *testing.T) {
	mw := smw.RateLimiter(3, time.Minute)
	ok := func(ctx echo.Context) error { return ctx.NoContent(http.StatusOK) }

	for i := 0; i < 3; i++ {
		if code := rateRequest(t, ok, mw, "10.0.0.1"); code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i+1, code)
		}
	}
	if code := rateRequest(t, ok, mw, "10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("fourth request: want 429, got %d", code)
	}
}

func TestRateLimiter_IsolatesAddresses(t *testing.T) {
	mw := smw.RateLimiter(1, time.Minute)
	ok := func(ctx echo.Context) error { return ctx.NoContent(http.StatusOK) }

	if code := rateRequest(t, ok, mw, "10.0.0.1"); code != http.StatusOK {
		t.Fatalf("first address: want 200, got %d", code)
	}
	if code := rateRequest(t, ok, mw, "10.0.0.2"); code != http.StatusOK {
		t.Fatalf("second address: want 200, got %d", code)
	}
	if code := rateRequest(t, ok, mw, "10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("first address again: want 429, got %d", code)
	}
}

func TestRateLimiter_WindowExpires(t *testing.T) {
	mw := smw.RateLimiter(1, 20*time.Millisecond)
	ok := func(ctx echo.Context) error { return ctx.NoContent(http.StatusOK) }

	if code := rateRequest(t, ok, mw, "10.0.0.9"); code != http.StatusOK {
		t.Fatalf("first request: want 200, got %d", code)
	}
	if code := rateRequest(t, ok, mw, "10.0.0.9"); code != http.StatusTooManyRequests {
		t.Fatalf("second request inside window: want 429, got %d", code)
	}
	time.Sleep(25 * time.Millisecond)
	if code := rateRequest(t, ok, mw, "10.0.0.9"); code != http.StatusOK {
		t.Fatalf("request after window: want 200, got %d", code)
	}
}
