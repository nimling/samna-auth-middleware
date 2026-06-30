package prophet

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	prophetdb "github.com/nimling/samna-auth-middleware/prophet/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetOrganizations(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	orgs := make([]Organization, 0)
	if err := prophetdb.Instance().NewSelect().
		Model(&orgs).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
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
	if err := prophetdb.Instance().NewRaw(`
		SELECT id AS object_id,
		       'claimius.organization' AS object_type,
		       name AS label,
		       CASE WHEN sa_owner_id = id THEN NULL ELSE sa_owner_id END AS parent_id,
		       jsonb_build_object('level', sa_level) AS data
		FROM claimius.organization
		WHERE app_id = ? AND sa_deleted_at IS NULL`, appID).
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
	org.AppID = appID
	created, err := postOrganization(ctx, *org)
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
	updated.AppID = appID
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
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, organization, 1)
}

func getOrganization(ctx echo.Context, orgID, appID uuid.UUID) (*Organization, error) {
	var organization Organization
	err := prophetdb.Instance().
		NewSelect().
		Model(&organization).
		ExcludeColumn("sa_deleted_at").
		Where("?TableAlias.id = ?", orgID).
		Where("?TableAlias.app_id = ?", appID).
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
	err = prophetdb.Instance().
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

func postOrganization(ctx echo.Context, org Organization) (*Organization, error) {
	if org.OwnerID == nil {
		orgType := "standard"
		if org.Type != nil && *org.Type != "" {
			orgType = *org.Type
		}
		var rootID uuid.UUID
		if err := prophetdb.Instance().NewRaw(
			"SELECT (claimius.create_root_organization(?, ?, ?, ?, ?) -> 'organization' ->> 'id')::uuid",
			org.AppID, org.Name, org.CreatedBy, org.Description, orgType,
		).Scan(ctx.Request().Context(), &rootID); err != nil {
			ctx.Logger().Errorf("postOrganization root: %v", err)
			return nil, errorsx.ErrDbUnexpected
		}
		var createdRoot Organization
		if err := prophetdb.Instance().NewSelect().
			Model(&createdRoot).
			Where("id = ?", rootID).
			Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("postOrganization root reload: %v", err)
			return nil, errorsx.ErrDbUnexpected
		}
		return &createdRoot, nil
	}

	var created Organization
	if _, err := prophetdb.Instance().NewInsert().
		Model(&org).
		Returning("*").
		Exec(ctx.Request().Context(), &created); err != nil {
		ctx.Logger().Errorf("postOrganization: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return &created, nil
}

func putOrganization(ctx echo.Context, org Organization, appID uuid.UUID) error {
	res, err := prophetdb.Instance().NewUpdate().
		Model(&org).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", org.ID).
		Where("app_id = ?", appID).
		OmitZero().
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
	res, err := prophetdb.Instance().
		NewUpdate().
		Model(&organization).
		Where("id = ?", organizationID).
		Where("app_id = ?", appID).
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
