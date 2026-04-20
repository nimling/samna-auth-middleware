package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
)

func GetClients(ctx echo.Context) error {
	body, err := client.GetClients(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetClients: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetSamnaClient(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.GetSamnaClient(ctx.Request().Context(), clientID)
	if err != nil {
		ctx.Logger().Errorf("GetSamnaClient: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostSamnaClient(ctx echo.Context) error {
	var reqBody map[string]any
	if err := ctx.Bind(&reqBody); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PostSamnaClient(ctx.Request().Context(), reqBody)
	if err != nil {
		ctx.Logger().Errorf("PostSamnaClient: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func PutSamnaClient(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var reqBody map[string]any
	if err := ctx.Bind(&reqBody); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PutSamnaClient(ctx.Request().Context(), clientID, reqBody)
	if err != nil {
		ctx.Logger().Errorf("PutSamnaClient: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteSamnaClient(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.DeleteSamnaClient(ctx.Request().Context(), clientID)
	if err != nil {
		ctx.Logger().Errorf("DeleteSamnaClient: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostSamnaClientRotate(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PostSamnaClientRotate(ctx.Request().Context(), clientID)
	if err != nil {
		ctx.Logger().Errorf("PostSamnaClientRotate: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostSamnaClientClaim(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var reqBody map[string]any
	if err := ctx.Bind(&reqBody); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PostSamnaClientClaim(ctx.Request().Context(), clientID, reqBody)
	if err != nil {
		ctx.Logger().Errorf("PostSamnaClientClaim: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func DeleteSamnaClientClaim(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.DeleteSamnaClientClaim(ctx.Request().Context(), clientID, claimID)
	if err != nil {
		ctx.Logger().Errorf("DeleteSamnaClientClaim: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
