package echo

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/audit"
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

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
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

		if len(orgIDs) > 0 {
			q.Where("organization.id IN (?)", bun.In(orgIDs))
		}
		if len(locIDs) > 0 {
			q.WhereGroup(" AND ", func(qq *bun.SelectQuery) *bun.SelectQuery {
				for _, locID := range locIDs {
					qq = qq.WhereOr("organization.id IN (SELECT a.object_id FROM claimius.get_location_ancestors(?, ?) a WHERE a.object_type = ?)", db.OBJECT_TYPE_LOCATION, locID, db.OBJECT_TYPE_ORGANIZATION)
				}
				return qq
			})
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
		return respondJSON(ctx, http.StatusOK, orgs, len(orgs))
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

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, orgID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_NONE)
		if err != nil || ac == nil {
			if err != nil && !errors.Is(err, db.ErrNoAccess) {
				ctx.Logger().Errorf("GetOrganization MutationAccess: %v", err)
				return respondError(ctx, http.StatusInternalServerError, "Internal server error")
			}
			return respondError(ctx, http.StatusForbidden, "Forbidden")
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
		return respondJSON(ctx, http.StatusOK, org, 1)
	}

	body, err := client.GetOrganization(ctx.Request().Context(), orgID)
	if err != nil {
		ctx.Logger().Errorf("GetOrganization: %v", err)
		return respondError(ctx, http.StatusForbidden, "Forbidden")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetOrganizationGraph(ctx echo.Context) error {
	startID, depth, level, err := parseGraphQuery(ctx, db.ACCESS_READ)
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		nodes, err := db.Instance().GetOwnerGraph(ctx.Request().Context(), userID, startID, depth, level)
		if err != nil {
			ctx.Logger().Errorf("GetOrganizationGraph: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, nodes, len(nodes))
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

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		audits, err := db.Instance().GetOrganizationAudit(ctx.Request().Context(), userID, orgID, db.ACCESS_READ)
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
	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		org := &db.Organization{}
		if err := ctx.Bind(org); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := org.ValidatePost(); err != nil {
			return respondError(ctx, http.StatusBadRequest, err.Error())
		}
		if org.OwnerID == uuid.Nil {
			ac, err := db.Instance().RootAccess(ctx.Request().Context(), userID, db.ACCESS_OWNER)
			if err != nil {
				if errors.Is(err, db.ErrNoAccess) {
					return respondError(ctx, http.StatusForbidden, "Forbidden")
				}
				ctx.Logger().Errorf("PostOrganization RootAccess: %v", err)
				return respondError(ctx, http.StatusInternalServerError, "Internal server error")
			}
			org, err = db.Instance().PostRootOrganization(ctx.Request().Context(), ac, org)
			if err != nil {
				ctx.Logger().Errorf("PostRootOrganization: %v", err)
				return respondError(ctx, http.StatusInternalServerError, "Internal server error")
			}
			audit.Write("PostOrganization", "create", db.OBJECT_TYPE_ORGANIZATION, org.ID, org.ID, ac.UserClaimID, "Created root organization "+org.Name)
			return respondJSON(ctx, http.StatusCreated, org, 1)
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, org.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostOrganization MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		org, err = db.Instance().PostOrganization(ctx.Request().Context(), ac, org)
		if err != nil {
			ctx.Logger().Errorf("PostOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		audit.Write("PostOrganization", "create", db.OBJECT_TYPE_ORGANIZATION, org.ID, org.OwnerID, ac.UserClaimID, "Created organization "+org.Name)
		return respondJSON(ctx, http.StatusCreated, org, 1)
	}

	var org db.Organization
	if err := ctx.Bind(&org); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := org.ValidatePost(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}
	result, err := client.PostOrganization(ctx.Request().Context(), org)
	if err != nil {
		ctx.Logger().Errorf("PostOrganization: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 1)
}

func PutOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	org := &db.Organization{}
	if err := org.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := org.ValidatePut(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		existing, err := db.Instance().GetOrganization(ctx.Request().Context(), orgID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, existing.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutOrganization MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		org, err = db.Instance().PutOrganization(ctx.Request().Context(), ac, orgID, org)
		if err != nil {
			ctx.Logger().Errorf("PutOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		audit.Write("PutOrganization", "update", db.OBJECT_TYPE_ORGANIZATION, org.ID, org.OwnerID, ac.UserClaimID, "Replaced organization "+org.Name)
		return respondJSON(ctx, http.StatusOK, org, 1)
	}

	result, err := client.PutOrganization(ctx.Request().Context(), orgID, *org)
	if err != nil {
		ctx.Logger().Errorf("PutOrganization: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func PatchOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	org := &db.Organization{}
	if err := org.Bind(ctx); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := org.ValidatePatch(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		existing, err := db.Instance().GetOrganization(ctx.Request().Context(), orgID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, existing.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PatchOrganization MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		org, err = db.Instance().PatchOrganization(ctx.Request().Context(), ac, orgID, org)
		if err != nil {
			ctx.Logger().Errorf("PatchOrganization: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		audit.Write("PatchOrganization", "update", db.OBJECT_TYPE_ORGANIZATION, org.ID, org.OwnerID, ac.UserClaimID, "Patched organization "+org.Name)
		return respondJSON(ctx, http.StatusOK, org, 1)
	}

	result, err := client.PatchOrganization(ctx.Request().Context(), orgID, *org)
	if err != nil {
		ctx.Logger().Errorf("PatchOrganization: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func DeleteOrganization(ctx echo.Context) error {
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, orgID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_OWNER)
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
		audit.Write("DeleteOrganization", "delete", db.OBJECT_TYPE_ORGANIZATION, org.ID, org.OwnerID, ac.UserClaimID, "Deleted organization "+org.Name)
		return respondJSON(ctx, http.StatusOK, org, 1)
	}

	result, err := client.DeleteOrganization(ctx.Request().Context(), orgID)
	if err != nil {
		ctx.Logger().Errorf("DeleteOrganization: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}
