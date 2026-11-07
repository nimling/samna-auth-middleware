package smw

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const (
	CodeParam  = client.CodeParam
	CodeHeader = client.CodeHeader
	CodeCookie = shared.CodeCookie
)

const (
	codeFailureLimit  = 20
	codeFailurePeriod = time.Minute
	codeInviteMaxAge  = 600
)

type codeSource int

const (
	codeSourceNone codeSource = iota
	codeSourceQuery
	codeSourceHeader
	codeSourceCookie
)

var codeFailures = &rateLimiter{
	windows: map[string]*rateWindow{},
	limit:   codeFailureLimit,
	period:  codeFailurePeriod,
}

func readCode(ctx echo.Context) (string, codeSource) {
	req := ctx.Request()
	query := req.URL.Query()
	if query.Has(CodeParam) {
		value := query.Get(CodeParam)
		query.Del(CodeParam)
		req.URL.RawQuery = query.Encode()
		req.RequestURI = req.URL.RequestURI()
		ctx.QueryParams().Del(CodeParam)
		if value != "" {
			return value, codeSourceQuery
		}
	}
	if value := req.Header.Get(CodeHeader); value != "" {
		req.Header.Del(CodeHeader)
		return value, codeSourceHeader
	}
	if cookie, err := ctx.Cookie(CodeCookie); err == nil && cookie.Value != "" {
		return cookie.Value, codeSourceCookie
	}
	return "", codeSourceNone
}

