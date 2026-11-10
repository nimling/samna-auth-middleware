package smw

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/analytics"
	"github.com/nimling/samna-auth-middleware/v2/client"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const (
	CodeParam  = client.CodeParam
	CodeHeader = client.CodeHeader
	CodeCookie = shared.CodeCookie
)

const codeInviteMaxAge = 600

type codeSource int

const (
	codeSourceNone codeSource = iota
	codeSourceQuery
	codeSourceHeader
	codeSourceCookie
)

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

func respondUnavailable(ctx echo.Context, what string, err error) error {
	ctx.Logger().Errorf("%s: %v", what, err)
	return ctx.String(http.StatusServiceUnavailable, "Service Unavailable")
}

func serveCode(ctx echo.Context, value string, source codeSource, proceed echo.HandlerFunc) error {
	ctx.Response().Header().Set("Referrer-Policy", "no-referrer")
	reqCtx := ctx.Request().Context()
	public, err := sdb.Instance().GetPublicClaims(reqCtx, uuid.Nil)
	if err != nil {
		return respondUnavailable(ctx, "load public claims", err)
	}
	if public.Find(shared.GetCodeHash(value)) != nil {
		if source == codeSourceQuery && isNavigationGet(ctx) {
			return respondCodeStripped(ctx)
		}
		return proceed(ctx)
	}
	if source != codeSourceCookie {
		if limit, period := rateSettings(); !rateLimit.allow(rateKey(ctx), limit, period) {
			return respondRateLimited(ctx, period)
		}
	}
	access, err := sdb.Instance().GetCodeAccess(reqCtx, value)
	if errors.Is(err, shared.ErrCodeInvalid) {
		if source == codeSourceCookie {
			clearCodeCookie(ctx)
			return proceed(ctx)
		}
		return respondCodeRefused(ctx)
	}
	if err != nil {
		return respondUnavailable(ctx, "resolve access code", err)
	}
	userID, _ := ctx.Get(UserIDKey).(uuid.UUID)
	if _, err := sdb.Instance().PostCodeRedeem(reqCtx, value, userID); err != nil {
		if source == codeSourceCookie {
			clearCodeCookie(ctx)
		}
		if errors.Is(err, shared.ErrCodeInvalid) {
			return respondCodeRefused(ctx)
		}
		return respondUnavailable(ctx, "redeem access code "+access.ID.String(), err)
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

func serveInviteLogin(ctx echo.Context, value string) error {
	login := getLoginPath()
	if login == "" || !isNavigationGet(ctx) {
		return respondCodeRefused(ctx)
	}
	writeCodeCookie(ctx, value, codeInviteMaxAge, nil)
	return ctx.Redirect(http.StatusSeeOther, login+"?next="+url.QueryEscape(ctx.Request().URL.RequestURI()))
}

func anonymousDB() *shared.DB {
	if d := sdb.Instance(); d != nil {
		return d.DB
	}
	if p := psdb.Instance(); p != nil {
		return p
	}
	return nil
}

func anonymousApp(ctx echo.Context, db *shared.DB) (uuid.UUID, error) {
	if !db.Prophet {
		return uuid.Nil, nil
	}
	if appID, ok := ctx.Get("app_id").(uuid.UUID); ok && appID != uuid.Nil {
		return appID, nil
	}
	var appID uuid.UUID
	if slug := ctx.Request().Header.Get("SAuth-App-Slug"); slug != "" {
		err := db.NewRaw("SELECT id FROM claimius.samna_app WHERE slug = ? AND sa_deleted_at IS NULL", slug).Scan(ctx.Request().Context(), &appID)
		return appID, err
	}
	err := db.NewRaw("SELECT claimius.get_app_id()").Scan(ctx.Request().Context(), &appID)
	return appID, err
}

func AnonymousMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		if userID, ok := ctx.Get(UserIDKey).(uuid.UUID); ok && userID != uuid.Nil {
			return next(ctx)
		}
		value, source := readCode(ctx)
		return serveAnonymous(ctx, value, source, next)
	}
}

func serveAnonymous(ctx echo.Context, value string, source codeSource, proceed echo.HandlerFunc) error {
	if limit, period := rateSettings(); !rateLimit.allow(rateKey(ctx), limit, period) {
		return respondRateLimited(ctx, period)
	}
	instance := anonymousDB()
	if instance == nil {
		return respondUnauthenticated(ctx)
	}
	reqCtx := ctx.Request().Context()
	appID, err := anonymousApp(ctx, instance)
	if err != nil {
		return respondUnavailable(ctx, "resolve the app of an anonymous request", err)
	}
	public, err := instance.GetPublicClaims(reqCtx, appID)
	if err != nil {
		return respondUnavailable(ctx, "load public claims", err)
	}
	userID := shared.AnonymousUserID
	if value != "" {
		ctx.Response().Header().Set("Referrer-Policy", "no-referrer")
		hash := shared.GetCodeHash(value)
		claim := public.Find(hash)
		switch {
		case claim != nil && claim.Accepts(hash, source == codeSourceQuery, source == codeSourceHeader, source == codeSourceCookie):
			ctx.SetRequest(ctx.Request().WithContext(shared.WithCodeHash(reqCtx, hash)))
			if source == codeSourceQuery && claim.Samk.Cookie && isNavigationGet(ctx) {
				writeCodeCookie(ctx, value, 0, claim.EndsAt)
				return respondCodeStripped(ctx)
			}
		case claim != nil:
			if source != codeSourceCookie {
				return respondCodeRefused(ctx)
			}
			clearCodeCookie(ctx)
		default:
			var err error
			if instance.Prophet {
				_, err = psdb.GetCodeAccess(reqCtx, appID, value)
			} else {
				_, err = instance.GetCodeAccess(reqCtx, value)
			}
			if err == nil {
				return serveInviteLogin(ctx, value)
			}
			if !errors.Is(err, shared.ErrCodeInvalid) {
				return respondUnavailable(ctx, "resolve access code", err)
			}
			if source != codeSourceCookie {
				return respondCodeRefused(ctx)
			}
			clearCodeCookie(ctx)
		}
	}
	if len(public) == 0 {
		return respondUnauthenticated(ctx)
	}
	ctx.Set(UserIDKey, userID)
	ctx.Set(AnonymousKey, analytics.AnonymousID(ctx))
	res := ctx.Response()
	res.Before(func() {
		if res.Status == http.StatusForbidden {
			res.Status = http.StatusUnauthorized
		}
	})
	return proceed(ctx)
}
