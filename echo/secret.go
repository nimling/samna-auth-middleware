package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetSecrets(ctx echo.Context) error {
	if debugUserID := getDebugUserID(); debugUserID != nil {
		secrets, err := db.Instance().GetSecrets(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_GUEST)
		if err != nil {
			ctx.Logger().Errorf("GetSecrets: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, secrets, len(secrets))
	}

	body, err := client.GetSecrets(ctx.Request().Context())
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

	if getDebugUserID() != nil {
		secret, err := db.Instance().GetSecret(ctx.Request().Context(), secretID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, secret, 1)
	}

	body, err := client.GetSecret(ctx.Request().Context(), secretID)
	if err != nil {
		ctx.Logger().Errorf("GetSecret: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostSecret(ctx echo.Context) error {
	var reqBody db.BodySecret
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		secret, err := db.Instance().PostSecret(ctx.Request().Context(), reqBody)
		if err != nil {
			ctx.Logger().Errorf("PostSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, err.Error())
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

	var reqBody db.BodySecret
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		secret, err := db.Instance().PutSecret(ctx.Request().Context(), secretID, reqBody)
		if err != nil {
			ctx.Logger().Errorf("PutSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, err.Error())
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

	if getDebugUserID() != nil {
		if err := db.Instance().DeleteSecret(ctx.Request().Context(), secretID); err != nil {
			ctx.Logger().Errorf("DeleteSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	body, err := client.DeleteSecret(ctx.Request().Context(), secretID)
	if err != nil {
		ctx.Logger().Errorf("DeleteSecret: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
