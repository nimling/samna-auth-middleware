package echo

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/audit"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
)

func GetLocations(ctx echo.Context) error {
	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		with, err := db.QueryWithObject(ctx)
		if err != nil {
			ctx.Logger().Warn(err)
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		locs := make([]db.Location, 0)
		q := db.Instance().GetLocationsQuery(&locs, userID, db.ACCESS_READ)
		for _, w := range with {
			switch w {
			case db.OBJECT_TYPE_ORGANIZATION:
				q = q.Relation(db.RelationOwner())
			default:
				ctx.Logger().Warnf("unsupported with object: %s", w)
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
		}
		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetLocations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, locs, len(locs))
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

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, locationID, db.OBJECT_TYPE_LOCATION, db.ACCESS_READ)
		if err != nil || ac == nil {
			if err != nil && !errors.Is(err, db.ErrNoAccess) {
				ctx.Logger().Errorf("GetLocation MutationAccess: %v", err)
				return respondError(ctx, http.StatusInternalServerError, "Internal server error")
			}
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		with, err := db.QueryWithObject(ctx)
		if err != nil {
			ctx.Logger().Warn(err)
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		var loc db.Location
		q := db.Instance().GetLocationQuery(&loc, locationID)
		for _, w := range with {
			switch w {
			case db.OBJECT_TYPE_ORGANIZATION:
				q = q.Relation(db.RelationOwner())
			default:
				ctx.Logger().Warnf("unsupported with object: %s", w)
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
		}
		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetLocation: %v", err)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, loc, 1)
	}

	body, err := client.GetLocation(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocation: %v", err)
		return respondError(ctx, http.StatusForbidden, "Forbidden")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocationAudit(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		audits, err := db.Instance().GetLocationAudit(ctx.Request().Context(), userID, locationID, db.ACCESS_READ)
		if err != nil {
			ctx.Logger().Errorf("GetLocationAudit: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, audits, len(audits))
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
	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		loc := &db.Location{}
		if err := ctx.Bind(loc); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := loc.ValidatePost(); err != nil {
			return respondError(ctx, http.StatusBadRequest, err.Error())
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, loc.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostLocation MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		loc, err = db.Instance().PostLocation(ctx.Request().Context(), ac, loc)
		if err != nil {
			ctx.Logger().Errorf("PostLocation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		locName := ""
		if loc.Name != nil {
			locName = *loc.Name
		}
		audit.Write("PostLocation", "create", db.OBJECT_TYPE_LOCATION, loc.ID, loc.OwnerID, ac.UserClaimID, "Created location "+locName)
		return respondJSON(ctx, http.StatusCreated, loc, 1)
	}

	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := loc.ValidatePost(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}
	result, err := client.PostLocation(ctx.Request().Context(), loc)
	if err != nil {
		ctx.Logger().Errorf("PostLocation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 1)
}

func PutLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, locationID, db.OBJECT_TYPE_LOCATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutLocation MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		existing, err := db.Instance().GetLocation(ctx.Request().Context(), locationID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		loc := &db.Location{}
		if err := ctx.Bind(loc); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := loc.ValidatePut(); err != nil {
			return respondError(ctx, http.StatusBadRequest, err.Error())
		}
		if loc.OwnerID != uuid.Nil && loc.OwnerID != existing.OwnerID {
			if newAC, accErr := db.Instance().MutationAccess(ctx.Request().Context(), userID, loc.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE); accErr != nil || newAC == nil {
				if accErr != nil {
					ctx.Logger().Warn(accErr)
				}
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		loc, err = db.Instance().PutLocation(ctx.Request().Context(), ac, locationID, loc)
		if err != nil {
			ctx.Logger().Errorf("PutLocation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		locName := ""
		if loc.Name != nil {
			locName = *loc.Name
		}
		audit.Write("PutLocation", "update", db.OBJECT_TYPE_LOCATION, loc.ID, loc.OwnerID, ac.UserClaimID, "Updated location "+locName)
		return respondJSON(ctx, http.StatusOK, loc, 1)
	}

	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := loc.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}
	result, err := client.PutLocation(ctx.Request().Context(), locationID, loc)
	if err != nil {
		ctx.Logger().Errorf("PutLocation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func DeleteLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, locationID, db.OBJECT_TYPE_LOCATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("DeleteLocation MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		loc, err := db.Instance().DeleteLocation(ctx.Request().Context(), ac, locationID)
		if err != nil {
			ctx.Logger().Errorf("DeleteLocation: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		locName := ""
		if loc.Name != nil {
			locName = *loc.Name
		}
		audit.Write("DeleteLocation", "delete", db.OBJECT_TYPE_LOCATION, loc.ID, loc.OwnerID, ac.UserClaimID, "Deleted location "+locName)
		return respondJSON(ctx, http.StatusOK, loc, 1)
	}

	result, err := client.DeleteLocation(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("DeleteLocation: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}
