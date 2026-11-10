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
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func GetLocations(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	locations := make([]Location, 0)
	if err := sdb.Instance().NewSelect().
		Model(&locations).
		ExcludeColumn("sa_deleted_at").
		Where("claimius.app_has_object(?, 'claimius.location', ?TableAlias.id)", appID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetLocations: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, locations, len(locations))
}

func GetLocation(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	location, err := getLocation(ctx, locationID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, location, 1)
}

func GetLocationAssets(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var objectAssets []ObjectAsset
	if err := sdb.Instance().NewSelect().
		Model(&objectAssets).
		ExcludeColumn("sa_deleted_at").
		Relation("Asset").
		Where("?TableAlias.object_id = ?", locationID).
		Where("?TableAlias.object_type = ?", OBJECT_TYPE_LOCATION).
		Where("?TableAlias.app_id = ?", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetLocationAssets: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	app, _ := ctx.Get("app").(SamnaApp)
	assets := make([]*Asset, 0, len(objectAssets))
	for _, oa := range objectAssets {
		if oa.Asset != nil {
			oa.Asset.AssetURL = GetAssetURL(app, oa.Asset.ID)
			assets = append(assets, oa.Asset)
		}
	}
	return respond.JSON(ctx, http.StatusOK, assets, len(assets))
}

func GetLocationClaims(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	claims := make([]Claim, 0)
	if err := sdb.Instance().NewSelect().
		Model(&claims).
		ExcludeColumn("sa_deleted_at").
		Where("claim.sa_deleted_at IS NULL").
		Where(`claim.id IN (SELECT claim_id FROM claimius.claim_object WHERE object_id = ? AND object_type = ? AND sa_deleted_at IS NULL)`, locationID, OBJECT_TYPE_LOCATION).
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetLocationClaims: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, claims, len(claims))
}

func PostLocation(ctx echo.Context) error {
	loc := &Location{}
	if err := loc.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := loc.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	created, err := postLocation(ctx, *loc)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusCreated, created, 1)
}

func PutLocation(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	updated := &Location{}
	if err := updated.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := updated.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	updated.ID = locationID
	if err := putLocation(ctx, *updated, appID); err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	final, err := getLocation(ctx, locationID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusOK, final, 1)
}

func DeleteLocation(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	location, err := deleteLocation(ctx, locationID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, location, 1)
}

func GetLocationAudit(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	if _, err := utils.ValidateQueryParams(ctx, true, []string{"operation"}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	audits := make([]Audit, 0)
	if err := sdb.Instance().NewSelect().
		Model(&audits).
		Where("app_id = ?", appID).
		Where("object_id = ?", locationID).
		Where("object_type = ?", OBJECT_TYPE_LOCATION).
		OrderExpr("sa_created_at DESC").
		Limit(100).
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetLocationAudit: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, audits, len(audits))
}

func getLocation(ctx echo.Context, locationID, appID uuid.UUID) (*Location, error) {
	var location Location
	err := sdb.Instance().
		NewSelect().
		Model(&location).
		ExcludeColumn("sa_deleted_at").
		Where("?TableAlias.id = ?", locationID).
		Where("claimius.app_has_object(?, 'claimius.location', ?TableAlias.id)", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("getLocation: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	var objectAssets []ObjectAsset
	err = sdb.Instance().
		NewSelect().
		Model(&objectAssets).
		ExcludeColumn("sa_deleted_at").
		Relation("Asset").
		Where("?TableAlias.object_id = ?", locationID).
		Where("?TableAlias.object_type = ?", OBJECT_TYPE_LOCATION).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ctx.Logger().Errorf("getLocation assets: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	for _, oa := range objectAssets {
		if oa.Asset != nil {
			location.Assets = append(location.Assets, oa.Asset)
		}
	}
	return &location, nil
}

func postLocation(ctx echo.Context, location Location) (*Location, error) {
	var created Location
	if _, err := sdb.Instance().NewInsert().
		Model(&location).
		Returning("*").
		Exec(ctx.Request().Context(), &created); err != nil {
		ctx.Logger().Errorf("postLocation: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return &created, nil
}

func putLocation(ctx echo.Context, location Location, appID uuid.UUID) error {
	res, err := sdb.Instance().NewUpdate().
		Model(&location).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", location.ID).
		Where("claimius.app_has_object(?, 'claimius.location', id)", appID).
		OmitZero().
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("putLocation: %v", err)
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

func deleteLocation(ctx echo.Context, locationID, appID uuid.UUID) (*Location, error) {
	var location Location
	res, err := sdb.Instance().
		NewUpdate().
		Model(&location).
		Where("id = ?", locationID).
		Where("claimius.app_has_object(?, 'claimius.location', id)", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("deleteLocation: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return nil, errorsx.ErrNotFound
	}
	return &location, nil
}
