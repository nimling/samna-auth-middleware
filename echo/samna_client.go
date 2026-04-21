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
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetClient(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.GetClient(ctx.Request().Context(), clientID)
	if err != nil {
		ctx.Logger().Errorf("GetClient: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostClient(ctx echo.Context) error {
	var reqBody map[string]any
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PostClient(ctx.Request().Context(), reqBody)
	if err != nil {
		ctx.Logger().Errorf("PostClient: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func PutClient(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var reqBody map[string]any
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PutClient(ctx.Request().Context(), clientID, reqBody)
	if err != nil {
		ctx.Logger().Errorf("PutClient: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteClient(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.DeleteClient(ctx.Request().Context(), clientID)
	if err != nil {
		ctx.Logger().Errorf("DeleteClient: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostClientRotate(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PostClientRotate(ctx.Request().Context(), clientID)
	if err != nil {
		ctx.Logger().Errorf("PostClientRotate: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostClientClaim(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var reqBody map[string]any
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.PostClientClaim(ctx.Request().Context(), clientID, reqBody)
	if err != nil {
		ctx.Logger().Errorf("PostClientClaim: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func DeleteClientClaim(ctx echo.Context) error {
	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.DeleteClientClaim(ctx.Request().Context(), clientID, claimID)
	if err != nil {
		ctx.Logger().Errorf("DeleteClientClaim: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
