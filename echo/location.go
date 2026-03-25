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
		qa := db.NewQueryAccess(*debugUserID)
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		locs, err := db.Instance().GetLocations(ctx.Request().Context(), qa.LocationWhere(db.ACCESS_LEVEL_MEMBER), relations...)
		if err != nil {
			ctx.Logger().Errorf("GetLocations: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, locs, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.GetLocations(ctx.Request().Context(), meta)
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
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &locationID, int(db.ACCESS_LEVEL_MEMBER))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		loc, err := db.Instance().GetLocationByID(ctx.Request().Context(), locationID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetLocation: %v", err)
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		return RespondJSON(ctx, http.StatusOK, loc, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.GetLocation(ctx.Request().Context(), meta, locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocation: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocationClaims(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetLocationClaims: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetLocationClaims(ctx.Request().Context(), meta, locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocationClaims: %v", err)
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

	meta := metaFromContext(ctx)
	body, err := client.GetLocationAssets(ctx.Request().Context(), meta, locationID)
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
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &ownerID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		loc.CreatedBy = claims[0].ID
		loc.RootID = claims[0].RootID
		if err := db.Instance().CreateLocation(ctx.Request().Context(), &loc); err != nil {
			ctx.Logger().Errorf("PostLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusCreated, loc, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.PostLocation(ctx.Request().Context(), meta, nil)
	if err != nil {
		ctx.Logger().Errorf("PostLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func PutLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &locationID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		var loc db.Location
		if err := ctx.Bind(&loc); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().UpdateLocation(ctx.Request().Context(), locationID, &loc); err != nil {
			ctx.Logger().Errorf("PutLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, loc, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.PutLocation(ctx.Request().Context(), meta, locationID, nil)
	if err != nil {
		ctx.Logger().Errorf("PutLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &locationID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if err := db.Instance().DeleteLocation(ctx.Request().Context(), locationID); err != nil {
			ctx.Logger().Errorf("DeleteLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	meta := metaFromContext(ctx)
	body, err := client.DeleteLocation(ctx.Request().Context(), meta, locationID)
	if err != nil {
		ctx.Logger().Errorf("DeleteLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
