package echo

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/middleware"
	"github.com/uptrace/bun"
)

func GetUsers(ctx echo.Context) error {
	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		users, err := db.Instance().GetUsers(ctx.Request().Context(), userID, db.ACCESS_NONE)
		if err != nil {
			ctx.Logger().Errorf("GetUsers: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, users, len(users))
	}

	body, err := client.GetUsers(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetUsers: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUser(ctx echo.Context) error {
	targetUserID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		user, err := db.Instance().GetUser(ctx.Request().Context(), targetUserID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				ctx.Logger().Infof("GetUser: not found %s", targetUserID)
				return respondError(ctx, http.StatusNotFound, "Not found")
			}
			ctx.Logger().Errorf("GetUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, user, 1)
	}

	body, err := client.GetUser(ctx.Request().Context(), targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ctx.Logger().Infof("GetUser: not found %s", targetUserID)
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Errorf("GetUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserAudit(ctx echo.Context) error {
	targetUserID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	rctx := ctx.Request().Context()
	actorID := ctx.Get(middleware.UserIDKey).(uuid.UUID)

	if db.GetDebugMode() != db.DebugModeNone {
		ac, err := db.Instance().MutationAccess(rctx, actorID, targetUserID, db.OBJECT_TYPE_USER, db.ACCESS_OWNER)
		if err != nil && !errors.Is(err, db.ErrNoAccess) {
			ctx.Logger().Errorf("GetUserAudit MutationAccess user: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		if ac == nil {
			orgIDs := make([]uuid.UUID, 0)
			err = db.Instance().NewSelect().
				Table("claimius.user_claim").
				ColumnExpr("DISTINCT sa_owner_id").
				Where("user_id = ?", targetUserID).
				Where("app_id = claimius.get_disciple_app_id()").
				Where("sa_deleted_at IS NULL").
				Scan(rctx, &orgIDs)
			if err != nil {
				ctx.Logger().Errorf("GetUserAudit user_claim scan: %v", err)
				return respondError(ctx, http.StatusInternalServerError, "Internal server error")
			}
			for _, orgID := range orgIDs {
				ac, err = db.Instance().MutationAccess(rctx, actorID, orgID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_OWNER)
				if err != nil && !errors.Is(err, db.ErrNoAccess) {
					ctx.Logger().Errorf("GetUserAudit MutationAccess organization: %v", err)
					return respondError(ctx, http.StatusInternalServerError, "Internal server error")
				}
				if ac != nil {
					break
				}
			}
			if ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}

		audits, err := db.Instance().GetUserAudit(rctx, targetUserID, db.ACCESS_READ)
		if err != nil {
			ctx.Logger().Errorf("GetUserAudit: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, audits, len(audits))
	}

	body, err := client.GetUserAudit(rctx, targetUserID)
	if err != nil {
		ctx.Logger().Errorf("GetUserAudit: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserClaims(ctx echo.Context) error {
	targetUserID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		claims, err := db.Instance().GetUserClaims(ctx.Request().Context(), targetUserID)
		if err != nil {
			ctx.Logger().Errorf("GetUserClaims: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, claims, len(claims))
	}

	body, err := client.GetUserClaims(ctx.Request().Context(), targetUserID)
	if err != nil {
		ctx.Logger().Errorf("GetUserClaims: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetUserRelations(ctx echo.Context) error {
	targetUserID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	params := ctx.QueryParams()

	orgIDs := make([]uuid.UUID, 0)
	for _, raw := range params["org_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, perr := uuid.Parse(s)
			if perr != nil {
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
			id, perr := uuid.Parse(s)
			if perr != nil {
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
			id, perr := uuid.Parse(s)
			if perr != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			usrIDs = append(usrIDs, id)
		}
	}

	if db.GetDebugMode() != db.DebugModeNone {
		relations := make([]db.UserRelation, 0)
		q := db.Instance().NewSelect().
			Model(&relations).
			ExcludeColumn("sa_deleted_at").
			ExcludeColumn("app_id").
			Where("user_relation.user_id = ?", targetUserID).
			Where("user_relation.sa_deleted_at IS NULL")

		if len(orgIDs) > 0 {
			q.Where("user_relation.object_type = ? AND user_relation.object_id IN (?)", db.OBJECT_TYPE_ORGANIZATION, bun.In(orgIDs))
		}
		if len(locIDs) > 0 {
			q.Where("user_relation.object_type = ? AND user_relation.object_id IN (?)", db.OBJECT_TYPE_LOCATION, bun.In(locIDs))
		}
		if len(usrIDs) > 0 {
			q.Where("user_relation.user_id IN (?)", bun.In(usrIDs))
		}

		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetUserRelations: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, relations, len(relations))
	}

	body, err := client.GetUserConnections(ctx.Request().Context(), targetUserID)
	if err != nil {
		ctx.Logger().Errorf("GetUserRelations: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostUser(ctx echo.Context) error {
	var user db.SamnaUser
	if err := ctx.Bind(&user); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		rctx := ctx.Request().Context()
		actorID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ownerID, oerr := resolveTargetOwner(rctx, actorID, user.OwnerID)
		if oerr != nil {
			if errors.Is(oerr, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PostUser resolve owner: %v", oerr)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		out, err := db.Instance().PostUser(rctx, user)
		if err != nil {
			ctx.Logger().Errorf("PostUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		ac, accErr := db.Instance().MutationAccess(rctx, actorID, ownerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if accErr != nil {
			ctx.Logger().Warnf("PostUser actor access: %v", accErr)
		} else {
			claimName := "user - " + out.UserID.String()
			claim := db.Claim{
				Name:    claimName,
				Access:  int(db.ACCESS_ALL),
				OwnerID: ac.OwnerID,
				RootID:  ac.RootID,
				Type:    db.CLAIM_TYPE_VIRTUAL,
			}
			targetObjectType := db.ObjectType(db.OBJECT_TYPE_USER)
			targetObjectID := out.UserID
			claimObject := db.ClaimObject{
				ObjectID:   &targetObjectID,
				ObjectType: &targetObjectType,
				Inherits:   false,
			}
			userClaim := db.UserClaim{
				UserID: actorID,
			}
			ownerClaim, postErr := db.Instance().PostClaim(rctx, ac, &claim)
			if postErr != nil {
				ctx.Logger().Warnf("PostUser virtual claim: %v", postErr)
			} else {
				claimObject.ClaimID = ownerClaim.ID
				userClaim.ClaimID = ownerClaim.ID
				if _, err := db.Instance().PostClaimObject(rctx, ac, ownerClaim.ID, &claimObject); err != nil {
					ctx.Logger().Warnf("PostUser claim_object: %v", err)
				}
				if _, err := db.Instance().PostClaimUser(rctx, ac, ownerClaim.ID, &userClaim); err != nil {
					ctx.Logger().Warnf("PostUser user_claim: %v", err)
				}
			}
		}
		return respondJSON(ctx, http.StatusCreated, out, 1)
	}

	result, err := client.PostUser(ctx.Request().Context(), user)
	if err != nil {
		ctx.Logger().Errorf("PostUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 1)
}

func PutUser(ctx echo.Context) error {
	targetUserID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		user := &db.SamnaUser{}
		if err := ctx.Bind(user); err != nil {
			return respondError(ctx, http.StatusBadRequest, "Bad request")
		}
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, targetUserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("PutUser MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		user, err = db.Instance().PutUser(ctx.Request().Context(), ac, targetUserID, user)
		if err != nil {
			ctx.Logger().Errorf("PutUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, user, 1)
	}

	var user db.SamnaUser
	if err := ctx.Bind(&user); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PutUser(ctx.Request().Context(), targetUserID, user)
	if err != nil {
		ctx.Logger().Errorf("PutUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func DeleteUser(ctx echo.Context) error {
	targetUserID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		userID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
		ac, err := db.Instance().MutationAccess(ctx.Request().Context(), userID, targetUserID, db.OBJECT_TYPE_USER, db.ACCESS_WRITE)
		if err != nil {
			if errors.Is(err, db.ErrNoAccess) {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
			ctx.Logger().Errorf("DeleteUser MutationAccess: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		user, err := db.Instance().DeleteUser(ctx.Request().Context(), ac, targetUserID)
		if err != nil {
			ctx.Logger().Errorf("DeleteUser: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, user, 1)
	}

	result, err := client.DeleteUser(ctx.Request().Context(), targetUserID)
	if err != nil {
		ctx.Logger().Errorf("DeleteUser: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}


func resolveTargetOwner(ctx context.Context, actorID uuid.UUID, requested *uuid.UUID) (uuid.UUID, error) {
	if requested != nil && *requested != uuid.Nil {
		ac, err := db.Instance().MutationAccess(ctx, actorID, *requested, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			return uuid.Nil, err
		}
		if ac == nil {
			return uuid.Nil, db.ErrNoAccess
		}
		return *requested, nil
	}
	if sc, err := db.Instance().GetClient(ctx, actorID); err == nil {
		return sc.OwnerID, nil
	}
	orgs, err := db.Instance().GetOrganizations(ctx, actorID, db.ACCESS_WRITE)
	if err != nil {
		return uuid.Nil, err
	}
	if len(orgs) == 0 {
		return uuid.Nil, db.ErrNoAccess
	}
	return orgs[0].ID, nil
}

func PostUserToken(ctx echo.Context) error {
	var payload db.SamnaUserToken
	if err := ctx.Bind(&payload); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	actorID := ctx.Get(middleware.UserIDKey).(uuid.UUID)
	ownerID, oerr := resolveTargetOwner(ctx.Request().Context(), actorID, payload.OwnerID)
	if oerr != nil {
		if errors.Is(oerr, db.ErrNoAccess) {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		ctx.Logger().Errorf("PostUserToken resolve owner: %v", oerr)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	payload.OwnerID = &ownerID
	result, err := client.PostUserToken(ctx.Request().Context(), payload)
	if err != nil {
		ctx.Logger().Errorf("PostUserToken: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusCreated, result, 1)
}

func PostUserTokenValidate(ctx echo.Context) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := ctx.Bind(&body); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, _, err := client.PostUserTokenValidate(ctx.Request().Context(), body.Token)
	if err != nil {
		ctx.Logger().Errorf("PostUserTokenValidate: %v", err)
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	return respondJSON(ctx, http.StatusOK, result, 1)
}

func GetUserAssets(ctx echo.Context) error {
	targetUserID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if db.GetDebugMode() != db.DebugModeNone {
		assets, err := db.Instance().GetUserAssets(ctx.Request().Context(), targetUserID)
		if err != nil {
			ctx.Logger().Errorf("GetUserAssets: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, assets, len(assets))
	}

	body, err := client.GetUserAssets(ctx.Request().Context(), targetUserID)
	if err != nil {
		ctx.Logger().Errorf("GetUserAssets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
