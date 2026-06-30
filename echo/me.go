package echo

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	discipledb "github.com/nimling/samna-auth-middleware/disciple/db"
	"github.com/nimling/samna-auth-middleware/shared"
)

func GetMe(ctx echo.Context) error {
	user, err := discipledb.Instance().GetMe(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMe: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	return respondJSON(ctx, http.StatusOK, user, 1)
}

func PutMe(ctx echo.Context) error {
	user := &shared.SamnaUser{}
	if err := user.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := user.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}
	updated, err := discipledb.Instance().UpdateMe(ctx, user)
	if err != nil {
		if errors.Is(err, shared.ErrNoAccess) {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		ctx.Logger().Errorf("PutMe: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, updated, 1)
}

func PatchMe(ctx echo.Context) error {
	user := &shared.SamnaUser{}
	if err := user.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := user.ValidatePatch(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}
	updated, err := discipledb.Instance().UpdateMe(ctx, user)
	if err != nil {
		if errors.Is(err, shared.ErrNoAccess) {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		ctx.Logger().Errorf("PatchMe: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, updated, 1)
}

func GetMeClaims(ctx echo.Context) error {
	claims, err := discipledb.Instance().GetMeClaims(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeClaims: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, claims, len(claims))
}

func GetMeRelations(ctx echo.Context) error {
	relations, err := discipledb.Instance().GetMeRelations(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeRelations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, relations, len(relations))
}

func GetMeOrganizations(ctx echo.Context) error {
	orgs, err := discipledb.Instance().GetMeOrganizations(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeOrganizations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, orgs, len(orgs))
}

func GetMeLocations(ctx echo.Context) error {
	locs, err := discipledb.Instance().GetMeLocations(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeLocations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, locs, len(locs))
}

func GetMeAssets(ctx echo.Context) error {
	assets, err := discipledb.Instance().GetMeAssets(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeAssets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, assets, len(assets))
}

func GetMeAudit(ctx echo.Context) error {
	audits, err := discipledb.Instance().GetMeAudit(ctx)
	if err != nil {
		ctx.Logger().Errorf("GetMeAudit: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, audits, len(audits))
}
