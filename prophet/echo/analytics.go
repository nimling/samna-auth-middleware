package echo

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/nimling/samna-auth-middleware/v2/analytics"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
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

// captureApp names the app a capture request belongs to. A disciple reaches
// the prophet on the sam channel and AppMiddleware has already set app_id. The
// prophet's own portal reaches these routes from a browser with no app
// credentials, so the system app it is configured as answers for it.
func captureApp(ctx echo.Context) (uuid.UUID, bool) {
	if id, ok := ctx.Get("app_id").(uuid.UUID); ok && id != uuid.Nil {
		return id, true
	}
	slug := utils.Cfg().AppSlug
	if slug == "" {
		return uuid.Nil, false
	}
	var app shared.SamnaApp
	err := sdb.Instance().NewSelect().
		Model(&app).
		Column("id").
		Where("slug = ?", slug).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())
	if err != nil || app.ID == uuid.Nil {
		ctx.Logger().Errorf("captureApp slug=%s: %v", slug, err)
		return uuid.Nil, false
	}
	return app.ID, true
}

func analyticsEnabled(ctx echo.Context, appID uuid.UUID) bool {
	var enabled bool
	err := sdb.Instance().
		NewRaw(`SELECT claimius.analytics_enabled(?)`, appID).
		Scan(ctx.Request().Context(), &enabled)
	if err != nil {
		ctx.Logger().Errorf("analyticsEnabled: %v", err)
		return false
	}
	return enabled
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
	appID, ok := captureApp(ctx)
	if !ok {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if !analyticsEnabled(ctx, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	anonymousID := analytics.AnonymousID(ctx)
	state, err := analytics.GetConsent(ctx.Request().Context(), appID, forwardedUser(ctx), anonymousID)
	if err != nil {
		ctx.Logger().Errorf("GetConsent: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if err := captured(ctx, appID, state); err != nil {
		ctx.Logger().Errorf("GetConsent captured: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, state, 1)
}

// captured fills the paths the app holds a fresh snapshot for, so the browser
// learns in the consent answer which pages need no capture instead of asking
// once per view.
func captured(ctx echo.Context, appID uuid.UUID, state *shared.ConsentState) error {
	state.Captured = make([]string, 0)
	if !state.Categories.Analytics {
		return nil
	}
	return sdb.Instance().
		NewRaw(`SELECT claimius.get_analytics_captured(?, ?::interval)`, appID, analytics.CaptureMaxAge).
		Scan(ctx.Request().Context(), &state.Captured)
}

func PostConsent(ctx echo.Context) error {
	appID, ok := captureApp(ctx)
	if !ok {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if !analyticsEnabled(ctx, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	var body shared.ConsentRequest
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	if body.AnonymousID == "" {
		body.AnonymousID = analytics.AnonymousID(ctx)
	}
	if body.AnonymousID == "" {
		return respond.Error(ctx, http.StatusBadRequest, "anonymous_id is required")
	}
	analytics.WriteAnonymousID(ctx, body.AnonymousID)
	if body.PolicyVersion == "" {
		body.PolicyVersion = analytics.PolicyVersion()
	}
	state, err := analytics.PostConsent(ctx.Request().Context(), appID, body, forwardedUser(ctx), forwardedIP(ctx), forwardedAgent(ctx))
	if err != nil {
		ctx.Logger().Errorf("PostConsent: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if err := captured(ctx, appID, state); err != nil {
		ctx.Logger().Errorf("PostConsent captured: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, state, 1)
}

func PostAnalytics(ctx echo.Context) error {
	appID, ok := captureApp(ctx)
	if !ok {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if !analyticsEnabled(ctx, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
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

func GetAnalyticsApps(ctx echo.Context) error {
	reader := forwardedUser(ctx)
	if reader == nil {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	items := make([]shared.AnalyticsApp, 0)
	err := sdb.Instance().
		NewRaw(`SELECT * FROM claimius.get_analytics_apps(?)`, *reader).
		Scan(ctx.Request().Context(), &items)
	if err != nil {
		ctx.Logger().Errorf("GetAnalyticsApps: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, items, len(items))
}

func GetAnalyticsEvents(ctx echo.Context) error {
	reader := forwardedUser(ctx)
	if reader == nil {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	producer, ok := uuidParam(ctx, "app")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "app must be a uuid")
	}
	sessionID, ok := uuidParam(ctx, "session_id")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "session_id must be a uuid")
	}
	visitor, ok := uuidParam(ctx, "visitor")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "visitor must be a uuid")
	}
	from, ok := timeParam(ctx, "from")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "from must be an RFC3339 timestamp")
	}
	until, ok := timeParam(ctx, "to")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "to must be an RFC3339 timestamp")
	}
	items := make([]shared.AnalyticsEvent, 0)
	err := sdb.Instance().
		NewRaw(`SELECT * FROM claimius.get_analytics_events(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			*reader, producer, sessionID, visitor,
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
	reader := forwardedUser(ctx)
	if reader == nil {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	producer, ok := uuidParam(ctx, "app")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "app must be a uuid")
	}
	visitor, ok := uuidParam(ctx, "visitor")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "visitor must be a uuid")
	}
	from, ok := timeParam(ctx, "from")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "from must be an RFC3339 timestamp")
	}
	until, ok := timeParam(ctx, "to")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "to must be an RFC3339 timestamp")
	}
	items := make([]shared.AnalyticsSession, 0)
	err := sdb.Instance().
		NewRaw(`SELECT * FROM claimius.get_analytics_sessions(?, ?, ?, ?, ?, ?, ?, ?)`,
			*reader, producer, visitor, textParam(ctx, "anonymous_id"),
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

func PostAnalyticsCapture(ctx echo.Context) error {
	appID, ok := captureApp(ctx)
	if !ok {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if !analyticsEnabled(ctx, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	path := ctx.QueryParam("path")
	if path == "" {
		return ctx.NoContent(http.StatusNoContent)
	}
	body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, analytics.CaptureBytesMax+1))
	if err != nil || len(body) == 0 {
		return ctx.NoContent(http.StatusNoContent)
	}
	if len(body) > analytics.CaptureBytesMax {
		return respond.Error(ctx, http.StatusRequestEntityTooLarge, "snapshot is too large")
	}
	if !json.Valid(body) {
		return respond.Error(ctx, http.StatusBadRequest, "snapshot must be json")
	}
	contentType := ctx.Request().Header.Get(echo.HeaderContentType)
	if contentType == "" {
		contentType = echo.MIMEApplicationJSON
	}
	_, err = sdb.Instance().
		NewRaw(`SELECT claimius.upsert_analytics_capture(?, ?, ?, ?)`, appID, path, contentType, body).
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("PostAnalyticsCapture: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return ctx.NoContent(http.StatusNoContent)
}

func GetAnalyticsCapture(ctx echo.Context) error {
	reader := forwardedUser(ctx)
	if reader == nil {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	producer, ok := uuidParam(ctx, "app")
	if !ok || producer == nil {
		return respond.Error(ctx, http.StatusBadRequest, "app must be a uuid")
	}
	path := ctx.QueryParam("path")
	if path == "" {
		return respond.Error(ctx, http.StatusBadRequest, "path is required")
	}
	items := make([]shared.AnalyticsCapture, 0)
	err := sdb.Instance().
		NewRaw(`SELECT c.* FROM claimius.get_analytics_capture(?, ?) c
			WHERE c.app_id IN (SELECT claimius.capture_user_apps(?))`,
			*producer, path, *reader).
		Scan(ctx.Request().Context(), &items)
	if err != nil {
		ctx.Logger().Errorf("GetAnalyticsCapture: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if len(items) == 0 {
		return ctx.NoContent(http.StatusNotFound)
	}
	return respond.JSON(ctx, http.StatusOK, items[0], 1)
}

func GetAnalyticsSummary(ctx echo.Context) error {
	reader := forwardedUser(ctx)
	if reader == nil {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	producer, ok := uuidParam(ctx, "app")
	if !ok || producer == nil {
		return respond.Error(ctx, http.StatusBadRequest, "app must be a uuid")
	}
	from, ok := timeParam(ctx, "from")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "from must be an RFC3339 timestamp")
	}
	until, ok := timeParam(ctx, "to")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "to must be an RFC3339 timestamp")
	}
	var raw string
	err := sdb.Instance().
		NewRaw(`SELECT claimius.get_analytics_summary(?, ?, ?, ?)`, *reader, *producer, from, until).
		Scan(ctx.Request().Context(), &raw)
	if err != nil {
		ctx.Logger().Errorf("GetAnalyticsSummary: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	var item shared.AnalyticsSummary
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		ctx.Logger().Errorf("GetAnalyticsSummary decode: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, item, 1)
}

func GetAnalyticsHeat(ctx echo.Context) error {
	reader := forwardedUser(ctx)
	if reader == nil {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	producer, ok := uuidParam(ctx, "app")
	if !ok || producer == nil {
		return respond.Error(ctx, http.StatusBadRequest, "app must be a uuid")
	}
	path := ctx.QueryParam("path")
	if path == "" {
		return respond.Error(ctx, http.StatusBadRequest, "path is required")
	}
	var width *int
	if raw := intParam(ctx, "viewport_width", 0); raw > 0 {
		width = &raw
	}
	from, ok := timeParam(ctx, "from")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "from must be an RFC3339 timestamp")
	}
	until, ok := timeParam(ctx, "to")
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "to must be an RFC3339 timestamp")
	}
	items := make([]shared.AnalyticsHeatPoint, 0)
	err := sdb.Instance().
		NewRaw(`SELECT h.* FROM claimius.get_analytics_heat(?, ?, ?, ?, ?::date, ?::date) h
			WHERE ? IN (SELECT claimius.capture_user_apps(?))`,
			*producer, path, width, textParam(ctx, "type"), from, until,
			*producer, *reader).
		Scan(ctx.Request().Context(), &items)
	if err != nil {
		ctx.Logger().Errorf("GetAnalyticsHeat: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, items, len(items))
}
