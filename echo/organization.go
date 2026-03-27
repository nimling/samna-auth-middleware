package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetOrganizations(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.OrganizationRelations())
		orgs, err := db.Instance().GetOrganizations(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_GUEST, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetOrganizations: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, orgs, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.GetOrganizations(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetOrganizations: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &orgID, int(db.ACCESS_LEVEL_GUEST))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.OrganizationRelations())
		org, err := db.Instance().GetOrganizationByID(ctx.Request().Context(), orgID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetOrganization: %v", err)
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		return RespondJSON(ctx, http.StatusOK, org, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.GetOrganization(ctx.Request().Context(), meta, orgID)
	if err != nil {
		ctx.Logger().Errorf("GetOrganization: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganizationGraph(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetOrganizationGraph: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetOrganizationGraph(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetOrganizationGraph: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganizationClaims(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetOrganizationClaims: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetOrganizationClaims(ctx.Request().Context(), meta, orgID)
	if err != nil {
		ctx.Logger().Errorf("GetOrganizationClaims: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganizationAssets(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetOrganizationAssets(ctx.Request().Context(), meta, orgID)
	if err != nil {
		ctx.Logger().Errorf("GetOrganizationAssets: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostOrganization(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		var org db.Organization
		if err := ctx.Bind(&org); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if org.OwnerID == nil {
			return RespondError(ctx, http.StatusBadRequest, "owner_id is required")
		}
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, org.OwnerID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		org.CreatedBy = claims[0].ID
		org.RootID = &claims[0].RootID
		if err := db.Instance().CreateOrganization(ctx.Request().Context(), &org); err != nil {
			ctx.Logger().Errorf("PostOrganization: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusCreated, org, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.PostOrganization(ctx.Request().Context(), meta, nil)
	if err != nil {
		ctx.Logger().Errorf("PostOrganization: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func PutOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		existing, err := db.Instance().GetOrganizationByID(ctx.Request().Context(), orgID)
		if err != nil {
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		accessCheckID := orgID
		if existing.OwnerID != nil {
			accessCheckID = *existing.OwnerID
		}
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &accessCheckID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		var org db.Organization
		if err := ctx.Bind(&org); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().UpdateOrganization(ctx.Request().Context(), orgID, &org); err != nil {
			ctx.Logger().Errorf("PutOrganization: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, org, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.PutOrganization(ctx.Request().Context(), meta, orgID, nil)
	if err != nil {
		ctx.Logger().Errorf("PutOrganization: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		existing, err := db.Instance().GetOrganizationByID(ctx.Request().Context(), orgID)
		if err != nil {
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		accessCheckID := orgID
		if existing.OwnerID != nil {
			accessCheckID = *existing.OwnerID
		}
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &accessCheckID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if err := db.Instance().DeleteOrganization(ctx.Request().Context(), orgID); err != nil {
			ctx.Logger().Errorf("DeleteOrganization: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	meta := metaFromContext(ctx)
	body, err := client.DeleteOrganization(ctx.Request().Context(), meta, orgID)
	if err != nil {
		ctx.Logger().Errorf("DeleteOrganization: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
