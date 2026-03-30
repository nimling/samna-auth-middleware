package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetClaimGraph(ctx echo.Context) error {
	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetClaimGraph: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetClaimGraph(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetClaimGraph: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetClaims(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.ClaimRelations())
		claims, err := db.Instance().GetClaims(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_GUEST, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetClaims: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, claims, nil)
	}

	body, err := client.GetClaims(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetClaims: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetClaim(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.ClaimRelations())
		claim, err := db.Instance().GetClaim(ctx.Request().Context(), *debugUserID, claimID, db.ACCESS_LEVEL_GUEST, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetClaim: %v", err)
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		return RespondJSON(ctx, http.StatusOK, claim, nil)
	}

	body, err := client.GetClaim(ctx.Request().Context(), claimID)
	if err != nil {
		ctx.Logger().Errorf("GetClaim: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetClaimAudit(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetClaimAudit: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetClaimAudit(ctx.Request().Context(), claimID)
	if err != nil {
		ctx.Logger().Errorf("GetClaimAudit: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetClaimSearch(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		ctx.Logger().Errorf("GetClaimSearch: not available in debug mode")
		return RespondError(ctx, http.StatusNotImplemented, "Not implemented")
	}

	body, err := client.GetClaimSearch(ctx.Request().Context(), claimID)
	if err != nil {
		ctx.Logger().Errorf("GetClaimSearch: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostClaim(ctx echo.Context) error {
	var claim db.Claim
	if err := ctx.Bind(&claim); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostClaim(ctx.Request().Context(), claim)
	if err != nil {
		ctx.Logger().Errorf("PostClaim: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusCreated, result, nil)
}

func PutClaim(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var claim db.Claim
	if err := ctx.Bind(&claim); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PutClaim(ctx.Request().Context(), claimID, claim)
	if err != nil {
		ctx.Logger().Errorf("PutClaim: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func DeleteClaim(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.DeleteClaim(ctx.Request().Context(), claimID)
	if err != nil {
		ctx.Logger().Errorf("DeleteClaim: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func PostClaimObject(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var co db.ClaimObject
	if err := ctx.Bind(&co); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostClaimObject(ctx.Request().Context(), claimID, co)
	if err != nil {
		ctx.Logger().Errorf("PostClaimObject: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusCreated, result, nil)
}

func DeleteClaimObject(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	objectID, err := uuid.Parse(ctx.Param("object_id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.DeleteClaimObject(ctx.Request().Context(), claimID, objectID)
	if err != nil {
		ctx.Logger().Errorf("DeleteClaimObject: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func PostClaimUser(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var uc db.UserClaim
	if err := ctx.Bind(&uc); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostUserClaim(ctx.Request().Context(), claimID, uc)
	if err != nil {
		ctx.Logger().Errorf("PostClaimUser: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusCreated, result, nil)
}

func DeleteClaimUser(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	userID, err := uuid.Parse(ctx.Param("user_id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.DeleteUserClaim(ctx.Request().Context(), claimID, userID)
	if err != nil {
		ctx.Logger().Errorf("DeleteClaimUser: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}
