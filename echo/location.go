package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetLocations(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		locs, err := db.Instance().GetLocations(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_MEMBER, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetLocations: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, locs, nil)
	}

	body, err := client.GetLocations(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetLocations: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, &locationID, db.ACCESS_LEVEL_MEMBER)
		if err != nil || claims == nil {
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		loc, err := db.Instance().GetLocation(ctx.Request().Context(), locationID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetLocation: %v", err)
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		return RespondJSON(ctx, http.StatusOK, loc, nil)
	}

	body, err := client.GetLocation(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocation: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocationAudit(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		audits, err := db.Instance().GetLocationAudit(ctx.Request().Context(), locationID)
		if err != nil {
			ctx.Logger().Errorf("GetLocationAudit: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, audits, nil)
	}

	body, err := client.GetLocationAudit(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocationAudit: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocationAssets(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.GetLocationAssets(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocationAssets: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostLocation(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		var loc db.Location
		if err := ctx.Bind(&loc); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		ownerID := loc.OwnerID
		claims, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, &ownerID, db.ACCESS_LEVEL_WRITE)
		if err != nil {
			ctx.Logger().Errorf("PostLocation ClaimAccess: %v", err)
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if claims == nil {
			ctx.Logger().Infof("PostLocation: no access for user %s on object %s", *debugUserID, ownerID)
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		loc.CreatedBy = claims.ID
		loc.RootID = claims.RootID
		if err := db.Instance().PostLocation(ctx.Request().Context(), &loc); err != nil {
			ctx.Logger().Errorf("PostLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusCreated, loc, nil)
	}

	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostLocation(ctx.Request().Context(), loc)
	if err != nil {
		ctx.Logger().Errorf("PostLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusCreated, result, nil)
}

func PutLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, &locationID, db.ACCESS_LEVEL_WRITE)
		if err != nil || claims == nil {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		var loc db.Location
		if err := ctx.Bind(&loc); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().PutLocation(ctx.Request().Context(), locationID, &loc); err != nil {
			ctx.Logger().Errorf("PutLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, loc, nil)
	}

	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PutLocation(ctx.Request().Context(), locationID, loc)
	if err != nil {
		ctx.Logger().Errorf("PutLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func DeleteLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, &locationID, db.ACCESS_LEVEL_WRITE)
		if err != nil || claims == nil {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if err := db.Instance().DeleteLocation(ctx.Request().Context(), locationID); err != nil {
			ctx.Logger().Errorf("DeleteLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteLocation(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("DeleteLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}
