package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
)

func GetCodes(ctx echo.Context) error {
	body, err := client.GetCodes(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetCodes: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetCode(ctx echo.Context) error {
	codeID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.GetCode(ctx.Request().Context(), codeID)
	if err != nil {
		ctx.Logger().Errorf("GetCode: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostCode(ctx echo.Context) error {
	var reqBody map[string]any
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PostCode(ctx.Request().Context(), reqBody)
	if err != nil {
		ctx.Logger().Errorf("PostCode: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func PutCode(ctx echo.Context) error {
	codeID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var reqBody map[string]any
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PutCode(ctx.Request().Context(), codeID, reqBody)
	if err != nil {
		ctx.Logger().Errorf("PutCode: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteCode(ctx echo.Context) error {
	codeID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.DeleteCode(ctx.Request().Context(), codeID)
	if err != nil {
		ctx.Logger().Errorf("DeleteCode: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
