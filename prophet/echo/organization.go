package echo

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func GetOrganizations(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	orgs := make([]Organization, 0)
	if err := sdb.Instance().NewSelect().
		Model(&orgs).
		ExcludeColumn("sa_deleted_at").
		Where("claimius.app_has_object(?, 'claimius.organization', ?TableAlias.id)", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetOrganizations: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, orgs, len(orgs))
}

func GetOrganization(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	org, err := getOrganization(ctx, orgID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, org, 1)
}

func GetOrganizationGraph(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	nodes := make([]*GraphNode, 0)
	if err := sdb.Instance().NewRaw(`
		SELECT id AS object_id,
		       'claimius.organization' AS object_type,
		       name AS label,
		       CASE WHEN sa_owner_id = id THEN NULL ELSE sa_owner_id END AS parent_id,
		       jsonb_build_object('level', sa_level) AS data
		FROM claimius.organization
		WHERE claimius.app_has_object(?, 'claimius.organization', id) AND sa_deleted_at IS NULL`, appID).
		Scan(ctx.Request().Context(), &nodes); err != nil {
		ctx.Logger().Errorf("GetOrganizationGraph: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, nodes, len(nodes))
}

func PostOrganization(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	org := &Organization{}
	if err := org.Bind(ctx); err != nil {
		ctx.Logger().Warnf("sam PostOrganization bind: %v", err)
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := org.ValidatePost(); err != nil {
		ctx.Logger().Warnf("sam PostOrganization validate: %v", err)
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	created, err := postOrganization(ctx, *org, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusCreated, created, 1)
}

func PutOrganization(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	organizationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	updated := &Organization{}
	if err := updated.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := updated.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	updated.ID = organizationID
	if err := putOrganization(ctx, *updated, appID); err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	final, err := getOrganization(ctx, organizationID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusOK, final, 1)
}

func DeleteOrganization(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	organizationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	organization, err := deleteOrganization(ctx, organizationID, appID)
	if err != nil {
		if errors.Is(err, shared.ErrOrganizationCustomer) {
			return respond.Error(ctx, http.StatusBadRequest, err.Error())
		}
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, organization, 1)
}

func getOrganization(ctx echo.Context, orgID, appID uuid.UUID) (*Organization, error) {
	var organization Organization
	err := sdb.Instance().
		NewSelect().
		Model(&organization).
		ExcludeColumn("sa_deleted_at").
		Where("?TableAlias.id = ?", orgID).
		Where("claimius.app_has_object(?, 'claimius.organization', ?TableAlias.id)", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("getOrganization: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	var objectAssets []ObjectAsset
	err = sdb.Instance().
		NewSelect().
		Model(&objectAssets).
		ExcludeColumn("sa_deleted_at").
		Relation("Asset").
		Where("?TableAlias.object_id = ?", orgID).
		Where("?TableAlias.object_type = ?", OBJECT_TYPE_ORGANIZATION).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ctx.Logger().Errorf("getOrganization assets: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	for _, oa := range objectAssets {
		if oa.Asset != nil {
			organization.Assets = append(organization.Assets, oa.Asset)
		}
	}
	return &organization, nil
}

func postOrganization(ctx echo.Context, org Organization, appID uuid.UUID) (*Organization, error) {
	if org.OwnerID == nil || *org.OwnerID == org.ID {
		var rootID uuid.UUID
		if err := sdb.Instance().NewRaw(
			"SELECT (claimius.upsert_root_organization(?, ?, ?, ?, ?, ?, ?, ?) -> 'organization' ->> 'id')::uuid",
			appID, org.Name, org.CreatedBy, org.Description, ORGANIZATION_TYPE_ENTITY, org.ID, org.ExternalID, org.Kind,
		).Scan(ctx.Request().Context(), &rootID); err != nil {
			ctx.Logger().Errorf("postOrganization root: %v", err)
			return nil, errorsx.ErrDbUnexpected
		}
		var createdRoot Organization
		if err := sdb.Instance().NewSelect().
			Model(&createdRoot).
			Where("id = ?", rootID).
			Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("postOrganization root reload: %v", err)
			return nil, errorsx.ErrDbUnexpected
		}
		return &createdRoot, nil
	}

	var created Organization
	if _, err := sdb.Instance().NewInsert().
		Model(&org).
		Returning("*").
		Exec(ctx.Request().Context(), &created); err != nil {
		ctx.Logger().Errorf("postOrganization: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return &created, nil
}

func putOrganization(ctx echo.Context, org Organization, appID uuid.UUID) error {
	columns := make([]string, 0, 6)
	for key, column := range map[string]string{
		"name": "name", "description": "description", "kind": "kind",
		"external_id": "external_id", "location_id": "sa_location_id", "level": "sa_level", "owner_id": "sa_owner_id",
	} {
		if org.HasKey(key) {
			columns = append(columns, column)
		}
	}
	if len(columns) == 0 {
		return errorsx.ErrBadRequest
	}
	if org.HasKey("owner_id") {
		var allowed bool
		if err := sdb.Instance().NewRaw(`
			SELECT o.sa_root_id = p.sa_root_id
			       AND NOT EXISTS (
			           SELECT 1 FROM claimius.get_owner_ancestors('claimius.organization', p.id) a WHERE a.object_id = o.id
			       )
			FROM claimius.organization o, claimius.organization p
			WHERE o.id = ? AND o.sa_deleted_at IS NULL
			  AND p.id = ? AND p.sa_deleted_at IS NULL
			  AND claimius.app_has_object(?, 'claimius.organization', p.id)`,
			org.ID, *org.OwnerID, appID).Scan(ctx.Request().Context(), &allowed); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errorsx.ErrNotFound
			}
			ctx.Logger().Errorf("putOrganization parent: %v", err)
			return errorsx.ErrDbUnexpected
		}
		if !allowed {
			return errorsx.ErrBadRequest
		}
	}
	res, err := sdb.Instance().NewUpdate().
		Model(&org).
		Column(columns...).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", org.ID).
		Where("claimius.app_has_object(?, 'claimius.organization', id)", appID).
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("putOrganization: %v", err)
		return errorsx.ErrDbUnexpected
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return errorsx.ErrNotFound
	}
	return nil
}

func deleteOrganization(ctx echo.Context, organizationID, appID uuid.UUID) (*Organization, error) {
	var organization Organization
	var orgType OrganizationType
	if err := sdb.Instance().NewRaw(
		"SELECT type FROM claimius.organization WHERE id = ? AND sa_deleted_at IS NULL AND claimius.app_has_object(?, 'claimius.organization', id)",
		organizationID, appID,
	).Scan(ctx.Request().Context(), &orgType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("deleteOrganization type: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	if orgType == ORGANIZATION_TYPE_CUSTOMER {
		return nil, shared.ErrOrganizationCustomer
	}
	res, err := sdb.Instance().
		NewUpdate().
		Model(&organization).
		Where("id = ?", organizationID).
		Where("claimius.app_has_object(?, 'claimius.organization', id)", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("deleteOrganization: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return nil, errorsx.ErrNotFound
	}
	return &organization, nil
}
