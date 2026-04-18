package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetObjectFields(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetObjectFields(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetObjectFields: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetObjectField(ctx.Request().Context(), fieldID)
	if err != nil {
		ctx.Logger().Errorf("GetObjectField: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostObjectField(ctx echo.Context) error {
	var field db.ObjectField
	if err := ctx.Bind(&field); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.PostObjectField(ctx.Request().Context(), field)
	if err != nil {
		ctx.Logger().Errorf("PostObjectField: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusCreated, result, nil)
}

func PutObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var field db.ObjectField
	if err := ctx.Bind(&field); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.PutObjectField(ctx.Request().Context(), fieldID, field)
	if err != nil {
		ctx.Logger().Errorf("PutObjectField: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func DeleteObjectField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.DeleteObjectField(ctx.Request().Context(), fieldID)
	if err != nil {
		ctx.Logger().Errorf("DeleteObjectField: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func GetUserFields(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetUserFields(ctx.Request().Context(), fieldID)
	if err != nil {
		ctx.Logger().Errorf("GetUserFields: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostUserField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var field db.UserField
	if err := ctx.Bind(&field); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.PostUserField(ctx.Request().Context(), fieldID, field)
	if err != nil {
		ctx.Logger().Errorf("PostUserField: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusCreated, result, nil)
}

func PutUserField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	valueID, err := uuid.Parse(ctx.Param("value_id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var field db.UserField
	if err := ctx.Bind(&field); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.PutUserField(ctx.Request().Context(), fieldID, valueID, field)
	if err != nil {
		ctx.Logger().Errorf("PutUserField: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func DeleteUserField(ctx echo.Context) error {
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	valueID, err := uuid.Parse(ctx.Param("value_id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.DeleteUserField(ctx.Request().Context(), fieldID, valueID)
	if err != nil {
		ctx.Logger().Errorf("DeleteUserField: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}
