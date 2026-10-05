package echo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun"
)

func GetClaims(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	claims := make([]Claim, 0)
	if err := sdb.Instance().NewSelect().
		Model(&claims).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetClaims: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, claims, len(claims))
}

func GetClaimGraph(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	nodes := make([]*GraphNode, 0)
	if err := sdb.Instance().NewRaw(`
		SELECT id AS object_id,
		       'claimius.claim' AS object_type,
		       name AS label,
		       CASE WHEN sa_root_id = id THEN NULL ELSE sa_root_id END AS parent_id,
		       jsonb_build_object('access', sa_access, 'inherits', inherits) AS data
		FROM claimius.claim
		WHERE app_id = ? AND sa_deleted_at IS NULL`, appID).
		Scan(ctx.Request().Context(), &nodes); err != nil {
		ctx.Logger().Errorf("GetClaimGraph: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, nodes, len(nodes))
}

func GetClaim(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	claim, err := getClaim(ctx, claimID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, claim, 1)
}

func PostClaim(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claim := &Claim{}
	if err := claim.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := claim.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	claim.AppID = appID
	if _, err := sdb.Instance().NewInsert().
		Model(claim).
		Returning("*").
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("PostClaim: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "claim insert failed")
	}
	return respond.JSON(ctx, http.StatusCreated, claim, 1)
}

func PutClaim(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	patch := &Claim{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	patch.ID = claimID
	patch.AppID = appID
	query := sdb.Instance().NewUpdate().
		Model(patch).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", claimID).
		Where("app_id = ?", appID).
		OmitZero().
		Returning("*")
	if patch.HasKey("sa_access") {
		query = query.Set("sa_access = ?", int(patch.Access))
	}
	if patch.HasKey("inherits") {
		query = query.Set("inherits = ?", patch.Inherits)
	}
	if patch.HasKey("level") {
		query = query.Set("level = ?", patch.Level)
	}
	res, err := query.Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("PutClaim: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "claim update failed")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if affected == 0 {
		return respond.Error(ctx, http.StatusNotFound, "claim not found")
	}
	return respond.JSON(ctx, http.StatusOK, patch, 1)
}

func DeleteClaim(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	var claim Claim
	res, err := sdb.Instance().NewUpdate().
		Model(&claim).
		Where("id = ?", claimID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("DeleteClaim: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if affected == 0 {
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	}
	return respond.JSON(ctx, http.StatusOK, claim, 1)
}

func PostClaimObject(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id must be a uuid")
	}
	claimObject := &ClaimObject{}
	if err := claimObject.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := claimObject.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	claimObject.AppID = appID
	claimObject.ClaimID = claimID
	if _, err := sdb.Instance().NewInsert().
		Model(claimObject).
		Returning("*").
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("PostClaimObject: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "claim_object insert failed")
	}
	return respond.JSON(ctx, http.StatusCreated, claimObject, 1)
}

func PatchClaimObject(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id must be a uuid")
	}
	objectID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	var claimObjectID uuid.UUID
	if err := sdb.Instance().NewSelect().
		TableExpr("claimius.claim_object").
		Column("id").
		Where("claim_id = ?", claimID).
		Where("object_id = ?", objectID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		OrderExpr("sa_created_at DESC").
		Limit(1).
		Scan(ctx.Request().Context(), &claimObjectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Errorf("PatchClaimObject lookup: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	patch := &ClaimObject{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	var scopeBytes []byte
	if patch.Scope != nil {
		scopeBytes = []byte(*patch.Scope)
	}
	var inherits *bool
	if patch.HasKey("inherits") {
		v := patch.Inherits
		inherits = &v
	}
	if _, err := sdb.Instance().NewRaw(
		"SELECT claimius.update_claim_object(?, ?, ?, ?, ?::jsonb)",
		claimObjectID, patch.Reason, inherits, patch.Access, scopeBytes,
	).Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("PatchClaimObject: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "claim_object update failed")
	}
	var updated ClaimObject
	if err := sdb.Instance().NewSelect().
		Model(&updated).
		Where("id = ?", claimObjectID).
		Where("app_id = ?", appID).
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("PatchClaimObject reselect: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "claim_object reselect failed")
	}
	return respond.JSON(ctx, http.StatusOK, updated, 1)
}

func DeleteClaimObject(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	objectID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	var claimObject ClaimObject
	res, err := sdb.Instance().NewUpdate().
		Model(&claimObject).
		Where("claim_id = ?", claimID).
		Where("object_id = ?", objectID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("DeleteClaimObject: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if affected == 0 {
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	}
	return respond.JSON(ctx, http.StatusOK, claimObject, 1)
}

func PostClaimUser(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id must be a uuid")
	}
	userClaim := &UserClaim{}
	if err := userClaim.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := userClaim.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	userClaim.AppID = appID
	userClaim.ClaimID = claimID
	if _, err := sdb.Instance().NewInsert().
		Model(userClaim).
		On("CONFLICT (claim_id, user_id, app_id) WHERE sa_deleted_at IS NULL DO UPDATE").
		Set("reason = EXCLUDED.reason").
		Set("sa_updated_at = now()").
		Returning("*").
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("PostClaimUser: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "user_claim insert failed")
	}
	return respond.JSON(ctx, http.StatusCreated, userClaim, 1)
}

func PatchClaimUser(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id must be a uuid")
	}
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	var userClaimID uuid.UUID
	if err := sdb.Instance().NewSelect().
		TableExpr("claimius.user_claim").
		Column("id").
		Where("claim_id = ?", claimID).
		Where("user_id = ?", userID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		OrderExpr("sa_created_at DESC").
		Limit(1).
		Scan(ctx.Request().Context(), &userClaimID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Errorf("PatchClaimUser lookup: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	patch := &UserClaim{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	var result struct {
		UserClaim json.RawMessage `bun:"update_user_claim"`
	}
	if err := sdb.Instance().NewRaw(
		"SELECT claimius.update_user_claim(?, ?, ?, ?) AS update_user_claim",
		userClaimID, patch.Reason, patch.StartsAt, patch.EndsAt,
	).Scan(ctx.Request().Context(), &result); err != nil {
		ctx.Logger().Errorf("PatchClaimUser update: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "user_claim update failed")
	}
	wrapper := struct {
		UserClaim UserClaim `json:"user_claim"`
	}{}
	if err := json.Unmarshal(result.UserClaim, &wrapper); err != nil {
		ctx.Logger().Errorf("PatchClaimUser unmarshal: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "user_claim decode failed")
	}
	return respond.JSON(ctx, http.StatusOK, wrapper.UserClaim, 1)
}

func DeleteClaimUser(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	var userClaim UserClaim
	res, err := sdb.Instance().NewUpdate().
		Model(&userClaim).
		Where("claim_id = ?", claimID).
		Where("user_id = ?", userID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("DeleteClaimUser: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if affected == 0 {
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	}
	return respond.JSON(ctx, http.StatusOK, userClaim, 1)
}

func GetClaimAudit(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	objectID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	params, err := utils.ValidateQueryParams(ctx, true, []string{"operation"})
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	audits := make([]Audit, 0)
	query := sdb.Instance().NewSelect().
		Model(&audits).
		Where("app_id = ?", appID).
		Where("object_id = ?", objectID).
		Where("object_type = ?", OBJECT_TYPE_CLAIM).
		OrderExpr("sa_created_at DESC").
		Limit(100)
	if operation := params.Get("operation"); operation != "" {
		query = query.Where("operation = ?", operation)
	}
	if err := query.Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetClaimAudit: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, audits, len(audits))
}

func getClaim(ctx echo.Context, claimID, appID uuid.UUID) (*Claim, error) {
	var claim Claim
	err := sdb.Instance().
		NewSelect().
		Model(&claim).
		ExcludeColumn("sa_deleted_at").
		Relation("ClaimObjects").
		Relation("ClaimUsers").
		Where("?TableAlias.id = ?", claimID).
		Where("?TableAlias.app_id = ?", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("getClaim: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	locationIDs := make([]uuid.UUID, 0)
	orgIDs := make([]uuid.UUID, 0)
	for _, co := range claim.ClaimObjects {
		if co.ObjectType == nil || co.ObjectID == nil {
			continue
		}
		switch *co.ObjectType {
		case OBJECT_TYPE_LOCATION:
			locationIDs = append(locationIDs, *co.ObjectID)
		case OBJECT_TYPE_ORGANIZATION:
			orgIDs = append(orgIDs, *co.ObjectID)
		}
	}

	locByID := make(map[uuid.UUID]*Location)
	orgByID := make(map[uuid.UUID]*Organization)

	if len(locationIDs) > 0 {
		var locations []Location
		if err = sdb.Instance().
			NewSelect().
			Model(&locations).
			ExcludeColumn("sa_deleted_at").
			Where("app_id = ?", appID).
			Where("sa_deleted_at IS NULL").
			Where("id IN (?)", bun.In(locationIDs)).
			Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("getClaim locations: %v", err)
			return nil, errorsx.ErrDbUnexpected
		}
		for i := range locations {
			locByID[locations[i].ID] = &locations[i]
		}
	}

	if len(orgIDs) > 0 {
		var orgs []Organization
		if err = sdb.Instance().
			NewSelect().
			Model(&orgs).
			ExcludeColumn("sa_deleted_at").
			Where("app_id = ?", appID).
			Where("sa_deleted_at IS NULL").
			Where("id IN (?)", bun.In(orgIDs)).
			Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("getClaim organizations: %v", err)
			return nil, errorsx.ErrDbUnexpected
		}
		for i := range orgs {
			orgByID[orgs[i].ID] = &orgs[i]
		}
	}

	for i, co := range claim.ClaimObjects {
		if co.ObjectType == nil || co.ObjectID == nil {
			continue
		}
		switch *co.ObjectType {
		case OBJECT_TYPE_LOCATION:
			if loc, ok := locByID[*co.ObjectID]; ok {
				claim.ClaimObjects[i].ObjectName = loc.Name
				claim.ClaimObjects[i].ObjectDescription = loc.Description
			}
		case OBJECT_TYPE_ORGANIZATION:
			if org, ok := orgByID[*co.ObjectID]; ok {
				claim.ClaimObjects[i].ObjectName = org.Name
				claim.ClaimObjects[i].ObjectDescription = org.Description
			}
		}
	}

	userIDs := make([]uuid.UUID, len(claim.ClaimUsers))
	for i, cu := range claim.ClaimUsers {
		userIDs[i] = cu.UserID
	}

	if len(userIDs) > 0 {
		var users []SamnaUser
		err = sdb.Instance().
			NewSelect().
			Model(&users).
			ExcludeColumn("sa_deleted_at").
			Where("app_id = ?", appID).
			Where("user_id IN (?)", bun.In(userIDs)).
			Where("sa_deleted_at IS NULL").
			Scan(ctx.Request().Context())
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			ctx.Logger().Errorf("getClaim users: %v", err)
			return nil, errorsx.ErrDbUnexpected
		}
		userByID := make(map[uuid.UUID]*SamnaUser, len(users))
		for i := range users {
			userByID[users[i].UserID] = &users[i]
		}
		for i, cu := range claim.ClaimUsers {
			if u, ok := userByID[cu.UserID]; ok {
				claim.ClaimUsers[i].FirstName = u.FirstName
				claim.ClaimUsers[i].LastName = u.LastName
			}
		}
	}

	return &claim, nil
}
