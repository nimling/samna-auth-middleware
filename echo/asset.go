package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetAssets(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetAssets: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetAssets(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetAssets: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetAsset(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetAsset: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetAsset(ctx.Request().Context(), meta, assetID)
	if err != nil {
		ctx.Logger().Errorf("GetAsset: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetAssetData(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetAssetData: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetAssetData(ctx.Request().Context(), meta, assetID)
	if err != nil {
		ctx.Logger().Errorf("GetAssetData: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/octet-stream", body)
}

func PostAsset(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PostAsset: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.PostAsset(ctx.Request().Context(), meta, nil)
	if err != nil {
		ctx.Logger().Errorf("PostAsset: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func PostAssetUpload(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PostAssetUpload: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.PostAssetUpload(ctx.Request().Context(), meta, assetID, ctx.Request().Header.Get("Content-Type"), ctx.Request().Body)
	if err != nil {
		ctx.Logger().Errorf("PostAssetUpload: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PutAsset(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PutAsset: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.PutAsset(ctx.Request().Context(), meta, assetID, nil)
	if err != nil {
		ctx.Logger().Errorf("PutAsset: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteAsset(ctx echo.Context) error {
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("DeleteAsset: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.DeleteAsset(ctx.Request().Context(), meta, assetID)
	if err != nil {
		ctx.Logger().Errorf("DeleteAsset: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
