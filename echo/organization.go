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
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, orgs, 0)
	}

	body, err := client.GetOrganizations(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetOrganizations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, &orgID, db.ACCESS_LEVEL_GUEST)
		if err != nil || claims == nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.OrganizationRelations())
		org, err := db.Instance().GetOrganization(ctx.Request().Context(), orgID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetOrganization: %v", err)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, org, 0)
	}

	body, err := client.GetOrganization(ctx.Request().Context(), orgID)
	if err != nil {
		ctx.Logger().Errorf("GetOrganization: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

// GetOrganizationGraph returns the ownership graph containing organizations
// and locations accessible to the authenticated user.
func GetOrganizationGraph(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		nodes, err := db.Instance().GetOwnerGraph(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_MEMBER)
		if err != nil {
			ctx.Logger().Errorf("GetOrganizationGraph: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, nodes, 0)
	}

	body, err := client.GetOrganizationGraph(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetOrganizationGraph: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganizationAudit(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		audits, err := db.Instance().GetOrganizationAudit(ctx.Request().Context(), orgID)
		if err != nil {
			ctx.Logger().Errorf("GetOrganizationAudit: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, audits, 0)
	}

	body, err := client.GetOrganizationAudit(ctx.Request().Context(), orgID)
	if err != nil {
		ctx.Logger().Errorf("GetOrganizationAudit: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganizationAssets(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.GetOrganizationAssets(ctx.Request().Context(), orgID)
	if err != nil {
		ctx.Logger().Errorf("GetOrganizationAssets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostOrganization(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		var org db.Organization
		if err := ctx.Bind(&org); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if org.OwnerID == nil {
			return respondError(ctx, http.StatusBadRequest, "owner_id is required")
		}
		claims, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, org.OwnerID, db.ACCESS_LEVEL_WRITE)
		if err != nil {
			ctx.Logger().Errorf("PostOrganization ClaimAccess: %v", err)
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if claims == nil {
			ctx.Logger().Infof("PostOrganization: no access for user %s on object %s", *debugUserID, *org.OwnerID)
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		org.CreatedBy = claims.ID
		org.RootID = &claims.RootID
		if err := db.Instance().PostOrganization(ctx.Request().Context(), &org); err != nil {
			ctx.Logger().Errorf("PostOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, org, 0)
	}

	var org db.Organization
	if err := ctx.Bind(&org); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostOrganization(ctx.Request().Context(), org)
	if err != nil {
		ctx.Logger().Errorf("PostOrganization: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func PutOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		existing, err := db.Instance().GetOrganization(ctx.Request().Context(), orgID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		accessCheckID := orgID
		if existing.OwnerID != nil {
			accessCheckID = *existing.OwnerID
		}
		claims, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, &accessCheckID, db.ACCESS_LEVEL_WRITE)
		if err != nil || claims == nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		var org db.Organization
		if err := ctx.Bind(&org); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().PutOrganization(ctx.Request().Context(), orgID, &org); err != nil {
			ctx.Logger().Errorf("PutOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, org, 0)
	}

	var org db.Organization
	if err := ctx.Bind(&org); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PutOrganization(ctx.Request().Context(), orgID, org)
	if err != nil {
		ctx.Logger().Errorf("PutOrganization: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func DeleteOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		existing, err := db.Instance().GetOrganization(ctx.Request().Context(), orgID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		accessCheckID := orgID
		if existing.OwnerID != nil {
			accessCheckID = *existing.OwnerID
		}
		claims, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, &accessCheckID, db.ACCESS_LEVEL_WRITE)
		if err != nil || claims == nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if err := db.Instance().DeleteOrganization(ctx.Request().Context(), orgID); err != nil {
			ctx.Logger().Errorf("DeleteOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteOrganization(ctx.Request().Context(), orgID)
	if err != nil {
		ctx.Logger().Errorf("DeleteOrganization: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}
