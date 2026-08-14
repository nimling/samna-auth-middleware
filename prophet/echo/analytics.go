package echo

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/nimling/samna-auth-middleware/analytics"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/shared"
)

const (
	headerForwardedFor   = "SAuth-Forwarded-For"
	headerForwardedUser  = "SAuth-Forwarded-User"
	headerForwardedAgent = "SAuth-Forwarded-Agent"
)

func forwardedIP(ctx echo.Context) string {
	if v := ctx.Request().Header.Get(headerForwardedFor); v != "" {
		return v
	}
	return ctx.RealIP()
}

func forwardedAgent(ctx echo.Context) string {
	if v := ctx.Request().Header.Get(headerForwardedAgent); v != "" {
		return v
	}
	return ctx.Request().UserAgent()
}

func forwardedUser(ctx echo.Context) *uuid.UUID {
	raw := ctx.Request().Header.Get(headerForwardedUser)
	if raw == "" {
		raw = ctx.QueryParam("user_id")
	}
	if raw == "" {
		if id, ok := ctx.Get("user_id").(uuid.UUID); ok && id != uuid.Nil {
			return &id
		}
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return nil
	}
	return &id
}

func captureAccess(ctx echo.Context, appID, targetApp uuid.UUID) bool {
	var allowed bool
	err := sdb.Instance().
		NewRaw(`SELECT claimius.capture_app_access(?, ?)`, appID, targetApp).
		Scan(ctx.Request().Context(), &allowed)
	if err != nil {
		ctx.Logger().Errorf("captureAccess: %v", err)
		return false
	}
	return allowed
}

func targetAppID(ctx echo.Context, appID uuid.UUID) (uuid.UUID, bool) {
	raw := ctx.QueryParam("app")
	if raw == "" {
		return appID, true
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
}

func uuidParam(ctx echo.Context, name string) (*uuid.UUID, bool) {
	raw := ctx.QueryParam(name)
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return nil, false
	}
	return &id, true
}

func textParam(ctx echo.Context, name string) *string {
	raw := ctx.QueryParam(name)
	if raw == "" {
		return nil
	}
	return &raw
}

func timeParam(ctx echo.Context, name string) (*time.Time, bool) {
	raw := ctx.QueryParam(name)
	if raw == "" {
		return nil, true
	}
	moment, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, false
	}
	return &moment, true
}

func GetConsent(ctx echo.Context) error {
	appID, ok := ctx.Get("app_id").(uuid.UUID)
	if !ok || appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if !captureAccess(ctx, appID, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	anonymousID := ctx.QueryParam("anonymous_id")
	state, err := analytics.GetConsent(ctx.Request().Context(), appID, forwardedUser(ctx), anonymousID)
	if err != nil {
		ctx.Logger().Errorf("GetConsent: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, state, 1)
}

func PostConsent(ctx echo.Context) error {
	appID, ok := ctx.Get("app_id").(uuid.UUID)
	if !ok || appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if !captureAccess(ctx, appID, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	var body shared.ConsentRequest
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	if body.AnonymousID == "" {
		return respond.Error(ctx, http.StatusBadRequest, "anonymous_id is required")
	}
	if body.PolicyVersion == "" {
		body.PolicyVersion = analytics.PolicyVersion()
	}
	state, err := analytics.PostConsent(ctx.Request().Context(), appID, body, forwardedUser(ctx), forwardedIP(ctx), forwardedAgent(ctx))
	if err != nil {
		ctx.Logger().Errorf("PostConsent: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, state, 1)
}

func PostAnalytics(ctx echo.Context) error {
	appID, ok := ctx.Get("app_id").(uuid.UUID)
	if !ok || appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if !captureAccess(ctx, appID, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	var body shared.AnalyticsBatch
	if err := ctx.Bind(&body); err != nil {
		return ctx.NoContent(http.StatusNoContent)
	}
	if body.AnonymousID == "" || body.SessionKey == "" {
		return ctx.NoContent(http.StatusNoContent)
	}
	granted, err := analytics.PostSession(
		ctx.Request().Context(), appID, body.AnonymousID, body.SessionKey,
		body.Context, forwardedUser(ctx), forwardedIP(ctx), forwardedAgent(ctx),
	)
	if err != nil {
		ctx.Logger().Errorf("PostAnalytics session: %v", err)
		return ctx.NoContent(http.StatusNoContent)
	}
	if granted {
		analytics.Post(appID, body.SessionKey, body.Events)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func GetAnalyticsEvents(ctx echo.Context) error {
	appID, ok := ctx.Get("app_id").(uuid.UUID)
	if !ok || appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	targetApp, ok := targetAppID(ctx, appID)
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "app must be a uuid")
	}
	sessionID, ok := uuidParam(ctx, "session_id")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "session_id must be a uuid")
	}
	userID, ok := uuidParam(ctx, "user_id")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "user_id must be a uuid")
	}
	from, ok := timeParam(ctx, "from")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "from must be an RFC3339 timestamp")
	}
	until, ok := timeParam(ctx, "to")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "to must be an RFC3339 timestamp")
	}
	if !captureAccess(ctx, appID, targetApp) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	items := make([]shared.AnalyticsEvent, 0)
	err := sdb.Instance().
		NewRaw(`SELECT * FROM claimius.get_analytics_events(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			appID, targetApp, sessionID, userID,
			textParam(ctx, "type"), textParam(ctx, "path"),
			from, until,
			intParam(ctx, "limit", 100), intParam(ctx, "offset", 0)).
		Scan(ctx.Request().Context(), &items)
	if err != nil {
		ctx.Logger().Errorf("GetAnalyticsEvents: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, items, len(items))
}

func GetAnalyticsSessions(ctx echo.Context) error {
	appID, ok := ctx.Get("app_id").(uuid.UUID)
	if !ok || appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	targetApp, ok := targetAppID(ctx, appID)
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "app must be a uuid")
	}
	userID, ok := uuidParam(ctx, "user_id")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "user_id must be a uuid")
	}
	from, ok := timeParam(ctx, "from")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "from must be an RFC3339 timestamp")
	}
	until, ok := timeParam(ctx, "to")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "to must be an RFC3339 timestamp")
	}
	if !captureAccess(ctx, appID, targetApp) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	items := make([]shared.AnalyticsSession, 0)
	err := sdb.Instance().
		NewRaw(`SELECT * FROM claimius.get_analytics_sessions(?, ?, ?, ?, ?, ?, ?, ?)`,
			appID, targetApp, userID, textParam(ctx, "anonymous_id"),
			from, until,
			intParam(ctx, "limit", 100), intParam(ctx, "offset", 0)).
		Scan(ctx.Request().Context(), &items)
	if err != nil {
		ctx.Logger().Errorf("GetAnalyticsSessions: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, items, len(items))
}

func intParam(ctx echo.Context, name string, fallback int) int {
	raw := ctx.QueryParam(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return fallback
	}
	return v
}
