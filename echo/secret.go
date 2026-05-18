package echo

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
	"github.com/uptrace/bun"
)

func GetSecrets(ctx echo.Context) error {
	params := ctx.QueryParams()

	orgIDs := make([]uuid.UUID, 0)
	for _, raw := range params["org_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			orgIDs = append(orgIDs, id)
		}
	}

	locIDs := make([]uuid.UUID, 0)
	for _, raw := range params["loc_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			locIDs = append(locIDs, id)
		}
	}

	usrIDs := make([]uuid.UUID, 0)
	for _, raw := range params["usr_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			usrIDs = append(usrIDs, id)
		}
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		secrets := make([]db.SamnaSecret, 0)
		q := db.Instance().NewSelect().
			Model(&secrets).
			ExcludeColumn("sa_deleted_at").
			ExcludeColumn("app_id").
			Where("samna_secret.sa_deleted_at IS NULL").
			Where("samna_secret.id IN (SELECT id FROM claimius.get_secrets(?, claimius.get_disciple_app_id(), ?))", userID, db.ACCESS_OWNER)

		if len(orgIDs) > 0 {
			q.Where("samna_secret.sa_owner_id IN (?)", bun.In(orgIDs))
		}
		if len(locIDs) > 0 {
			q.Where("samna_secret.sa_owner_id IN (SELECT sa_owner_id FROM claimius.location WHERE sa_deleted_at IS NULL AND id IN (?))", bun.In(locIDs))
		}
		if len(usrIDs) > 0 {
			q.WhereGroup(" AND ", func(qq *bun.SelectQuery) *bun.SelectQuery {
				for _, uid := range usrIDs {
					qq = qq.WhereOr("samna_secret.id IN (SELECT id FROM claimius.get_secrets(?, claimius.get_disciple_app_id(), ?))", uid, db.ACCESS_OWNER)
				}
				return qq
			})
		}

		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetSecrets: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, secrets, len(secrets))
	}

	body, err := client.GetSecrets(ctx.Request().Context(), ctx.QueryString())
	if err != nil {
		ctx.Logger().Errorf("GetSecrets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetSecret(ctx echo.Context) error {
	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, secretID, db.OBJECT_TYPE_SAMNA_SECRET, db.ACCESS_OWNER)
		if err != nil || ac == nil {
			if err != nil && !errors.Is(err, db.ErrNoAccess) {
				ctx.Logger().Errorf("GetSecret MutationAccess: %v", err)
				return respondError(ctx, http.StatusInternalServerError, "Internal server error")
			}
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		secret, err := db.Instance().GetSecret(ctx.Request().Context(), secretID)
		if err != nil {
			ctx.Logger().Errorf("GetSecret: %v", err)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, secret, 1)
	}

	body, err := client.GetSecret(ctx.Request().Context(), secretID)
	if err != nil {
		ctx.Logger().Errorf("GetSecret: %v", err)
		return respondError(ctx, http.StatusForbidden, "Forbidden")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostSecret(ctx echo.Context) error {
	reqBody := db.BodySecret{}
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := reqBody.ValidatePost(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, *reqBody.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_OWNER)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostSecret MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		secret, err := db.Instance().PostSecret(ctx.Request().Context(), ac, reqBody)
		if err != nil {
			ctx.Logger().Errorf("PostSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, secret, 1)
	}

	body, err := client.PostSecret(ctx.Request().Context(), reqBody)
	if err != nil {
		ctx.Logger().Errorf("PostSecret: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func PutSecret(ctx echo.Context) error {
	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	reqBody := db.BodySecret{}
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := reqBody.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, secretID, db.OBJECT_TYPE_SAMNA_SECRET, db.ACCESS_OWNER)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutSecret MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		secret, err := db.Instance().PutSecret(ctx.Request().Context(), ac, secretID, reqBody)
		if err != nil {
			ctx.Logger().Errorf("PutSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, secret, 1)
	}

	body, err := client.PutSecret(ctx.Request().Context(), secretID, reqBody)
	if err != nil {
		ctx.Logger().Errorf("PutSecret: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteSecret(ctx echo.Context) error {
	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, secretID, db.OBJECT_TYPE_SAMNA_SECRET, db.ACCESS_OWNER)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("DeleteSecret MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		secret, err := db.Instance().DeleteSecret(ctx.Request().Context(), ac, secretID)
		if err != nil {
			ctx.Logger().Errorf("DeleteSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, secret, 1)
	}

	body, err := client.DeleteSecret(ctx.Request().Context(), secretID)
	if err != nil {
		ctx.Logger().Errorf("DeleteSecret: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
