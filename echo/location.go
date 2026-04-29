package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetLocations(ctx echo.Context) error {
	if debugUserID := getDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		locs, err := db.Instance().GetLocations(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_MEMBER, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetLocations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, locs, 0)
	}

	body, err := client.GetLocations(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetLocations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, locationID, db.OBJECT_TYPE_LOCATION, db.ACCESS_LEVEL_MEMBER)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, locationID, db.OBJECT_TYPE_LOCATION)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusNotFound, "Not found")
			}
		}
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		loc, err := db.Instance().GetLocation(ctx.Request().Context(), locationID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetLocation: %v", err)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, loc, 0)
	}

	body, err := client.GetLocation(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocation: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocationAudit(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		audits, err := db.Instance().GetLocationAudit(ctx.Request().Context(), locationID)
		if err != nil {
			ctx.Logger().Errorf("GetLocationAudit: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, audits, 0)
	}

	body, err := client.GetLocationAudit(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocationAudit: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocationAssets(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.GetLocationAssets(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocationAssets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostLocation(ctx echo.Context) error {
	if debugUserID := getDebugUserID(); debugUserID != nil {
		var loc db.Location
		if err := ctx.Bind(&loc); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, loc.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_LEVEL_WRITE)
		if err != nil {
			ctx.Logger().Errorf("PostLocation GetAccess: %v", err)
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, loc.OwnerID, db.OBJECT_TYPE_ORGANIZATION)
			if err != nil {
				ctx.Logger().Errorf("PostLocation GetDirectAccess: %v", err)
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		if ac == nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		loc.ID = uuid.UUID{}
		loc.CreatedBy = *ac.UserClaimID
		if err := db.Instance().PostLocation(ctx.Request().Context(), &loc); err != nil {
			ctx.Logger().Errorf("PostLocation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, loc, 0)
	}

	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostLocation(ctx.Request().Context(), loc)
	if err != nil {
		ctx.Logger().Errorf("PostLocation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func PutLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, locationID, db.OBJECT_TYPE_LOCATION, db.ACCESS_LEVEL_WRITE)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, locationID, db.OBJECT_TYPE_LOCATION)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		var loc db.Location
		if err := ctx.Bind(&loc); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().PutLocation(ctx.Request().Context(), locationID, &loc); err != nil {
			ctx.Logger().Errorf("PutLocation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, loc, 0)
	}

	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PutLocation(ctx.Request().Context(), locationID, loc)
	if err != nil {
		ctx.Logger().Errorf("PutLocation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func DeleteLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, locationID, db.OBJECT_TYPE_LOCATION, db.ACCESS_LEVEL_WRITE)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, locationID, db.OBJECT_TYPE_LOCATION)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		if err := db.Instance().DeleteLocation(ctx.Request().Context(), locationID); err != nil {
			ctx.Logger().Errorf("DeleteLocation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteLocation(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("DeleteLocation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}
