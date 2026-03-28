package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetUsers(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetUsers: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetUsers(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetUsers: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.UserRelations())
		user, err := db.Instance().GetUserByID(ctx.Request().Context(), userID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetUser: %v", err)
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		return RespondJSON(ctx, http.StatusOK, user, nil)
	}

	body, err := client.GetUser(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUser: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserClaims(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		claims, err := db.Instance().GetUserClaims(ctx.Request().Context(), userID)
		if err != nil {
			ctx.Logger().Errorf("GetUserClaims: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, claims, nil)
	}

	body, err := client.GetUserClaims(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUserClaims: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserConnections(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetUserConnections: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetUserConnections(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUserConnections: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PostUser: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	var user db.SamnaUser
	if err := ctx.Bind(&user); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostUser(ctx.Request().Context(), userID, user)
	if err != nil {
		ctx.Logger().Errorf("PostUser: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusCreated, result, nil)
}

func PutUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		var user db.SamnaUser
		if err := ctx.Bind(&user); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().UpdateUser(ctx.Request().Context(), userID, &user); err != nil {
			ctx.Logger().Errorf("PutUser: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, user, nil)
	}

	var user db.SamnaUser
	if err := ctx.Bind(&user); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PutUser(ctx.Request().Context(), userID, user)
	if err != nil {
		ctx.Logger().Errorf("PutUser: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func DeleteUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("DeleteUser: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	result, err := client.DeleteUser(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("DeleteUser: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func GetUserClients(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetUserClients: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetUserClients(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUserClients: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserClient(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	clientID, err := uuid.Parse(ctx.Param("client_id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetUserClient: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetUserClient(ctx.Request().Context(), userID, clientID)
	if err != nil {
		ctx.Logger().Errorf("GetUserClient: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserAssets(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.GetUserAssets(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUserAssets: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PutUserClient(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	clientID, err := uuid.Parse(ctx.Param("client_id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PutUserClient: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.PutUserClient(ctx.Request().Context(), userID, clientID, nil)
	if err != nil {
		ctx.Logger().Errorf("PutUserClient: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
