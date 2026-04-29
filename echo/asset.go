package echo

import (
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetAssets(ctx echo.Context) error {
	if getDebugUserID() != nil {
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

	if getDebugUserID() != nil {
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

	if getDebugUserID() != nil {
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
	var asset db.Asset
	if err := ctx.Bind(&asset); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		if err := db.Instance().PostAsset(ctx.Request().Context(), &asset); err != nil {
			ctx.Logger().Errorf("PostAsset: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, asset, 1)
	}

	result, err := client.PostAsset(ctx.Request().Context(), asset)
	if err != nil {
		ctx.Logger().Errorf("PostAsset: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func PostAssetData(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
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

	var asset db.Asset
	if err := ctx.Bind(&asset); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		if err := db.Instance().PutAsset(ctx.Request().Context(), assetID, &asset); err != nil {
			ctx.Logger().Errorf("PutAsset: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, asset, 1)
	}

	result, err := client.PutAsset(ctx.Request().Context(), assetID, asset)
	if err != nil {
		ctx.Logger().Errorf("PutAsset: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func DeleteAsset(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		if err := db.Instance().DeleteAsset(ctx.Request().Context(), assetID); err != nil {
			ctx.Logger().Errorf("DeleteAsset: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteAsset(ctx.Request().Context(), assetID)
	if err != nil {
		ctx.Logger().Errorf("DeleteAsset: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}
