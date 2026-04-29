package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
)

// GetClaimGraph returns the claim graph containing claims, user claims,
// users, organizations, and typed object references for the authenticated user.
func GetClaimGraph(ctx echo.Context) error {
	if debugUserID := getDebugUserID(); debugUserID != nil {
		nodes, err := db.Instance().GetClaimGraph(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_GUEST)
		if err != nil {
			ctx.Logger().Errorf("GetClaimGraph: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, nodes, 0)
	}

	body, err := client.GetClaimGraph(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetClaimGraph: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetClaims(ctx echo.Context) error {
	if debugUserID := getDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.ClaimRelations())
		claims, err := db.Instance().GetClaims(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_GUEST, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetClaims: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, claims, 0)
	}

	body, err := client.GetClaims(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetClaims: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetClaim(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.ClaimRelations())
		claim, err := db.Instance().GetClaim(ctx.Request().Context(), *debugUserID, claimID, db.ACCESS_LEVEL_GUEST, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetClaim: %v", err)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, claim, 0)
	}

	body, err := client.GetClaim(ctx.Request().Context(), claimID)
	if err != nil {
		ctx.Logger().Errorf("GetClaim: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetClaimAudit(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if getDebugUserID() != nil {
		results, err := db.Instance().GetClaimAudit(ctx.Request().Context(), claimID)
		if err != nil {
			ctx.Logger().Errorf("GetClaimAudit: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, results, 0)
	}

	body, err := client.GetClaimAudit(ctx.Request().Context(), claimID)
	if err != nil {
		ctx.Logger().Errorf("GetClaimAudit: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}


func PostClaim(ctx echo.Context) error {
	var claim db.Claim
	if err := ctx.Bind(&claim); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if claim.Type == db.CLAIM_TYPE_SYSTEM {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if claim.AccessLevel == int(db.ACCESS_LEVEL_SYSTEM) {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if claim.OwnerID == (uuid.UUID{}) {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claim.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_LEVEL_ADMIN)
		if err != nil {
			ctx.Logger().Errorf("PostClaim GetAccess: %v", err)
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, claim.OwnerID, db.OBJECT_TYPE_ORGANIZATION)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		claim.RootID = ac.RootID
		claim.CreatedBy = ac.UserClaimID
		out, err := db.Instance().PostClaim(ctx.Request().Context(), claim)
		if err != nil {
			ctx.Logger().Errorf("PostClaim: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, out, 0)
	}

	result, err := client.PostClaim(ctx.Request().Context(), claim)
	if err != nil {
		ctx.Logger().Errorf("PostClaim: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func PutClaim(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var claim db.Claim
	if err := ctx.Bind(&claim); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if claim.Type == db.CLAIM_TYPE_SYSTEM {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if claim.AccessLevel == int(db.ACCESS_LEVEL_SYSTEM) {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_LEVEL_ADMIN)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		out, err := db.Instance().PutClaim(ctx.Request().Context(), claimID, claim)
		if err != nil {
			ctx.Logger().Errorf("PutClaim: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, out, 0)
	}

	result, err := client.PutClaim(ctx.Request().Context(), claimID, claim)
	if err != nil {
		ctx.Logger().Errorf("PutClaim: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func DeleteClaim(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_LEVEL_ADMIN)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		if err := db.Instance().DeleteClaim(ctx.Request().Context(), claimID, *ac.UserClaimID); err != nil {
			ctx.Logger().Errorf("DeleteClaim: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteClaim(ctx.Request().Context(), claimID)
	if err != nil {
		ctx.Logger().Errorf("DeleteClaim: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func PostClaimObject(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var co db.ClaimObject
	if err := ctx.Bind(&co); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_LEVEL_ADMIN)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		co.OwnerID = ac.OwnerID
		co.RootID = ac.RootID
		co.CreatedBy = ac.UserClaimID
		out, err := db.Instance().PostClaimObject(ctx.Request().Context(), claimID, co)
		if err != nil {
			ctx.Logger().Errorf("PostClaimObject: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, out, 0)
	}

	result, err := client.PostClaimObject(ctx.Request().Context(), claimID, co)
	if err != nil {
		ctx.Logger().Errorf("PostClaimObject: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func DeleteClaimObject(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	objectID, err := uuid.Parse(ctx.Param("object_id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_LEVEL_ADMIN)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		if err := db.Instance().DeleteClaimObject(ctx.Request().Context(), claimID, objectID, *ac.UserClaimID); err != nil {
			ctx.Logger().Errorf("DeleteClaimObject: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteClaimObject(ctx.Request().Context(), claimID, objectID)
	if err != nil {
		ctx.Logger().Errorf("DeleteClaimObject: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}

func PostClaimUser(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var uc db.UserClaim
	if err := ctx.Bind(&uc); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_LEVEL_ADMIN)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		uc.OwnerID = ac.OwnerID
		uc.CreatedBy = *ac.UserClaimID
		out, err := db.Instance().PostClaimUser(ctx.Request().Context(), claimID, uc)
		if err != nil {
			ctx.Logger().Errorf("PostClaimUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, out, 0)
	}

	result, err := client.PostUserClaim(ctx.Request().Context(), claimID, uc)
	if err != nil {
		ctx.Logger().Errorf("PostClaimUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 0)
}

func DeleteClaimUser(ctx echo.Context) error {
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	userID, err := uuid.Parse(ctx.Param("user_id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_LEVEL_ADMIN)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		if err := db.Instance().DeleteClaimUser(ctx.Request().Context(), claimID, userID, *ac.UserClaimID); err != nil {
			ctx.Logger().Errorf("DeleteClaimUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteUserClaim(ctx.Request().Context(), claimID, userID)
	if err != nil {
		ctx.Logger().Errorf("DeleteClaimUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 0)
}
