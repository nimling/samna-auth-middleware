package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetOrganizationAssets(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var objectAssets []ObjectAsset
	if err := sdb.Instance().NewSelect().
		Model(&objectAssets).
		ExcludeColumn("sa_deleted_at").
		Relation("Asset").
		Where("?TableAlias.object_id = ?", orgID).
		Where("?TableAlias.object_type = ?", OBJECT_TYPE_ORGANIZATION).
		Where("?TableAlias.app_id = ?", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetOrganizationAssets: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	app, _ := ctx.Get("app").(SamnaApp)
	assets := make([]*Asset, 0, len(objectAssets))
	for _, oa := range objectAssets {
		if oa.Asset != nil {
			oa.Asset.AssetURL = app.RedirectURI + "/api/asset/" + oa.Asset.ID.String() + "/data"
			assets = append(assets, oa.Asset)
		}
	}
	return respond.JSON(ctx, http.StatusOK, assets, len(assets))
}

func GetOrganizationClaims(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	claims := make([]Claim, 0)
	if err := sdb.Instance().NewSelect().
		Model(&claims).
		ExcludeColumn("sa_deleted_at").
		Where("claim.app_id = ?", appID).
		Where("claim.sa_deleted_at IS NULL").
		Where(`claim.id IN (SELECT claim_id FROM claimius.claim_object WHERE object_id = ? AND object_type = ? AND sa_deleted_at IS NULL)`, orgID, OBJECT_TYPE_ORGANIZATION).
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetOrganizationClaims: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, claims, len(claims))
}

func GetOrganizationAudit(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	orgID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	if _, err := utils.ValidateQueryParams(ctx, true, []string{"operation"}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	audits := make([]Audit, 0)
	if err := sdb.Instance().NewSelect().
		Model(&audits).
		Where("app_id = ?", appID).
		Where("object_id = ?", orgID).
		Where("object_type = ?", OBJECT_TYPE_ORGANIZATION).
		OrderExpr("sa_created_at DESC").
		Limit(100).
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetOrganizationAudit: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, audits, len(audits))
}
