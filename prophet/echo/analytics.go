package echo

import (
	"net/http"
	"strconv"

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

func analyticsEnabled(ctx echo.Context, appID uuid.UUID) bool {
	var enabled bool
	err := sdb.Instance().NewRaw(`
		SELECT EXISTS (
			SELECT 1
			FROM claimius.user_claim uc
				JOIN claimius.claim c ON c.id = uc.claim_id
				JOIN claimius.claim_object co ON co.claim_id = uc.claim_id
			WHERE c.app_id = ?
				AND uc.sa_deleted_at IS NULL
				AND c.sa_deleted_at IS NULL
				AND co.sa_deleted_at IS NULL
				AND (uc.starts_at IS NULL OR uc.starts_at <= now())
				AND (uc.ends_at IS NULL OR uc.ends_at > now())
				AND co.object_id IS NULL
				AND jsonb_path_exists(co.scope, '$.analytics'::jsonpath)
				AND ((c.sa_access | co.sa_access) & 16) = 0
		)
	`, appID).Scan(ctx.Request().Context(), &enabled)
	if err != nil {
		ctx.Logger().Errorf("analyticsEnabled: %v", err)
		return false
	}
	return enabled
}

func GetConsent(ctx echo.Context) error {
	appID, ok := ctx.Get("app_id").(uuid.UUID)
	if !ok || appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if !analyticsEnabled(ctx, appID) {
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
	if !analyticsEnabled(ctx, appID) {
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
	if !analyticsEnabled(ctx, appID) {
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
	if !analyticsEnabled(ctx, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	items := make([]shared.AnalyticsEvent, 0)
	q := sdb.Instance().NewSelect().
		Model(&items).
		Where("app_id = ?", appID).
		Order("occurred_at DESC").
		Limit(intParam(ctx, "limit", 100)).
		Offset(intParam(ctx, "offset", 0))
	if raw := ctx.QueryParam("session_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return respond.Error(ctx, http.StatusBadRequest, "session_id must be a uuid")
		}
		q = q.Where("session_id = ?", id)
	}
	if userID := forwardedUser(ctx); userID != nil {
		q = q.Where("user_id = ?", *userID)
	}
	if v := ctx.QueryParam("type"); v != "" {
		q = q.Where("type = ?", v)
	}
	if v := ctx.QueryParam("path"); v != "" {
		q = q.Where("path = ?", v)
	}
	if err := q.Scan(ctx.Request().Context()); err != nil {
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
	if !analyticsEnabled(ctx, appID) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	items := make([]shared.AnalyticsSession, 0)
	q := sdb.Instance().NewSelect().
		Model(&items).
		Where("app_id = ?", appID).
		Order("started_at DESC").
		Limit(intParam(ctx, "limit", 100)).
		Offset(intParam(ctx, "offset", 0))
	if userID := forwardedUser(ctx); userID != nil {
		q = q.Where("user_id = ?", *userID)
	}
	if v := ctx.QueryParam("anonymous_id"); v != "" {
		q = q.Where("anonymous_id = ?", v)
	}
	if err := q.Scan(ctx.Request().Context()); err != nil {
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
