package echo

import (
	"crypto/rsa"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/pkg/azure"
	"github.com/nimling/samna-auth-middleware/v2/pkg/encryption"
	"github.com/nimling/samna-auth-middleware/v2/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	objectAssetColumns  = `id, object_type, object_id, asset_id, kind, name, description, mime_type, size, "index", sa_owner_id, sa_created_by, sa_deleted_at, sa_created_at, sa_updated_at`
	objectAssetTokenTTL = time.Minute
)

type objectAssetHome struct {
	AppID   uuid.UUID `bun:"app_id"`
	AssetID uuid.UUID `bun:"asset_id"`
}

type objectAssetToken struct {
	jwt.RegisteredClaims
	ClientID string `json:"client_id"`
}

func PostObjectAsset(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	link := &shared.ObjectAsset{}
	if err := link.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := link.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	actor, err := getObjectAssetActor(ctx, link.CreatedBy)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	owner, userClaimID, err := getObjectAssetGrant(ctx, appID, actor, string(link.ObjectType), link.ObjectID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	link.OwnerID = owner
	link.CreatedBy = userClaimID
	link.DeletedAt = nil
	if _, err := sdb.Instance().DB.NewInsert().
		Model(link).
		Value("app_id", "?", appID).
		Returning(objectAssetColumns).
		Exec(ctx.Request().Context()); err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" {
			return respond.Error(ctx, http.StatusConflict, "the asset already has a live link")
		}
		ctx.Logger().Errorf("PostObjectAsset: %v", err)
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusCreated, link, 1)
}

