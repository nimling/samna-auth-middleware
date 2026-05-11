package echo

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
	"github.com/uptrace/bun"
)

func GetOrganizations(ctx echo.Context) error {
	params := ctx.QueryParams()

	orgIDs := make([]uuid.UUID, 0)
	for _, raw := range params["org_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			orgIDs = append(orgIDs, id)
		}
	}

	locIDs := make([]uuid.UUID, 0)
	for _, raw := range params["loc_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			locIDs = append(locIDs, id)
		}
	}

	usrIDs := make([]uuid.UUID, 0)
	for _, raw := range params["usr_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			usrIDs = append(usrIDs, id)
		}
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		with, err := db.QueryWithObject(ctx)
		if err != nil {
			ctx.Logger().Warn(err)
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		orgs := make([]db.Organization, 0)
		q := db.Instance().GetOrganizationsQuery(&orgs, *debugUserID, db.ACCESS_NONE)
		for _, w := range with {
			switch w {
			case db.OBJECT_TYPE_ORGANIZATION:
				q = q.Relation(db.RelationParent())
			default:
				ctx.Logger().Warnf("unsupported with object: %s", w)
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
		}

		if len(orgIDs) > 0 {
			q.Where("organization.id IN (?)", bun.In(orgIDs))
		}
		if len(locIDs) > 0 {
			q.Where("organization.id IN (SELECT sa_owner_id FROM claimius.location WHERE sa_deleted_at IS NULL AND id IN (?))", bun.In(locIDs))
		}
		if len(usrIDs) > 0 {
			q.WhereGroup(" AND ", func(qq *bun.SelectQuery) *bun.SelectQuery {
				for _, uid := range usrIDs {
					qq = qq.WhereOr("organization.id IN (SELECT id FROM claimius.get_organizations(?, claimius.get_disciple_app_id(), ?))", uid, db.ACCESS_NONE)
				}
				return qq
			})
		}

		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetOrganizations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, orgs, 0)
	}

	body, err := client.GetOrganizations(ctx.Request().Context(), ctx.QueryString())
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

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, orgID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_NONE)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, orgID, db.OBJECT_TYPE_ORGANIZATION)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusNotFound, "Not found")
			}
		}
		with, err := db.QueryWithObject(ctx)
		if err != nil {
			ctx.Logger().Warn(err)
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		var org db.Organization
		q := db.Instance().GetOrganizationQuery(&org, orgID)
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

func GetOrganizationGraph(ctx echo.Context) error {
	startID, depth, level, err := parseGraphQuery(ctx, db.ACCESS_READ)
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		nodes, err := db.Instance().GetOwnerGraph(ctx.Request().Context(), *debugUserID, startID, depth, level)
		if err != nil {
			ctx.Logger().Errorf("GetOrganizationGraph: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, nodes, 0)
	}

	body, err := client.GetOrganizationGraph(ctx.Request().Context(), startID, depth, level)
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

	if getDebugUserID() != nil {
		actorID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		audits, err := db.Instance().GetOrganizationAudit(ctx.Request().Context(), actorID, orgID, db.ACCESS_READ)
		if err != nil {
			ctx.Logger().Errorf("GetOrganizationAudit: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, audits, len(audits))
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
	if debugUserID := getDebugUserID(); debugUserID != nil {
		var org db.Organization
		if err := ctx.Bind(&org); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if org.OwnerID == (uuid.UUID{}) {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if org.Type == db.ORGANIZATION_TYPE_SYSTEM {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), *debugUserID, org.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostOrganization MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		org.ID = uuid.UUID{}
		if err := db.Instance().PostOrganization(ctx.Request().Context(), ac, &org); err != nil {
			ctx.Logger().Errorf("PostOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, org, 0)
	}

	var org db.Organization
	if err := ctx.Bind(&org); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if org.Type == db.ORGANIZATION_TYPE_SYSTEM {
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

	if debugUserID := getDebugUserID(); debugUserID != nil {
		existing, err := db.Instance().GetOrganization(ctx.Request().Context(), orgID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), *debugUserID, existing.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutOrganization MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		var org db.Organization
		if err := ctx.Bind(&org); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if org.Type == db.ORGANIZATION_TYPE_SYSTEM {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().PutOrganization(ctx.Request().Context(), ac, orgID, &org); err != nil {
			ctx.Logger().Errorf("PutOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, org, 0)
	}

	var org db.Organization
	if err := ctx.Bind(&org); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if org.Type == db.ORGANIZATION_TYPE_SYSTEM {
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

	if debugUserID := getDebugUserID(); debugUserID != nil {
		existing, err := db.Instance().GetOrganization(ctx.Request().Context(), orgID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), *debugUserID, existing.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("DeleteOrganization MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		org, err := db.Instance().DeleteOrganization(ctx.Request().Context(), ac, orgID)
		if err != nil {
			ctx.Logger().Errorf("DeleteOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, org, 0)
	}

	result, err := client.DeleteOrganization(ctx.Request().Context(), orgID)
	if err != nil {
		ctx.Logger().Errorf("DeleteOrganization: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}
