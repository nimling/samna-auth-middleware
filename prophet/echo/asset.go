package echo

import (
	"database/sql"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/azure"
	"github.com/nimling/samna-auth-middleware/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
)

type BodyAsset struct {
	Name        *string     `json:"name"`
	Description *string     `json:"description"`
	MimeType    *string     `json:"mime_type"`
	OwnerID     *uuid.UUID  `json:"owner_id"`
	CreatedBy   *uuid.UUID  `json:"created_by"`
	ObjectID    *uuid.UUID  `json:"object_id"`
	ObjectType  *ObjectType `json:"object_type"`
}

type assetInput struct {
	Name        string
	Description *string
	MimeType    string
	OwnerID     uuid.UUID
	CreatedBy   uuid.UUID
	Type        string
}

func GetAssets(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	assets := make([]Asset, 0)
	amount, err := sdb.Instance().
		NewSelect().
		Model(&assets).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		ScanAndCount(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	app, _ := ctx.Get("app").(SamnaApp)
	for i := range assets {
		assets[i].AssetURL = app.RedirectURI + "/api/asset/" + assets[i].ID.String() + "/data"
	}

	return respond.JSON(ctx, http.StatusOK, assets, amount)
}

func GetAsset(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	var asset Asset
	err = sdb.Instance().
		NewSelect().
		Model(&asset).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", assetID).
		Where("app_id = ?", appID).
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	app, _ := ctx.Get("app").(SamnaApp)
	asset.AssetURL = app.RedirectURI + "/api/asset/" + asset.ID.String() + "/data"

	return respond.JSON(ctx, http.StatusOK, asset, 1)
}

func GetAssetData(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	var asset Asset
	err = sdb.Instance().
		NewSelect().
		Model(&asset).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", assetID).
		Where("app_id = ?", appID).
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	blobClient, err := azure.GetBlobClient(ctx, asset.ID.String())
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	resp, err := blobClient.DownloadStream(ctx.Request().Context(), nil)
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}
	defer resp.Body.Close()

	return respond.Stream(ctx, http.StatusOK, asset.MimeType, resp.Body)
}

func PostAsset(ctx echo.Context) error {
	formFile, _ := ctx.FormFile("file")

	var req BodyAsset
	if formFile == nil {
		if err := ctx.Bind(&req); err != nil {
			ctx.Logger().Error(err)
			return respond.Error(ctx, http.StatusBadRequest, "")
		}
		if req.ObjectType == nil || req.ObjectID == nil || req.Name == nil || req.MimeType == nil || req.OwnerID == nil || req.CreatedBy == nil {
			return respond.Error(ctx, http.StatusBadRequest, "")
		}
		input := assetInput{
			Name:        *req.Name,
			Description: req.Description,
			MimeType:    *req.MimeType,
			OwnerID:     *req.OwnerID,
			CreatedBy:   *req.CreatedBy,
			Type:        ctx.QueryParam("type"),
		}
		asset, err := assetPostObject(ctx, *req.ObjectType, *req.ObjectID, input, nil)
		if err != nil {
			return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
		}
		return respond.JSON(ctx, http.StatusCreated, asset, 1)
	}

	params, err := ctx.FormParams()
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	objectIDStr := params.Get("object_id")
	objectTypeStr := params.Get("object_type")
	if objectIDStr == "" || objectTypeStr == "" {
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	objectID, err := uuid.Parse(objectIDStr)
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	input, err := assetResolveInputFromForm(ctx, formFile)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}

	asset, err := assetPostObject(ctx, ObjectType(objectTypeStr), objectID, *input, formFile)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}

	return respond.JSON(ctx, http.StatusCreated, asset, 1)
}

