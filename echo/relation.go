package echo

import (
	"database/sql"
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

func GetRelations(ctx echo.Context) error {
	userID, ok := ctx.Get(middleware.UserIDKey).(uuid.UUID)
	if !ok {
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}

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

	relations := make([]db.UserRelation, 0)
	q := db.Instance().NewSelect().
		Model(&relations).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("user_relation.user_id = ?", userID).
		Where("user_relation.sa_deleted_at IS NULL")

	if len(orgIDs) > 0 {
		q.Where("user_relation.object_type = ? AND user_relation.object_id IN (?)", db.OBJECT_TYPE_ORGANIZATION, bun.In(orgIDs))
	}
	if len(locIDs) > 0 {
		q.Where("user_relation.object_type = ? AND user_relation.object_id IN (?)", db.OBJECT_TYPE_LOCATION, bun.In(locIDs))
	}
	if len(usrIDs) > 0 {
		q.Where("user_relation.user_id IN (?)", bun.In(usrIDs))
	}

	if err := q.Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetRelations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, relations, len(relations))
}

func GetRelation(ctx echo.Context) error {
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	userID, ok := ctx.Get(middleware.UserIDKey).(uuid.UUID)
	if !ok {
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}

	relation, err := db.Instance().GetRelation(ctx.Request().Context(), userID, relationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Errorf("GetRelation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, relation, 1)
}

func PostRelation(ctx echo.Context) error {
	userID, ok := ctx.Get(middleware.UserIDKey).(uuid.UUID)
	if !ok {
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}

	relation := &db.UserRelation{}
	if err := ctx.Bind(relation); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	relation.UserID = userID
	if err := relation.ValidatePost(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, relation.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostRelation MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		relation, err = db.Instance().PostRelation(ctx.Request().Context(), ac, relation)
		if err != nil {
			ctx.Logger().Errorf("PostRelation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, relation, 1)
	}

	result, err := client.PostRelation(ctx.Request().Context(), *relation)
	if err != nil {
		ctx.Logger().Errorf("PostRelation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 1)
}

func PutRelation(ctx echo.Context) error {
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	relation := &db.UserRelation{}
	if err := relation.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := relation.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		existing, err := db.Instance().GetRelation(ctx.Request().Context(), userID, relationID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return respondError(ctx, http.StatusNotFound, "Not found")
			}
			ctx.Logger().Errorf("PutRelation GetRelation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, existing.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutRelation MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		relation, err = db.Instance().PutRelation(ctx.Request().Context(), ac, relationID, relation)
		if err != nil {
			ctx.Logger().Errorf("PutRelation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, relation, 1)
	}

	result, err := client.PutRelation(ctx.Request().Context(), relationID, *relation)
	if err != nil {
		ctx.Logger().Errorf("PutRelation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func PatchRelation(ctx echo.Context) error {
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	relation := &db.UserRelation{}
	if err := relation.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := relation.ValidatePatch(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		existing, err := db.Instance().GetRelation(ctx.Request().Context(), userID, relationID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return respondError(ctx, http.StatusNotFound, "Not found")
			}
			ctx.Logger().Errorf("PatchRelation GetRelation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, existing.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PatchRelation MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		relation, err = db.Instance().PatchRelation(ctx.Request().Context(), ac, relationID, relation)
		if err != nil {
			ctx.Logger().Errorf("PatchRelation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, relation, 1)
	}

	result, err := client.PatchRelation(ctx.Request().Context(), relationID, *relation)
	if err != nil {
		ctx.Logger().Errorf("PatchRelation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func DeleteRelation(ctx echo.Context) error {
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, userID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("DeleteRelation MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		relation, err := db.Instance().DeleteRelation(ctx.Request().Context(), ac, relationID)
		if err != nil {
			ctx.Logger().Errorf("DeleteRelation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, relation, 1)
	}

	result, err := client.DeleteRelation(ctx.Request().Context(), relationID)
	if err != nil {
		ctx.Logger().Errorf("DeleteRelation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}
