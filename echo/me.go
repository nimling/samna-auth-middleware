package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
)

func GetMe(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if getDebugUserID() != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.UserRelations())
		user, err := db.Instance().GetUser(ctx.Request().Context(), userID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetMe: %v", err)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, user, 0)
	}

	body, err := client.GetUser(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetMe: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PutMe(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if getDebugUserID() != nil {
		var user db.SamnaUser
		if err := ctx.Bind(&user); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().PutUser(ctx.Request().Context(), userID, &user); err != nil {
			ctx.Logger().Errorf("PutMe: %v", err)
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
		ctx.Logger().Errorf("PutMe: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func GetMeClaims(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if getDebugUserID() != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.ClaimRelations())
		claims, err := db.Instance().GetClaims(ctx.Request().Context(), userID, db.ACCESS_NONE, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetMeClaims: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, claims, 0)
	}

	body, err := client.GetUserClaims(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetMeClaims: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeRelations(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if getDebugUserID() != nil {
		relations, err := db.Instance().GetUserRelations(ctx.Request().Context(), userID)
		if err != nil {
			ctx.Logger().Errorf("GetMeRelations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, relations, 0)
	}

	body, err := client.GetUserConnections(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetMeRelations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeOrganizations(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if getDebugUserID() != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.OrganizationRelations())
		orgs, err := db.Instance().GetOrganizations(ctx.Request().Context(), userID, db.ACCESS_NONE, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetMeOrganizations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, orgs, 0)
	}

	body, err := client.GetUserOrganizations(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetMeOrganizations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeLocations(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if getDebugUserID() != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		locs, err := db.Instance().GetLocations(ctx.Request().Context(), userID, db.ACCESS_READ, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetMeLocations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, locs, 0)
	}

	body, err := client.GetUserLocations(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetMeLocations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}


func GetMeAssets(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if getDebugUserID() != nil {
		assets, err := db.Instance().GetUserAssets(ctx.Request().Context(), userID)
		if err != nil {
			ctx.Logger().Errorf("GetMeAssets: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, assets, len(assets))
	}

	body, err := client.GetUserAssets(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetMeAssets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeAudit(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if getDebugUserID() != nil {
		audits, err := db.Instance().GetUserAudit(ctx.Request().Context(), userID, userID, db.ACCESS_READ)
		if err != nil {
			ctx.Logger().Errorf("GetMeAudit: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, audits, len(audits))
	}

	body, err := client.GetUserAudit(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetMeAudit: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
