package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetAssets(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetAssets: not available in debug mode")
		return respondError(ctx, http.StatusNotImplemented, "Not implemented")
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

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetAsset: not available in debug mode")
		return respondError(ctx, http.StatusNotImplemented, "Not implemented")
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

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetAssetData: not available in debug mode")
		return respondError(ctx, http.StatusNotImplemented, "Not implemented")
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
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PostAsset: not available in debug mode")
		return respondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	var asset db.Asset
	if err := ctx.Bind(&asset); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
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

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PostAssetData: not available in debug mode")
		return respondError(ctx, http.StatusNotImplemented, "Not implemented")
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

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PutAsset: not available in debug mode")
		return respondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	var asset db.Asset
	if err := ctx.Bind(&asset); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
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

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("DeleteAsset: not available in debug mode")
		return respondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	result, err := client.DeleteAsset(ctx.Request().Context(), assetID)
	if err != nil {
		ctx.Logger().Errorf("DeleteAsset: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}
