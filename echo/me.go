package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetMe(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.UserRelations())
		user, err := db.Instance().GetUserByID(ctx.Request().Context(), *debugUserID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetMe: %v", err)
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		return RespondJSON(ctx, http.StatusOK, user, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.GetMe(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetMe: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PutMe(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		var user db.SamnaUser
		if err := ctx.Bind(&user); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().UpdateUser(ctx.Request().Context(), *debugUserID, &user); err != nil {
			ctx.Logger().Errorf("PutMe: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, user, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.PutMe(ctx.Request().Context(), meta, nil)
	if err != nil {
		ctx.Logger().Errorf("PutMe: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeClaims(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().GetUserClaims(ctx.Request().Context(), *debugUserID)
		if err != nil {
			ctx.Logger().Errorf("GetMeClaims: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, claims, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.GetMeClaims(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetMeClaims: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostMeAsset(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("PostMeAsset: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.PostMeAsset(ctx.Request().Context(), meta, ctx.Request().Header.Get("Content-Type"), ctx.Request().Body)
	if err != nil {
		ctx.Logger().Errorf("PostMeAsset: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteMeAsset(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("DeleteMeAsset: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	meta := metaFromContext(ctx)
	err = client.DeleteMeAsset(ctx.Request().Context(), meta, assetID)
	if err != nil {
		ctx.Logger().Errorf("DeleteMeAsset: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return ctx.NoContent(http.StatusOK)
}

func GetMeConnections(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetMeConnections: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetMeConnections(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetMeConnections: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeModules(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetMeModules: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetMeModules(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetMeModules: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeOrganizations(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.OrganizationRelations())
		orgs, err := db.Instance().GetOrganizations(ctx.Request().Context(), relations...)
		if err != nil {
			ctx.Logger().Errorf("GetMeOrganizations: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, orgs, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.GetMeOrganizations(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetMeOrganizations: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeLocations(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		locs, err := db.Instance().GetLocations(ctx.Request().Context(), relations...)
		if err != nil {
			ctx.Logger().Errorf("GetMeLocations: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, locs, nil)
	}

	meta := metaFromContext(ctx)
	body, err := client.GetMeLocations(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetMeLocations: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeClients(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetMeClients: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetMeClients(ctx.Request().Context(), meta)
	if err != nil {
		ctx.Logger().Errorf("GetMeClients: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetMeClient(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetMeClient: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	clientID, err := uuid.Parse(ctx.Param("client_id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	meta := metaFromContext(ctx)
	body, err := client.GetMeClient(ctx.Request().Context(), meta, clientID)
	if err != nil {
		ctx.Logger().Errorf("GetMeClient: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
