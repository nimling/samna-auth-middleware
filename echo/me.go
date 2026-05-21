package echo

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/nimling/samna-auth-middleware/db"
)

func GetMe(ctx echo.Context) error {
	user, err := db.Instance().GetMe(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMe: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	return respondJSON(ctx, http.StatusOK, user, 1)
}

func PutMe(ctx echo.Context) error {
	user := &db.SamnaUser{}
	if err := user.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := user.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}
	updated, err := db.Instance().UpdateMe(ctx, user)
	if err != nil {
		if errors.Is(err, db.ErrNoAccess) {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		ctx.Logger().Errorf("PutMe: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, updated, 1)
}

func PatchMe(ctx echo.Context) error {
	user := &db.SamnaUser{}
	if err := user.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := user.ValidatePatch(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}
	updated, err := db.Instance().PatchMe(ctx, user)
	if err != nil {
		if errors.Is(err, db.ErrNoAccess) {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		ctx.Logger().Errorf("PatchMe: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, updated, 1)
}

func GetMeClaims(ctx echo.Context) error {
	claims, err := db.Instance().GetMeClaims(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeClaims: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, claims, len(claims))
}

func GetMeRelations(ctx echo.Context) error {
	relations, err := db.Instance().GetMeRelations(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeRelations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, relations, len(relations))
}

func GetMeOrganizations(ctx echo.Context) error {
	orgs, err := db.Instance().GetMeOrganizations(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeOrganizations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, orgs, len(orgs))
}

func GetMeLocations(ctx echo.Context) error {
	locs, err := db.Instance().GetMeLocations(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeLocations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, locs, len(locs))
}

func GetMeAssets(ctx echo.Context) error {
	assets, err := db.Instance().GetMeAssets(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeAssets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, assets, len(assets))
}

func GetMeAudit(ctx echo.Context) error {
	audits, err := db.Instance().GetMeAudit(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeAudit: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, audits, len(audits))
}