func writeCodeCookie(ctx echo.Context, value string, maxAge int, expires *time.Time) {
	cookie := &http.Cookie{
		Name:     CodeCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
	if expires != nil {
		cookie.Expires = expires.UTC()
		cookie.MaxAge = int(time.Until(*expires).Seconds())
		if cookie.MaxAge <= 0 {
			cookie.MaxAge = -1
		}
	}
	ctx.SetCookie(cookie)
}

func clearCodeCookie(ctx echo.Context) {
	writeCodeCookie(ctx, "", -1, nil)
}

func isNavigationGet(ctx echo.Context) bool {
	return ctx.Request().Method == http.MethodGet && isNavigation(ctx)
}

func respondCodeStripped(ctx echo.Context) error {
	return ctx.Redirect(http.StatusSeeOther, ctx.Request().URL.RequestURI())
}

func respondCodeRefused(ctx echo.Context) error {
	return ctx.String(http.StatusUnauthorized, "Unauthorized")
}

func serveCode(ctx echo.Context, config *AuthMiddlewareConfig, value string, source codeSource, authenticated bool, proceed echo.HandlerFunc) error {
	ctx.Response().Header().Set("Referrer-Policy", "no-referrer")
	ip := ctx.RealIP()
	if source != codeSourceCookie && codeFailures.blocked(ip) {
		ctx.Response().Header().Set("Retry-After", strconv.Itoa(int(codeFailurePeriod.Seconds())))
		return ctx.String(http.StatusTooManyRequests, "Too Many Requests")
	}

	reqCtx := ctx.Request().Context()
	access, err := sdb.Instance().GetCodeAccess(reqCtx, value)
	if errors.Is(err, shared.ErrCodeInvalid) {
		if source == codeSourceCookie {
			clearCodeCookie(ctx)
			return serveRequest(ctx, config, authenticated, proceed)
		}
		codeFailures.fail(ip)
		return respondCodeRefused(ctx)
	}
	if err != nil {
		ctx.Logger().Errorf("resolve access code: %v", err)
		return ctx.String(http.StatusServiceUnavailable, "Service Unavailable")
	}

	switch access.Kind {
	case shared.CODE_KIND_GUEST:
		if !config.Guest {
			if authenticated && source == codeSourceQuery && isNavigationGet(ctx) {
				return respondCodeStripped(ctx)
			}
			return serveRequest(ctx, config, authenticated, proceed)
		}
		return serveGuestCode(ctx, access, value, source, authenticated, proceed)
	case shared.CODE_KIND_INVITE:
		return serveInviteCode(ctx, access, value, source, authenticated, proceed)
	}
	codeFailures.fail(ip)
	return respondCodeRefused(ctx)
}

func serveGuestCode(ctx echo.Context, access *shared.CodeAccess, value string, source codeSource, authenticated bool, proceed echo.HandlerFunc) error {
	reqCtx := ctx.Request().Context()
	if access.Count > 0 && source != codeSourceCookie {
		if _, err := sdb.Instance().PutCodeUsed(reqCtx, value); err != nil {
			if errors.Is(err, shared.ErrCodeInvalid) {
				codeFailures.fail(ctx.RealIP())
				return respondCodeRefused(ctx)
			}
			ctx.Logger().Errorf("use access code %s: %v", access.ID, err)
			return ctx.String(http.StatusServiceUnavailable, "Service Unavailable")
		}
	}
	ctx.Set(CodeKey, access)
	if !authenticated {
		ctx.Set(UserIDKey, *access.GuestUserID)
		if err := shared.WriteAudit(reqCtx, sdb.Instance().DB.DB, nil, "guest request", "read", shared.OBJECT_TYPE_CLAIM,
			access.ClaimID, access.OwnerID, access.GuestUserClaimID,
			fmt.Sprintf("guest of claim %s with code %s: %s %s", access.ClaimID, access.ID, ctx.Request().Method, ctx.Request().URL.Path)); err != nil {
			ctx.Logger().Errorf("audit guest request on code %s: %v", access.ID, err)
			return ctx.String(http.StatusServiceUnavailable, "Service Unavailable")
		}
	}
	if source == codeSourceQuery && isNavigationGet(ctx) {
		if !authenticated {
			writeCodeCookie(ctx, value, 0, access.EndsAt)
		}
		return respondCodeStripped(ctx)
	}
	return proceed(ctx)
}

func serveInviteCode(ctx echo.Context, access *shared.CodeAccess, value string, source codeSource, authenticated bool, proceed echo.HandlerFunc) error {
	if !authenticated {
		login := getLoginPath()
		if login == "" || !isNavigationGet(ctx) {
			return respondCodeRefused(ctx)
		}
		writeCodeCookie(ctx, value, codeInviteMaxAge, nil)
		return ctx.Redirect(http.StatusSeeOther, login+"?next="+url.QueryEscape(ctx.Request().URL.RequestURI()))
	}
	userID, _ := ctx.Get(UserIDKey).(uuid.UUID)
	if _, err := sdb.Instance().PostCodeRedeem(ctx.Request().Context(), value, userID); err != nil {
		if source == codeSourceCookie {
			clearCodeCookie(ctx)
		}
		if errors.Is(err, shared.ErrCodeInvalid) {
			codeFailures.fail(ctx.RealIP())
			return respondCodeRefused(ctx)
		}
		ctx.Logger().Errorf("redeem access code %s: %v", access.ID, err)
		return ctx.String(http.StatusServiceUnavailable, "Service Unavailable")
	}
	if source == codeSourceCookie {
		clearCodeCookie(ctx)
	}
	ctx.Set(CodeKey, access)
	if source == codeSourceQuery && isNavigationGet(ctx) {
		return respondCodeStripped(ctx)
	}
	return proceed(ctx)
}

func serveAnonymous(ctx echo.Context, proceed echo.HandlerFunc) error {
	ctx.Set(UserIDKey, shared.AnonymousUserID)
	ctx.Set(AnonymousKey, true)
	res := ctx.Response()
	res.Before(func() {
		if res.Status == http.StatusForbidden {
			res.Status = http.StatusUnauthorized
		}
	})
	return proceed(ctx)
}

func serveRequest(ctx echo.Context, config *AuthMiddlewareConfig, authenticated bool, proceed echo.HandlerFunc) error {
	if authenticated {
		return proceed(ctx)
	}
	if config.Guest {
		return serveAnonymous(ctx, proceed)
	}
	if config.Optional {
		return proceed(ctx)
	}
	return respondUnauthenticated(ctx)
}