func PutObjectAsset(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	linkID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	patch := &shared.ObjectAsset{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	link, err := getObjectAssetWritable(ctx, appID, linkID, patch.CreatedBy)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	columns := make([]string, 0, 6)
	if patch.HasKey("kind") {
		link.Kind = patch.Kind
		columns = append(columns, "kind")
	}
	if patch.HasKey("name") {
		link.Name = patch.Name
		columns = append(columns, "name")
	}
	if patch.HasKey("description") {
		link.Description = patch.Description
		columns = append(columns, "description")
	}
	if patch.HasKey("mime_type") {
		link.MimeType = patch.MimeType
		columns = append(columns, "mime_type")
	}
	if patch.HasKey("size") {
		link.Size = patch.Size
		columns = append(columns, "size")
	}
	if patch.HasKey("index") {
		link.Index = patch.Index
		columns = append(columns, "index")
	}
	if len(columns) == 0 {
		return respond.JSON(ctx, http.StatusOK, link, 1)
	}
	if _, err := sdb.Instance().DB.NewUpdate().
		Model(link).
		Column(columns...).
		Where("id = ?", linkID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning(objectAssetColumns).
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("PutObjectAsset: %v", err)
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, link, 1)
}

func DeleteObjectAsset(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	linkID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	link, err := getObjectAssetWritable(ctx, appID, linkID, nil)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	if _, err := sdb.Instance().DB.NewUpdate().
		Model(link).
		Set("sa_deleted_at = now()").
		Where("id = ?", linkID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning(objectAssetColumns).
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("DeleteObjectAsset: %v", err)
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, link, 1)
}

func GetObjectAssetData(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	linkID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	reqCtx := ctx.Request().Context()
	var home objectAssetHome
	if err := sdb.Instance().NewRaw(`
		SELECT l.app_id, l.asset_id
		FROM claimius.object_asset l
		WHERE l.id = ?
		  AND l.sa_deleted_at IS NULL
		  AND ? IN (SELECT claimius.apps_for_object(l.object_type, l.object_id, l.app_id))`, linkID, appID).
		Scan(reqCtx, &home); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "")
		}
		ctx.Logger().Errorf("GetObjectAssetData: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	var held bool
	if err := sdb.Instance().NewRaw(`SELECT to_regclass('public.asset') IS NOT NULL`).Scan(reqCtx, &held); err != nil {
		ctx.Logger().Errorf("GetObjectAssetData asset table: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}
	if held {
		if err := sdb.Instance().NewRaw(`SELECT EXISTS (SELECT 1 FROM public.asset WHERE id = ? AND app_id = ? AND sa_deleted_at IS NULL)`, home.AssetID, home.AppID).Scan(reqCtx, &held); err != nil {
			ctx.Logger().Errorf("GetObjectAssetData held: %v", err)
			return respond.Error(ctx, http.StatusInternalServerError, "")
		}
	}
	if held {
		return getProphetAssetData(ctx, home)
	}

	var system struct {
		ID   uuid.UUID `bun:"id"`
		Slug string    `bun:"slug"`
	}
	if err := sdb.Instance().NewRaw("SELECT id, slug FROM claimius.get_app()").Scan(reqCtx, &system); err != nil {
		ctx.Logger().Errorf("GetObjectAssetData system app: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	var app SamnaApp
	if err := sdb.Instance().NewSelect().Model(&app).
		Where("id = ?", home.AppID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(reqCtx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "")
		}
		ctx.Logger().Errorf("GetObjectAssetData home app: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}
	base := getObjectAssetAuthBase(app.RedirectURIs)
	if base == "" {
		return respond.Error(ctx, http.StatusBadGateway, "the home app has no callback to reach it by")
	}
	key, err := encryption.ImportMasterKeyPair(app.PrivateKey, app.PrivateSeed)
	if err != nil {
		ctx.Logger().Errorf("GetObjectAssetData home key: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}
	token, err := getObjectAssetToken(key.PrivateKey, app.ID, utils.Cfg().Endpoint, app.Slug, system.ID, system.Slug, time.Now())
	if err != nil {
		ctx.Logger().Errorf("GetObjectAssetData sign: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}
	return getRelayedAssetData(ctx, base+"/asset/"+home.AssetID.String()+"/data", token)
}

func getObjectAssetActor(ctx echo.Context, createdBy *uuid.UUID) (uuid.UUID, error) {
	if raw := ctx.Request().Header.Get(headerForwardedUser); raw != "" {
		actor, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, errorsx.ErrBadRequest
		}
		return actor, nil
	}
	if createdBy == nil || *createdBy == uuid.Nil {
		return uuid.Nil, errorsx.ErrForbidden
	}
	var actor uuid.UUID
	if err := sdb.Instance().NewRaw(`
		SELECT user_id FROM claimius.user_claim
		WHERE id = ? AND sa_deleted_at IS NULL`, *createdBy).
		Scan(ctx.Request().Context(), &actor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, errorsx.ErrForbidden
		}
		return uuid.Nil, errorsx.ErrDbUnexpected
	}
	return actor, nil
}

func getObjectAssetGrant(ctx echo.Context, appID, actor uuid.UUID, objectType string, objectID uuid.UUID) (uuid.UUID, *uuid.UUID, error) {
	reqCtx := ctx.Request().Context()
	if objectType == string(OBJECT_TYPE_USER) && objectID == actor {
		grants := make([]uuid.UUID, 0, 1)
		if err := sdb.Instance().NewRaw(`
			SELECT uc.id
			FROM claimius.user_claim uc
			JOIN claimius.samna_app a ON a.id = ? AND a.claim_id = uc.claim_id
			WHERE uc.user_id = ?
			  AND uc.sa_deleted_at IS NULL
			  AND claimius.check_user_active(uc.user_id)
			ORDER BY uc.sa_created_at
			LIMIT 1`, appID, actor).Scan(reqCtx, &grants); err != nil {
			ctx.Logger().Errorf("object asset own grant: %v", err)
			return uuid.Nil, nil, errorsx.ErrDbUnexpected
		}
		if len(grants) == 0 {
			return uuid.Nil, nil, errorsx.ErrForbidden
		}
		return actor, &grants[0], nil
	}
	if objectType == string(OBJECT_TYPE_USER) {
		users := make([]shared.AccessUser, 0, 1)
		if err := sdb.Instance().NewRaw("SELECT * FROM claimius.get_user_access(?, ?, ?)",
			actor, objectID.String(), int(ACCESS_WRITE)).Scan(reqCtx, &users); err != nil {
			ctx.Logger().Errorf("object asset user grant: %v", err)
			return uuid.Nil, nil, errorsx.ErrDbUnexpected
		}
		if len(users) == 0 || users[0].UserClaimID == nil || users[0].OwnerID == nil {
			return uuid.Nil, nil, errorsx.ErrForbidden
		}
		return *users[0].OwnerID, users[0].UserClaimID, nil
	}
	grants := make([]shared.AccessClaim, 0, 1)
	if err := sdb.Instance().NewRaw("SELECT * FROM claimius.get_access(?, ?, ?, ?)",
		actor, objectID, objectType, int(ACCESS_WRITE)).Scan(reqCtx, &grants); err != nil {
		ctx.Logger().Errorf("object asset grant: %v", err)
		return uuid.Nil, nil, errorsx.ErrDbUnexpected
	}
	if len(grants) == 0 || grants[0].UserClaimID == nil {
		return uuid.Nil, nil, errorsx.ErrForbidden
	}
	return grants[0].OwnerID, grants[0].UserClaimID, nil
}

func getObjectAssetWritable(ctx echo.Context, appID, linkID uuid.UUID, createdBy *uuid.UUID) (*shared.ObjectAsset, error) {
	link := &shared.ObjectAsset{}
	if err := sdb.Instance().DB.NewSelect().
		Model(link).
		Where("id = ?", linkID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		return nil, errorsx.ErrDbUnexpected
	}
	actor, err := getObjectAssetActor(ctx, createdBy)
	if err != nil {
		return nil, err
	}
	if _, _, err := getObjectAssetGrant(ctx, appID, actor, string(link.ObjectType), link.ObjectID); err != nil {
		return nil, err
	}
	return link, nil
}

func getObjectAssetAuthBase(redirectURIs []string) string {
	for _, uri := range redirectURIs {
		if base, ok := strings.CutSuffix(strings.TrimRight(uri, "/"), "/callback"); ok {
			return base
		}
	}
	return ""
}

func getObjectAssetToken(key *rsa.PrivateKey, homeID uuid.UUID, issuer, audience string, subject uuid.UUID, clientID string, now time.Time) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, objectAssetToken{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    issuer,
			Subject:   subject.String(),
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(objectAssetTokenTTL)),
		},
		ClientID: clientID,
	})
	token.Header["kid"] = homeID.String()
	return token.SignedString(key)
}

func getRelayedAssetData(ctx echo.Context, target, token string) error {
	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet, target, nil)
	if err != nil {
		return respond.Error(ctx, http.StatusBadGateway, "")
	}
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	for _, names := range [][]string{client.ConditionalHeaders, client.RangeHeaders} {
		for _, name := range names {
			if v := ctx.Request().Header.Get(name); v != "" {
				req.Header.Set(name, v)
			}
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ctx.Logger().Errorf("relay asset: %v", err)
		return respond.Error(ctx, http.StatusBadGateway, "")
	}
	defer resp.Body.Close()
	for _, name := range client.AssetHeaders {
		if v := resp.Header.Get(name); v != "" {
			ctx.Response().Header().Set(name, v)
		}
	}
	ctx.Response().WriteHeader(resp.StatusCode)
	if resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return nil
	}
	_, err = io.Copy(ctx.Response(), resp.Body)
	return err
}

func getProphetAssetData(ctx echo.Context, home objectAssetHome) error {
	reqCtx := ctx.Request().Context()
	var asset Asset
	if err := sdb.Instance().NewSelect().
		Model(&asset).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", home.AssetID).
		Where("app_id = ?", home.AppID).
		Where("sa_deleted_at IS NULL").
		Scan(reqCtx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}
	blobClient, err := azure.GetBlobClient(ctx, asset.AppID, asset.ID)
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}
	conditions := &blob.ModifiedAccessConditions{}
	if v := ctx.Request().Header.Get("If-None-Match"); v != "" {
		etag := azcore.ETag(v)
		conditions.IfNoneMatch = &etag
	} else if v := ctx.Request().Header.Get("If-Modified-Since"); v != "" {
		if since, err := http.ParseTime(v); err == nil {
			conditions.IfModifiedSince = &since
		}
	}
	access := &blob.AccessConditions{ModifiedAccessConditions: conditions}
	options := &blob.DownloadStreamOptions{AccessConditions: access}
	r := client.AssetRange{Status: http.StatusOK}
	if ctx.Request().Header.Get("Range") != "" {
		props, err := blobClient.GetProperties(reqCtx, &blob.GetPropertiesOptions{AccessConditions: access})
		if done, failure := putBlobNotModified(ctx, err); done {
			return failure
		}
		if err != nil {
			ctx.Logger().Error(err)
			return respond.Error(ctx, http.StatusInternalServerError, "")
		}
		size, etag, lastModified := int64(-1), "", ""
		if props.ContentLength != nil {
			size = *props.ContentLength
		}
		if props.ETag != nil {
			etag = string(*props.ETag)
		}
		if props.LastModified != nil {
			lastModified = props.LastModified.UTC().Format(http.TimeFormat)
		}
		r = client.GetAssetRange(ctx.Request().Header, size, etag, lastModified)
		if r.Status == http.StatusRequestedRangeNotSatisfiable {
			ctx.Response().Header().Set("Accept-Ranges", "bytes")
			ctx.Response().Header().Set("Content-Range", r.GetContentRange())
			return ctx.NoContent(r.Status)
		}
		if r.Status == http.StatusPartialContent {
			options.Range = blob.HTTPRange{Offset: r.Start, Count: r.Length}
		}
	}
	resp, err := blobClient.DownloadStream(reqCtx, options)
	if done, failure := putBlobNotModified(ctx, err); done {
		return failure
	}
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}
	defer resp.Body.Close()
	header := ctx.Response().Header()
	header.Set(echo.HeaderContentType, asset.MimeType)
	if resp.ContentType != nil && *resp.ContentType != "" {
		header.Set(echo.HeaderContentType, *resp.ContentType)
	}
	if resp.ContentLength != nil {
		header.Set(echo.HeaderContentLength, strconv.FormatInt(*resp.ContentLength, 10))
	}
	if resp.ETag != nil {
		header.Set("ETag", string(*resp.ETag))
	}
	if resp.LastModified != nil {
		header.Set(echo.HeaderLastModified, resp.LastModified.UTC().Format(http.TimeFormat))
	}
	if resp.CacheControl != nil {
		header.Set(echo.HeaderCacheControl, *resp.CacheControl)
	}
	header.Set("Accept-Ranges", "bytes")
	if r.Status == http.StatusPartialContent {
		header.Set("Content-Range", r.GetContentRange())
		header.Set(echo.HeaderContentLength, strconv.FormatInt(r.Length, 10))
	}
	return respond.Stream(ctx, r.Status, header.Get(echo.HeaderContentType), resp.Body)
}

func putBlobNotModified(ctx echo.Context, err error) (bool, error) {
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != http.StatusNotModified {
		return false, nil
	}
	for _, name := range client.AssetHeaders {
		if v := respErr.RawResponse.Header.Get(name); v != "" {
			ctx.Response().Header().Set(name, v)
		}
	}
	return true, ctx.NoContent(http.StatusNotModified)
}
