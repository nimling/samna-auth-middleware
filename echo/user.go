package echo

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/uptrace/bun"
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

	params := ctx.QueryParams()

	orgIDs := make([]uuid.UUID, 0)
	for _, raw := range params["org_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, perr := uuid.Parse(s)
			if perr != nil {
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
			id, perr := uuid.Parse(s)
			if perr != nil {
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
			id, perr := uuid.Parse(s)
			if perr != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			usrIDs = append(usrIDs, id)
		}
	}

	if getDebugUserID() != nil {
		relations := make([]db.UserRelation, 0)
		q := db.Instance().NewSelect().
			Model(&relations).
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

	if getDebugUserID() != nil {
		out, err := db.Instance().PostUser(ctx.Request().Context(), userID, user)
		if err != nil {
			ctx.Logger().Errorf("PostUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, out, 0)
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

	if getDebugUserID() != nil {
		if err := db.Instance().DeleteUser(ctx.Request().Context(), userID); err != nil {
			ctx.Logger().Errorf("DeleteUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteUser(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("DeleteUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func PostUserToken(ctx echo.Context) error {
	var payload db.SamnaUserToken
	if err := ctx.Bind(&payload); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostUserToken(ctx.Request().Context(), payload)
	if err != nil {
		ctx.Logger().Errorf("PostUserToken: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func PostUserTokenValidate(ctx echo.Context) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := ctx.Bind(&body); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostUserTokenValidate(ctx.Request().Context(), body.Token)
	if err != nil {
		ctx.Logger().Errorf("PostUserTokenValidate: %v", err)
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
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

