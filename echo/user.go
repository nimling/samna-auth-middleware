package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetUsers(ctx echo.Context) error {
	if debugUserID := getDebugUserID(); debugUserID != nil {
		users, err := db.Instance().GetUsers(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_GUEST)
		if err != nil {
			ctx.Logger().Errorf("GetUsers: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, users, 0)
	}

	body, err := client.GetUsers(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetUsers: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.UserRelations())
		user, err := db.Instance().GetUser(ctx.Request().Context(), userID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetUser: %v", err)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, user, 0)
	}

	body, err := client.GetUser(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUser: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserAudit(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		audits, err := db.Instance().GetUserAudit(ctx.Request().Context(), userID)
		if err != nil {
			ctx.Logger().Errorf("GetUserAudit: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, audits, 0)
	}

	body, err := client.GetUserAudit(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUserAudit: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserClaims(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		claims, err := db.Instance().GetUserClaims(ctx.Request().Context(), userID)
		if err != nil {
			ctx.Logger().Errorf("GetUserClaims: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, claims, 0)
	}

	body, err := client.GetUserClaims(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUserClaims: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserRelations(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		relations, err := db.Instance().GetUserRelations(ctx.Request().Context(), userID)
		if err != nil {
			ctx.Logger().Errorf("GetUserRelations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, relations, 0)
	}

	body, err := client.GetUserConnections(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUserRelations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var user db.SamnaUser
	if err := ctx.Bind(&user); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostUser(ctx.Request().Context(), userID, user)
	if err != nil {
		ctx.Logger().Errorf("PostUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func PutUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		var user db.SamnaUser
		if err := ctx.Bind(&user); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().PutUser(ctx.Request().Context(), userID, &user); err != nil {
			ctx.Logger().Errorf("PutUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, user, 0)
	}

	var user db.SamnaUser
	if err := ctx.Bind(&user); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PutUser(ctx.Request().Context(), userID, user)
	if err != nil {
		ctx.Logger().Errorf("PutUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func DeleteUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.DeleteUser(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("DeleteUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func GetUserAssets(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		assets, err := db.Instance().GetUserAssets(ctx.Request().Context(), userID)
		if err != nil {
			ctx.Logger().Errorf("GetUserAssets: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, assets, len(assets))
	}

	body, err := client.GetUserAssets(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetUserAssets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

