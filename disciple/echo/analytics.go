package echo

import (
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/nimling/samna-auth-middleware/analytics"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
)

const anonymousCookie = "sauth_anonymous_id"

func forwardContext(ctx echo.Context) client.Forward {
	f := client.Forward{
		IP:        ctx.RealIP(),
		UserAgent: ctx.Request().UserAgent(),
	}
	if id, ok := ctx.Get("user_id").(uuid.UUID); ok && id != uuid.Nil {
		f.UserID = id.String()
	}
	return f
}

func anonymousID(ctx echo.Context) string {
	if cookie, err := ctx.Cookie(anonymousCookie); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	if v := ctx.QueryParam("anonymous_id"); v != "" {
		return v
	}
	return ""
}

func writeAnonymousCookie(ctx echo.Context, value string) {
	if value == "" {
		return
	}
	if cookie, err := ctx.Cookie(anonymousCookie); err == nil && cookie.Value == value {
		return
	}
	utils.WriteCookie(ctx, &http.Cookie{
		Name:     anonymousCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 365,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

func GetConsent(ctx echo.Context) error {
	id := anonymousID(ctx)
	if id == "" {
		id = uuid.NewString()
	}
	writeAnonymousCookie(ctx, id)

	var state shared.ConsentState
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Get(forwarded, "consent?anonymous_id="+url.QueryEscape(id), &state); err != nil {
		ctx.Logger().Errorf("GetConsent: %v", err)
		return respondJSON(ctx, http.StatusOK, shared.ConsentState{
			AnonymousID:   id,
			PolicyVersion: analytics.PolicyVersion(),
			PolicyURL:     analytics.PolicyURL(),
			Categories:    shared.ConsentCategories{Necessary: true},
		}, 1)
	}
	if state.AnonymousID == "" {
		state.AnonymousID = id
	}
	return respondJSON(ctx, http.StatusOK, state, 1)
}

func PostConsent(ctx echo.Context) error {
	var body shared.ConsentRequest
	if err := ctx.Bind(&body); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if body.AnonymousID == "" {
		body.AnonymousID = anonymousID(ctx)
	}
	if body.AnonymousID == "" {
		body.AnonymousID = uuid.NewString()
	}
	writeAnonymousCookie(ctx, body.AnonymousID)

	var state shared.ConsentState
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Post(forwarded, "consent", body, &state); err != nil {
		ctx.Logger().Errorf("PostConsent: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Consent service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, state, 1)
}

func GetAnalyticsEvents(ctx echo.Context) error {
	items := make([]shared.AnalyticsEvent, 0)
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Get(forwarded, "analytics/event"+passthroughQuery(ctx), &items); err != nil {
		ctx.Logger().Errorf("GetAnalyticsEvents: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Analytics service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, items, len(items))
}

func GetAnalyticsSessions(ctx echo.Context) error {
	items := make([]shared.AnalyticsSession, 0)
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Get(forwarded, "analytics/session"+passthroughQuery(ctx), &items); err != nil {
		ctx.Logger().Errorf("GetAnalyticsSessions: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Analytics service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, items, len(items))
}

func passthroughQuery(ctx echo.Context) string {
	allowed := []string{"type", "path", "session_id", "user_id", "anonymous_id", "limit", "offset"}
	values := url.Values{}
	for _, key := range allowed {
		if v := ctx.QueryParam(key); v != "" {
			values.Set(key, v)
		}
	}
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}

func PostAnalytics(ctx echo.Context) error {
	var body shared.AnalyticsBatch
	if err := ctx.Bind(&body); err != nil {
		return ctx.NoContent(http.StatusNoContent)
	}
	if body.AnonymousID == "" {
		body.AnonymousID = anonymousID(ctx)
	}
	if body.AnonymousID == "" || body.SessionKey == "" {
		return ctx.NoContent(http.StatusNoContent)
	}
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Post(forwarded, "analytics", body, nil); err != nil {
		ctx.Logger().Errorf("PostAnalytics: %v", err)
	}
	return ctx.NoContent(http.StatusNoContent)
}
