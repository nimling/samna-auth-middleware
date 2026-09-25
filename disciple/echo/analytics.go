package echo

import (
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/nimling/samna-auth-middleware/analytics"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/shared"
)

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

func GetConsent(ctx echo.Context) error {
	id := analytics.AnonymousID(ctx)

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
		body.AnonymousID = analytics.AnonymousID(ctx)
	}
	analytics.WriteAnonymousID(ctx, body.AnonymousID)

	var state shared.ConsentState
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Post(forwarded, "consent", body, &state); err != nil {
		ctx.Logger().Errorf("PostConsent: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Consent service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, state, 1)
}

func GetAnalyticsApps(ctx echo.Context) error {
	items := make([]shared.AnalyticsApp, 0)
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Get(forwarded, "analytics/app", &items); err != nil {
		ctx.Logger().Errorf("GetAnalyticsApps: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Analytics service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, items, len(items))
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
	allowed := []string{"app", "type", "path", "session_id", "visitor", "anonymous_id", "from", "to", "limit", "offset", "viewport_width"}
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
		body.AnonymousID = analytics.AnonymousID(ctx)
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

func PostAnalyticsCapture(ctx echo.Context) error {
	if ctx.QueryParam("path") == "" {
		return ctx.NoContent(http.StatusNoContent)
	}
	body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, analytics.CaptureBytesMax+1))
	if err != nil || len(body) == 0 {
		return ctx.NoContent(http.StatusNoContent)
	}
	if len(body) > analytics.CaptureBytesMax {
		return respondError(ctx, http.StatusRequestEntityTooLarge, "snapshot is too large")
	}
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	forwarded = client.WithContentType(forwarded, ctx.Request().Header.Get(echo.HeaderContentType))
	if err := client.Post(forwarded, "analytics/capture"+passthroughQuery(ctx), body, nil); err != nil {
		ctx.Logger().Errorf("PostAnalyticsCapture: %v", err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func GetAnalyticsSummary(ctx echo.Context) error {
	var item shared.AnalyticsSummary
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Get(forwarded, "analytics/summary"+passthroughQuery(ctx), &item); err != nil {
		ctx.Logger().Errorf("GetAnalyticsSummary: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Analytics service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, item, 1)
}

func GetAnalyticsCapture(ctx echo.Context) error {
	var item shared.AnalyticsCapture
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Get(forwarded, "analytics/capture"+passthroughQuery(ctx), &item); err != nil {
		ctx.Logger().Errorf("GetAnalyticsCapture: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Analytics service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, item, 1)
}

func GetAnalyticsHeat(ctx echo.Context) error {
	items := make([]shared.AnalyticsHeatPoint, 0)
	forwarded := client.WithForward(ctx.Request().Context(), forwardContext(ctx))
	if err := client.Get(forwarded, "analytics/heat"+passthroughQuery(ctx), &items); err != nil {
		ctx.Logger().Errorf("GetAnalyticsHeat: %v", err)
		return respondError(ctx, http.StatusBadGateway, "Analytics service unavailable")
	}
	return respondJSON(ctx, http.StatusOK, items, len(items))
}
