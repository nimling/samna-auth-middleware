package echo

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
)

func GetObjectFields(ctx echo.Context) error {
	if db.GetDebugMode() != db.DebugModeNone {
		fields, err := db.Instance().GetObjectFields(ctx.Request().Context())
		if err != nil {
			ctx.Logger().Errorf("GetObjectFields: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, fields, len(fields))
	}

	body, err := client.GetObjectFields(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetObjectFields: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		field, err := db.Instance().GetObjectField(ctx.Request().Context(), fieldID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	body, err := client.GetObjectField(ctx.Request().Context(), fieldID)
	if err != nil {
		ctx.Logger().Errorf("GetObjectField: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostObjectField(ctx echo.Context) error {
	field := &db.ObjectField{}
	if err := ctx.Bind(field); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := field.ValidatePost(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, userID, db.OBJECT_TYPE_USER, db.ACCESS_OWNER)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostObjectField MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		field, err = db.Instance().PostObjectField(ctx.Request().Context(), ac, field)
		if err != nil {
			ctx.Logger().Errorf("PostObjectField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, field, 1)
	}

	result, err := client.PostObjectField(ctx.Request().Context(), *field)
	if err != nil {
		ctx.Logger().Errorf("PostObjectField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 1)
}

func PutObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	field := &db.ObjectField{}
	if err := field.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := field.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, userID, db.OBJECT_TYPE_USER, db.ACCESS_OWNER)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutObjectField MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		field, err = db.Instance().PutObjectField(ctx.Request().Context(), ac, fieldID, field)
		if err != nil {
			ctx.Logger().Errorf("PutObjectField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	result, err := client.PutObjectField(ctx.Request().Context(), fieldID, *field)
	if err != nil {
		ctx.Logger().Errorf("PutObjectField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func PatchObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	field := &db.ObjectField{}
	if err := field.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := field.ValidatePatch(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, userID, db.OBJECT_TYPE_USER, db.ACCESS_OWNER)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PatchObjectField MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		field, err = db.Instance().PatchObjectField(ctx.Request().Context(), ac, fieldID, field)
		if err != nil {
			ctx.Logger().Errorf("PatchObjectField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	result, err := client.PatchObjectField(ctx.Request().Context(), fieldID, *field)
	if err != nil {
		ctx.Logger().Errorf("PatchObjectField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func DeleteObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
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
			ctx.Logger().Errorf("DeleteObjectField MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		field, err := db.Instance().DeleteObjectField(ctx.Request().Context(), ac, fieldID)
		if err != nil {
			ctx.Logger().Errorf("DeleteObjectField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	result, err := client.DeleteObjectField(ctx.Request().Context(), fieldID)
	if err != nil {
		ctx.Logger().Errorf("DeleteObjectField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func GetUserFields(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		fields, err := db.Instance().GetUserFields(ctx.Request().Context(), fieldID)
		if err != nil {
			ctx.Logger().Errorf("GetUserFields: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, fields, len(fields))
	}

	body, err := client.GetUserFields(ctx.Request().Context(), fieldID)
	if err != nil {
		ctx.Logger().Errorf("GetUserFields: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostUserField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	field := &db.UserField{}
	if err := ctx.Bind(field); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	field.FieldID = fieldID
	if err := field.ValidatePost(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, field.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostUserField MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		field, err = db.Instance().PostUserField(ctx.Request().Context(), ac, field)
		if err != nil {
			ctx.Logger().Errorf("PostUserField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, field, 1)
	}

	result, err := client.PostUserField(ctx.Request().Context(), fieldID, *field)
	if err != nil {
		ctx.Logger().Errorf("PostUserField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 1)
}

func PutUserField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	valueID, err := uuid.Parse(ctx.Param("value_id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	field := &db.UserField{}
	if err := field.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := field.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, field.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutUserField MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		field, err = db.Instance().PutUserField(ctx.Request().Context(), ac, valueID, field)
		if err != nil {
			ctx.Logger().Errorf("PutUserField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	result, err := client.PutUserField(ctx.Request().Context(), fieldID, valueID, *field)
	if err != nil {
		ctx.Logger().Errorf("PutUserField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func PatchUserField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	valueID, err := uuid.Parse(ctx.Param("value_id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	field := &db.UserField{}
	if err := field.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := field.ValidatePatch(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, field.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PatchUserField MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		field, err = db.Instance().PatchUserField(ctx.Request().Context(), ac, valueID, field)
		if err != nil {
			ctx.Logger().Errorf("PatchUserField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	result, err := client.PatchUserField(ctx.Request().Context(), fieldID, valueID, *field)
	if err != nil {
		ctx.Logger().Errorf("PatchUserField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func DeleteUserField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	valueID, err := uuid.Parse(ctx.Param("value_id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		existing := &db.UserField{}
		err := db.Instance().NewSelect().
			Model(existing).
			Column("user_id").
			Where("id = ?", valueID).
			Where("sa_deleted_at IS NULL").
			Limit(1).
			Scan(ctx.Request().Context())
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, existing.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("DeleteUserField MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		field, err := db.Instance().DeleteUserField(ctx.Request().Context(), ac, valueID)
		if err != nil {
			ctx.Logger().Errorf("DeleteUserField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	result, err := client.DeleteUserField(ctx.Request().Context(), fieldID, valueID)
	if err != nil {
		ctx.Logger().Errorf("DeleteUserField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}
