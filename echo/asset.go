package echo

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
)

func GetAssets(ctx echo.Context) error {
	if db.GetDebugMode() != db.DebugModeNone {
		assets, err := db.Instance().GetAssets(ctx.Request().Context())
		if err != nil {
			ctx.Logger().Errorf("GetAssets: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, assets, len(assets))
	}

	body, err := client.GetAssets(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetAssets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetAsset(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		asset, err := db.Instance().GetAsset(ctx.Request().Context(), assetID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, asset, 1)
	}

	body, err := client.GetAsset(ctx.Request().Context(), assetID)
	if err != nil {
		ctx.Logger().Errorf("GetAsset: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetAssetData(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		asset, err := db.Instance().GetAsset(ctx.Request().Context(), assetID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		if asset.BlobURL == nil || *asset.BlobURL == "" {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet, *asset.BlobURL, nil)
		if err != nil {
			ctx.Logger().Errorf("GetAssetData build request: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			ctx.Logger().Errorf("GetAssetData fetch blob: %v", err)
			return respondError(ctx, http.StatusBadGateway, "Bad gateway")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return respondError(ctx, resp.StatusCode, "Bad gateway")
		}
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = asset.MimeType
		}
		return ctx.Stream(http.StatusOK, contentType, resp.Body)
	}

	body, err := client.GetAssetData(ctx.Request().Context(), assetID)
	if err != nil {
		ctx.Logger().Errorf("GetAssetData: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/octet-stream", body)
}

func PostAsset(ctx echo.Context) error {
	asset := &db.Asset{}
	if err := ctx.Bind(asset); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if err := asset.ValidatePost(); err != nil {
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, asset.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostAsset MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		asset, err = db.Instance().PostAsset(ctx.Request().Context(), ac, asset)
		if err != nil {
			ctx.Logger().Errorf("PostAsset: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, asset, 1)
	}

	result, err := client.PostAsset(ctx.Request().Context(), *asset)
	if err != nil {
		ctx.Logger().Errorf("PostAsset: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 1)
}

func PostAssetData(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		asset, err := db.Instance().GetAsset(ctx.Request().Context(), assetID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		if asset.BlobURL == nil || *asset.BlobURL == "" {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodPut, *asset.BlobURL, ctx.Request().Body)
		if err != nil {
			ctx.Logger().Errorf("PostAssetData build request: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		req.Header.Set("Content-Type", ctx.Request().Header.Get("Content-Type"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			ctx.Logger().Errorf("PostAssetData put blob: %v", err)
			return respondError(ctx, http.StatusBadGateway, "Bad gateway")
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			io.Copy(io.Discard, resp.Body)
			return respondError(ctx, resp.StatusCode, "Bad gateway")
		}
		return respondJSON(ctx, http.StatusOK, asset, 1)
	}

	body, err := client.PostAssetData(ctx.Request().Context(), assetID, ctx.Request().Header.Get("Content-Type"), ctx.Request().Body)
	if err != nil {
		ctx.Logger().Errorf("PostAssetData: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PutAsset(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	asset := &db.Asset{}
	if err := ctx.Bind(asset); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		existing, err := db.Instance().GetAsset(ctx.Request().Context(), assetID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, existing.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutAsset MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		asset, err = db.Instance().PutAsset(ctx.Request().Context(), ac, assetID, asset)
		if err != nil {
			ctx.Logger().Errorf("PutAsset: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, asset, 1)
	}

	result, err := client.PutAsset(ctx.Request().Context(), assetID, *asset)
	if err != nil {
		ctx.Logger().Errorf("PutAsset: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func DeleteAsset(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		existing, err := db.Instance().GetAsset(ctx.Request().Context(), assetID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, existing.UserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("DeleteAsset MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		asset, err := db.Instance().DeleteAsset(ctx.Request().Context(), ac, assetID)
		if err != nil {
			ctx.Logger().Errorf("DeleteAsset: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, asset, 1)
	}

	result, err := client.DeleteAsset(ctx.Request().Context(), assetID)
	if err != nil {
		ctx.Logger().Errorf("DeleteAsset: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}
