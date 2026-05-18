package echo

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
)

func GetMe(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if db.GetDebugMode() != db.DebugModeNone {
		user, err := db.Instance().GetUser(ctx.Request().Context(), userID, userID)
		if err != nil {
			ctx.Logger().Errorf("GetMe: %v", err)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, user, 1)
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

	user := &db.SamnaUser{}
	if err := user.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := user.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, userID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutMe MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		user, err = db.Instance().PutUser(ctx.Request().Context(), ac, userID, user)
		if err != nil {
			ctx.Logger().Errorf("PutMe: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, user, 1)
	}

	result, err := client.PutUser(ctx.Request().Context(), userID, *user)
	if err != nil {
		ctx.Logger().Errorf("PutMe: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func PatchMe(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	user := &db.SamnaUser{}
	if err := user.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := user.ValidatePatch(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, userID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PatchMe MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		user, err = db.Instance().PatchUser(ctx.Request().Context(), ac, userID, user)
		if err != nil {
			ctx.Logger().Errorf("PatchMe: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, user, 1)
	}

	result, err := client.PatchUser(ctx.Request().Context(), userID, *user)
	if err != nil {
		ctx.Logger().Errorf("PatchMe: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func GetMeClaims(ctx echo.Context) error {
	userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if db.GetDebugMode() != db.DebugModeNone {
		with, err := db.QueryWithObject(ctx)
		if err != nil {
			ctx.Logger().Warn(err)
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		claims := make([]db.Claim, 0)
		q := db.Instance().GetClaimsQuery(&claims, userID, db.ACCESS_NONE)
		for _, w := range with {
			switch w {
			case db.OBJECT_TYPE_CLAIM_OBJECT:
				q = q.Relation(db.RelationClaimObjects())
			case db.OBJECT_TYPE_USER_CLAIM:
				q = q.Relation(db.RelationClaimUsers())
			case db.OBJECT_TYPE_ORGANIZATION:
				q = q.Relation(db.RelationOwner())
			default:
				ctx.Logger().Warnf("unsupported with object: %s", w)
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
		}
		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetMeClaims: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, claims, len(claims))
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

	if db.GetDebugMode() != db.DebugModeNone {
		relations, err := db.Instance().GetUserRelations(ctx.Request().Context(), userID)
		if err != nil {
			ctx.Logger().Errorf("GetMeRelations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, relations, len(relations))
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

	if db.GetDebugMode() != db.DebugModeNone {
		with, err := db.QueryWithObject(ctx)
		if err != nil {
			ctx.Logger().Warn(err)
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		orgs := make([]db.Organization, 0)
		q := db.Instance().GetOrganizationsQuery(&orgs, userID, db.ACCESS_NONE)
		for _, w := range with {
			switch w {
			case db.OBJECT_TYPE_ORGANIZATION:
				q = q.Relation(db.RelationParent())
			default:
				ctx.Logger().Warnf("unsupported with object: %s", w)
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
		}
		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetMeOrganizations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, orgs, len(orgs))
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

	if db.GetDebugMode() != db.DebugModeNone {
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
			ctx.Logger().Errorf("GetMeLocations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, locs, len(locs))
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

	if db.GetDebugMode() != db.DebugModeNone {
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

	if db.GetDebugMode() != db.DebugModeNone {
		audits, err := db.Instance().GetUserAudit(ctx.Request().Context(), userID, db.ACCESS_READ)
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
