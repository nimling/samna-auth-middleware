package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetObjectFields(ctx echo.Context) error {
	if getDebugUserID() != nil {
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

	if getDebugUserID() != nil {
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
	var field db.ObjectField
	if err := ctx.Bind(&field); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		if err := db.Instance().PostObjectField(ctx.Request().Context(), &field); err != nil {
			ctx.Logger().Errorf("PostObjectField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, field, 1)
	}

	result, err := client.PostObjectField(ctx.Request().Context(), field)
	if err != nil {
		ctx.Logger().Errorf("PostObjectField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func PutObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var field db.ObjectField
	if err := ctx.Bind(&field); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		if err := db.Instance().PutObjectField(ctx.Request().Context(), fieldID, &field); err != nil {
			ctx.Logger().Errorf("PutObjectField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	result, err := client.PutObjectField(ctx.Request().Context(), fieldID, field)
	if err != nil {
		ctx.Logger().Errorf("PutObjectField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func DeleteObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		field, err := db.Instance().DeleteObjectField(ctx.Request().Context(), fieldID)
		if err != nil {
			ctx.Logger().Errorf("DeleteObjectField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 0)
	}

	result, err := client.DeleteObjectField(ctx.Request().Context(), fieldID)
	if err != nil {
		ctx.Logger().Errorf("DeleteObjectField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func GetUserFields(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
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

	var field db.UserField
	if err := ctx.Bind(&field); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	field.FieldID = fieldID

	if getDebugUserID() != nil {
		if err := db.Instance().PostUserField(ctx.Request().Context(), &field); err != nil {
			ctx.Logger().Errorf("PostUserField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, field, 1)
	}

	result, err := client.PostUserField(ctx.Request().Context(), fieldID, field)
	if err != nil {
		ctx.Logger().Errorf("PostUserField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
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

	var field db.UserField
	if err := ctx.Bind(&field); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		if err := db.Instance().PutUserField(ctx.Request().Context(), valueID, &field); err != nil {
			ctx.Logger().Errorf("PutUserField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 1)
	}

	result, err := client.PutUserField(ctx.Request().Context(), fieldID, valueID, field)
	if err != nil {
		ctx.Logger().Errorf("PutUserField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
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

	if getDebugUserID() != nil {
		field, err := db.Instance().DeleteUserField(ctx.Request().Context(), valueID)
		if err != nil {
			ctx.Logger().Errorf("DeleteUserField: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, field, 0)
	}

	result, err := client.DeleteUserField(ctx.Request().Context(), fieldID, valueID)
	if err != nil {
		ctx.Logger().Errorf("DeleteUserField: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}
