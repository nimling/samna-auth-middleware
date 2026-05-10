package echo

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/uptrace/bun"
)

// GetClaimGraph returns the claim graph containing claims, user claims,
// users, organizations, and typed object references for the authenticated user.
func GetClaimGraph(ctx echo.Context) error {
	startID, depth, level, err := parseGraphQuery(ctx, db.ACCESS_NONE)
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		nodes, err := db.Instance().GetClaimGraph(ctx.Request().Context(), *debugUserID, startID, depth, level)
		if err != nil {
			ctx.Logger().Errorf("GetClaimGraph: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, nodes, 0)
	}

	body, err := client.GetClaimGraph(ctx.Request().Context(), startID, depth, level)
	if err != nil {
		ctx.Logger().Errorf("GetClaimGraph: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func parseGraphQuery(ctx echo.Context, defaultAccess db.ClaimAccess) (*uuid.UUID, int, db.ClaimAccess, error) {
	var startID *uuid.UUID
	if raw := ctx.QueryParam("start_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return nil, 0, 0, err
		}
		startID = &parsed
	}

	depth := 0
	if raw := ctx.QueryParam("depth"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return nil, 0, 0, err
		}
		depth = parsed
	}

	requiredAccess := defaultAccess
	if raw := ctx.QueryParam("required_access"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return nil, 0, 0, err
		}
		requiredAccess = db.ClaimAccess(parsed)
	}

	return startID, depth, requiredAccess, nil
}

func GetClaims(ctx echo.Context) error {
	params := ctx.QueryParams()

	orgIDs := make([]uuid.UUID, 0)
	for _, raw := range params["org_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			orgIDs = append(orgIDs, id)
		}
	}

	locIDs := make([]uuid.UUID, 0)
	for _, raw := range params["loc_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			locIDs = append(locIDs, id)
		}
	}

	usrIDs := make([]uuid.UUID, 0)
	for _, raw := range params["usr_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			usrIDs = append(usrIDs, id)
		}
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.ClaimRelations())
		claims := make([]db.Claim, 0)
		q := db.Instance().NewSelect().
			Model(&claims).
			ExcludeColumn("app_id").
			Where(db.ClaimWhere(*debugUserID, db.ACCESS_NONE))
		q = db.ApplyRelations(q, relations)

		if len(orgIDs) > 0 {
			q.Where("claim.sa_owner_id IN (?)", bun.In(orgIDs))
		}
		if len(locIDs) > 0 {
			q.Where("claim.id IN (SELECT claim_id FROM claimius.claim_object WHERE sa_deleted_at IS NULL AND object_type = ? AND object_id IN (?))", db.OBJECT_TYPE_LOCATION, bun.In(locIDs))
		}
		if len(usrIDs) > 0 {
			q.Where("claim.id IN (SELECT claim_id FROM claimius.user_claim WHERE app_id = claimius.get_disciple_app_id() AND sa_deleted_at IS NULL AND user_id IN (?))", bun.In(usrIDs))
		}

		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetClaims: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, claims, 0)
	}

	body, err := client.GetClaims(ctx.Request().Context(), ctx.QueryString())
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
		claim, err := db.Instance().GetClaim(ctx.Request().Context(), *debugUserID, claimID, db.ACCESS_NONE, relations...)
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
	if claim.Access == 0 {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if claim.OwnerID == (uuid.UUID{}) {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claim.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_OWNER)
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
		if ac.ClaimID != nil {
			objectID := out.ID
			objectType := db.ObjectType(db.OBJECT_TYPE_CLAIM)
			_, coErr := db.Instance().PostClaimObject(ctx.Request().Context(), *ac.ClaimID, db.ClaimObject{
				ClaimID:    *ac.ClaimID,
				ObjectID:   &objectID,
				ObjectType: &objectType,
				OwnerID:    claim.OwnerID,
				RootID:     ac.RootID,
				CreatedBy:  ac.UserClaimID,
				Inherits:   true,
			})
			if coErr != nil {
				ctx.Logger().Warnf("PostClaim bind claim_object: %v", coErr)
			}
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
	if claim.Access == 0 {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
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
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
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
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
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
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
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
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
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
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
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