func PostAssetUpload(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	var asset Asset
	err = sdb.Instance().
		NewSelect().
		Model(&asset).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", assetID).
		Where("app_id = ?", appID).
		Where("status != 'complete'").
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	formFile, err := ctx.FormFile("file")
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	file, err := formFile.Open()
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "")
	}
	defer file.Close()

	blobClient, err := azure.GetBlobClient(ctx, asset.ID.String())
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	res, err := blobClient.Upload(ctx.Request().Context(), file, nil)
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	if res.RequestID != nil {
		ctx.Logger().Info("Uploaded " + *res.RequestID)
	}

	asset.Status = "complete"
	_, err = sdb.Instance().
		NewUpdate().
		Model(&asset).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", asset.ID).
		Column("status").
		Returning("*").
		Exec(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	app, _ := ctx.Get("app").(SamnaApp)
	asset.AssetURL = app.RedirectURI + "/api/asset/" + asset.ID.String() + "/data"

	return respond.JSON(ctx, http.StatusOK, asset, 1)
}

func PutAsset(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	var req BodyAsset
	err = ctx.Bind(&req)
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	asset := Asset{}

	if req.Name != nil {
		asset.Name = *req.Name
	}

	if req.Description != nil {
		asset.Description = req.Description
	}

	_, err = sdb.Instance().
		NewUpdate().
		OmitZero().
		Model(&asset).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", assetID).
		Where("app_id = ?", appID).
		Returning("*").
		Exec(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	app, _ := ctx.Get("app").(SamnaApp)
	asset.AssetURL = app.RedirectURI + "/api/asset/" + asset.ID.String() + "/data"

	return respond.JSON(ctx, http.StatusOK, asset, 1)
}

func DeleteAsset(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	var asset Asset
	_, err = sdb.Instance().
		NewUpdate().
		OmitZero().
		Where("id = ?", assetID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Model(&asset).
		Exec(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	var objectAsset ObjectAsset
	_, err = sdb.Instance().
		NewUpdate().
		OmitZero().
		Where("asset_id = ?", assetID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Model(&objectAsset).
		Exec(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	app, _ := ctx.Get("app").(SamnaApp)
	asset.AssetURL = app.RedirectURI + "/api/asset/" + asset.ID.String() + "/data"

	return respond.JSON(ctx, http.StatusOK, asset, 1)
}

func assetObjectAssets(ctx echo.Context, id uuid.UUID, objectType ObjectType) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	var objectAssets []ObjectAsset
	err := sdb.Instance().NewSelect().
		Model(&objectAssets).
		ExcludeColumn("sa_deleted_at").
		Relation("Asset").
		Where("?TableAlias.object_id = ?", id).
		Where("?TableAlias.object_type = ?", objectType).
		Where("?TableAlias.app_id = ?", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
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

func assetPostObject(ctx echo.Context, objectType ObjectType, objectID uuid.UUID, input assetInput, formFile *multipart.FileHeader) (*Asset, error) {
	if (input.Type == "profile" || input.Type == "banner" || input.Type == "logo") && !strings.HasPrefix(input.MimeType, "image/") {
		return nil, errorsx.ErrBadRequest
	}

	appID := ctx.Get("app_id").(uuid.UUID)

	tx, err := sdb.Instance().BeginTx(ctx.Request().Context(), nil)
	if err != nil {
		ctx.Logger().Errorf("assetPostObject tx: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	asset := Asset{
		AppID:       appID,
		Name:        input.Name,
		Description: input.Description,
		MimeType:    input.MimeType,
		Status:      "pending",
		OwnerID:     input.OwnerID,
		CreatedBy:   input.CreatedBy,
	}
	if input.Type != "" {
		asset.Type = &input.Type
	}

	if _, err := tx.NewInsert().Model(&asset).Returning("*").Exec(ctx.Request().Context()); err != nil {
		_ = tx.Rollback()
		ctx.Logger().Errorf("assetPostObject insert asset: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	if formFile != nil {
		file, openErr := formFile.Open()
		if openErr != nil {
			_ = tx.Rollback()
			ctx.Logger().Errorf("assetPostObject file open: %v", openErr)
			return nil, errorsx.ErrBadRequest
		}
		defer file.Close()

		blobClient, blobErr := azure.GetBlobClient(ctx, asset.ID.String())
		if blobErr != nil {
			_ = tx.Rollback()
			ctx.Logger().Errorf("assetPostObject blob client: %v", blobErr)
			return nil, errorsx.ErrDbUnexpected
		}

		if _, uploadErr := blobClient.Upload(ctx.Request().Context(), file, nil); uploadErr != nil {
			_ = tx.Rollback()
			ctx.Logger().Errorf("assetPostObject upload: %v", uploadErr)
			return nil, errorsx.ErrDbUnexpected
		}

		asset.Status = "complete"
		if _, err := tx.NewUpdate().Model(&asset).
			Where("sa_deleted_at IS NULL").Where("id = ?", asset.ID).Column("status").Returning("*").Exec(ctx.Request().Context()); err != nil {
			_ = tx.Rollback()
			ctx.Logger().Errorf("assetPostObject update status: %v", err)
			return nil, errorsx.ErrDbUnexpected
		}
	}

	objectAsset := ObjectAsset{
		AppID:      appID,
		AssetID:    asset.ID,
		ObjectID:   objectID,
		ObjectType: objectType,
		OwnerID:    input.OwnerID,
		CreatedBy:  input.CreatedBy,
	}

	if _, err := tx.NewInsert().Model(&objectAsset).Returning("*").Exec(ctx.Request().Context()); err != nil {
		_ = tx.Rollback()
		ctx.Logger().Errorf("assetPostObject object_asset insert: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	if err := tx.Commit(); err != nil {
		ctx.Logger().Errorf("assetPostObject commit: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	app, _ := ctx.Get("app").(SamnaApp)
	asset.AssetURL = app.RedirectURI + "/api/asset/" + asset.ID.String() + "/data"

	return &asset, nil
}

func assetResolveInputFromForm(ctx echo.Context, formFile *multipart.FileHeader) (*assetInput, error) {
	params, err := ctx.FormParams()
	if err != nil {
		return nil, errorsx.ErrBadRequest
	}

	input := assetInput{
		Type: ctx.QueryParam("type"),
	}

	if params.Has("name") {
		input.Name = params.Get("name")
	} else if formFile != nil {
		name := formFile.Filename
		fileExt := filepath.Ext(name)
		input.Name = strings.TrimSuffix(name, fileExt)
	}
	if input.Name == "" {
		return nil, errorsx.ErrBadRequest
	}

	if params.Has("mime_type") {
		input.MimeType = params.Get("mime_type")
	} else if formFile != nil {
		buffer := make([]byte, 512)
		src, openErr := formFile.Open()
		if openErr != nil {
			return nil, errorsx.ErrBadRequest
		}
		defer src.Close()
		if _, readErr := src.Read(buffer); readErr != nil && readErr != io.EOF {
			return nil, errorsx.ErrBadRequest
		}
		if _, seekErr := src.Seek(0, 0); seekErr != nil {
			return nil, errorsx.ErrBadRequest
		}
		input.MimeType = http.DetectContentType(buffer)
	}
	if input.MimeType == "" {
		return nil, errorsx.ErrBadRequest
	}

	if !params.Has("owner_id") {
		return nil, errorsx.ErrBadRequest
	}
	ownerID, parseErr := uuid.Parse(params.Get("owner_id"))
	if parseErr != nil {
		return nil, errorsx.ErrBadRequest
	}
	input.OwnerID = ownerID

	if !params.Has("created_by") {
		return nil, errorsx.ErrBadRequest
	}
	createdBy, parseErr := uuid.Parse(params.Get("created_by"))
	if parseErr != nil {
		return nil, errorsx.ErrBadRequest
	}
	input.CreatedBy = createdBy

	if params.Has("description") {
		desc := params.Get("description")
		input.Description = &desc
	}

	return &input, nil
}
