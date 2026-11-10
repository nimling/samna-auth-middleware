package echo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

type BodyUser struct {
	UserID     *uuid.UUID `json:"user_id"`
	FirstName  string     `json:"first_name"`
	LastName   string     `json:"last_name"`
	UserName   *string    `json:"user_name"`
	Phone      string     `json:"phone"`
	Email      string     `json:"email"`
	ExternalID *string    `json:"external_id"`
	ClaimID    *uuid.UUID `json:"claim_id"`
	Reason     *string    `json:"reason"`
	OwnerID    *uuid.UUID `json:"owner_id"`
	CreatedBy  *uuid.UUID `json:"created_by"`
}

type SamnaUserToken struct {
	Token     string     `json:"token"`
	IDClaim   *string    `json:"id_claim"`
	User      *SamnaUser `json:"user"`
	ClaimID   *uuid.UUID `json:"claim_id"`
	OwnerID   *uuid.UUID `json:"owner_id"`
	Reason    *string    `json:"reason"`
	CreatedBy *uuid.UUID `json:"created_by"`
}

type SamnaUserExternal struct {
	User      *SamnaUser `json:"user"`
	ClaimID   *uuid.UUID `json:"claim_id"`
	OwnerID   *uuid.UUID `json:"owner_id"`
	Reason    *string    `json:"reason"`
	CreatedBy *uuid.UUID `json:"created_by"`
}

type SamnaUserGuest struct {
	User      *SamnaUser `json:"user"`
	ClaimID   *uuid.UUID `json:"claim_id"`
	EndsAt    *time.Time `json:"ends_at"`
	OwnerID   *uuid.UUID `json:"owner_id"`
	Reason    *string    `json:"reason"`
	CreatedBy *uuid.UUID `json:"created_by"`
}

type BodyUserTokenValidate struct {
	Token      string `json:"token"`
	ExternalID string `json:"external_id"`
}

func GetUsers(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	params, err := utils.ValidateQueryParams(ctx, true, []string{"id", "email"})
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	users := make([]*SamnaUser, 0)
	query := sdb.Instance().
		NewSelect().
		Model(&users).
		ExcludeColumn("sa_deleted_at").
		Where("? IN (SELECT claimius.apps_for_object(?, user_id))", appID, OBJECT_TYPE_USER).
		Where("sa_deleted_at IS NULL")

	if ids, idErr := params.GetUUIDs("id"); idErr != nil {
		return respond.Error(ctx, http.StatusBadRequest, idErr.Error())
	} else if len(ids) > 0 {
		query.Where("id IN (?)", bun.In(ids))
	}

	if emails := params.GetSlice("email"); len(emails) > 0 {
		query.Where("email IN (?)", bun.In(emails))
	}

	_, err = query.ScanAndCount(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "user list query failed")
	}

	return respond.JSON(ctx, http.StatusOK, users, len(users))
}

func GetUser(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	user, err := getUser(ctx, userID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}

	return respond.JSON(ctx, http.StatusOK, user, 1)
}

func GetUserAssets(ctx echo.Context) error {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	var objectAssets []ObjectAsset
	err = sdb.Instance().NewSelect().
		Model(&objectAssets).
		ExcludeColumn("sa_deleted_at").
		Relation("Asset").
		Where("?TableAlias.object_id = ?", id).
		Where("?TableAlias.object_type = ?", OBJECT_TYPE_USER).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "")
	}

	app, _ := ctx.Get("app").(SamnaApp)
	assets := make([]*Asset, 0, len(objectAssets))
	for _, oa := range objectAssets {
		if oa.Asset != nil {
			oa.Asset.AssetURL = GetAssetURL(app, oa.Asset.ID)
			assets = append(assets, oa.Asset)
		}
	}

	return respond.JSON(ctx, http.StatusOK, assets, len(assets))
}

func GetUserClaims(ctx echo.Context) error {
	if err := respond.InitPagination(ctx, nil, nil); err != nil {
		ctx.Logger().Warnf("Failed to init pagination: %v", err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Warnf("Invalid user ID param: %v", err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	claims, count, err := getUserClaims(ctx, userID, respond.PaginationQuery(ctx))
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}

	return respond.JSON(ctx, http.StatusOK, claims, count)
}

func GetUserConnections(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	users, err := getUserConnections(ctx, userID)
	if err != nil && !errors.Is(err, errorsx.ErrNotFound) {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}

	return respond.JSON(ctx, http.StatusOK, users, len(users))
}

func GetUserAudit(ctx echo.Context) error {
	objectID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}

	params, err := utils.ValidateQueryParams(ctx, true, []string{"operation"})
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	audits, err := getUserAudit(ctx, "claimius.samna_user", &objectID, params.Get("operation"))
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}

	return respond.JSON(ctx, http.StatusOK, audits, len(audits))
}

func GetUserRelations(ctx echo.Context) error {
	targetUserID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}

	relations, err := getUserRelations(ctx, targetUserID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}

	return respond.JSON(ctx, http.StatusOK, relations, len(relations))
}

func GetUserOrganizations(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	orgs, err := getUserOrganizations(ctx, userID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, orgs, len(orgs))
}

func GetUserLocations(ctx echo.Context) error {
	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	locations, err := getUserLocations(ctx, userID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, locations, len(locations))
}

func PostUser(ctx echo.Context) error {
	var req BodyUser
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}

	if req.Email == "" {
		return respond.Error(ctx, http.StatusBadRequest, "email required")
	}

	userName := ""
	if req.UserName != nil {
		userName = *req.UserName
	}
	user, err := postUser(ctx, req, userName)
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	if req.ClaimID != nil {
		if _, err := sdb.Instance().NewRaw(
			"SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?, p_reason => ?)",
			*req.ClaimID, user.UserID, req.OwnerID, req.CreatedBy, req.Reason,
		).Exec(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("assign claim: %v", err)
		}
	}

	return respond.JSON(ctx, http.StatusCreated, user, 1)
}

func PutUser(ctx echo.Context) error {

	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}

	updUser := &SamnaUser{}
	if err := updUser.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := updUser.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	updUser.UserID = userID

	if err := putUser(ctx, updUser); err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}

	return respond.JSON(ctx, http.StatusOK, updUser, 1)
}

func DeleteUser(ctx echo.Context) error {

	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var user SamnaUser
	var affected int64
	err = sdb.Instance().RunInTx(ctx.Request().Context(), nil, func(c context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().
			Model(&user).
			Set("sa_deleted_at = NOW()").
			Where("user_id = ?", userID).
			Where("sa_deleted_at IS NULL").
			Returning("*").
			Exec(c)
		if err != nil {
			return err
		}
		if affected, err = res.RowsAffected(); err != nil || affected == 0 {
			return err
		}
		_, err = tx.NewRaw("UPDATE claimius.user_identity SET sa_deleted_at = NOW() WHERE user_id = ? AND sa_deleted_at IS NULL", userID).Exec(c)
		return err
	})
	if err != nil {
		ctx.Logger().Errorf("DeleteUser: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal Server Error")
	}
	if affected == 0 {
		return respond.Error(ctx, http.StatusNotFound, "Not Found")
	}

	return respond.JSON(ctx, http.StatusOK, user, 1)
}

func PostUserToken(ctx echo.Context) error {
	var req SamnaUserToken
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	if req.Token == "" {
		return respond.Error(ctx, http.StatusBadRequest, "token is required")
	}
	if req.ClaimID == nil || req.OwnerID == nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id and owner_id are required")
	}

	parsed, _, err := jwt.NewParser().ParseUnverified(req.Token, jwt.MapClaims{})
	if err != nil {
		ctx.Logger().Warnf("PostUserToken parse: %v", err)
		return respond.Error(ctx, http.StatusBadRequest, "invalid token")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "invalid token claims")
	}
	idClaim := "sub"
	if req.IDClaim != nil && *req.IDClaim != "" {
		idClaim = *req.IDClaim
	}
	rawID, present := claims[idClaim]
	if !present {
		return respond.Error(ctx, http.StatusBadRequest, "id claim missing in token")
	}
	externalID, ok := rawID.(string)
	if !ok || externalID == "" {
		return respond.Error(ctx, http.StatusBadRequest, "id claim not a string")
	}
	in := SamnaUser{}
	if req.User != nil {
		in = *req.User
	}
	in.ExternalID = &externalID
	identity := map[string]any{"external_id": externalID, "id_claim": idClaim, "type": "token", "data": claims}
	user, status, err := createOrganizationUser(ctx, req.CreatedBy, req.OwnerID, req.ClaimID, in, identity, req.Reason)
	if err != nil {
		return respond.Error(ctx, status, err.Error())
	}
	return respond.JSON(ctx, status, user, 1)
}

func PostUserTokenValidate(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)
	if appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}

	var req BodyUserTokenValidate
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	if (req.Token == "") == (req.ExternalID == "") {
		return respond.Error(ctx, http.StatusBadRequest, "token or external_id is required")
	}

	var match struct {
		UserID  uuid.UUID `bun:"user_id"`
		OwnerID uuid.UUID `bun:"sa_owner_id"`
	}
	var err error
	if req.ExternalID != "" {
		err = sdb.Instance().NewRaw(
			"SELECT t.user_id, t.sa_owner_id FROM claimius.user_identity t JOIN claimius.samna_user u ON u.user_id = t.user_id "+
				"WHERE claimius.app_has_object(?, 'claimius.organization', t.sa_owner_id) AND u.sa_deleted_at IS NULL AND t.sa_deleted_at IS NULL AND t.type = 'external' AND t.external_id = ? "+
				"ORDER BY t.sa_created_at DESC LIMIT 1",
			appID, req.ExternalID,
		).Scan(ctx.Request().Context(), &match)
	} else {
		parsed, _, perr := jwt.NewParser().ParseUnverified(req.Token, jwt.MapClaims{})
		if perr != nil {
			return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
		}
		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok {
			return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
		}
		claimsJSON, merr := json.Marshal(claims)
		if merr != nil {
			ctx.Logger().Errorf("marshal token claims: %v", merr)
			return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
		}
		err = sdb.Instance().NewRaw(
			"SELECT t.user_id, t.sa_owner_id FROM claimius.user_identity t JOIN claimius.samna_user u ON u.user_id = t.user_id "+
				"WHERE claimius.app_has_object(?, 'claimius.organization', t.sa_owner_id) AND u.sa_deleted_at IS NULL AND t.sa_deleted_at IS NULL AND t.external_id = (?::jsonb ->> t.id_claim) "+
				"ORDER BY t.sa_created_at DESC LIMIT 1",
			appID, string(claimsJSON),
		).Scan(ctx.Request().Context(), &match)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
		}
		ctx.Logger().Errorf("validate token lookup: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	var user SamnaUser
	if err := sdb.Instance().NewSelect().
		Model(&user).
		Where("user_id = ?", match.UserID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("validate token user: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusOK, struct {
		SamnaUser
		OwnerID uuid.UUID `json:"owner_id"`
	}{user, match.OwnerID}, 1)
}

func PostUserExternal(ctx echo.Context) error {
	var req SamnaUserExternal
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	if req.ClaimID == nil || req.OwnerID == nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id and owner_id are required")
	}
	if req.User == nil || req.User.ExternalID == nil || *req.User.ExternalID == "" {
		return respond.Error(ctx, http.StatusBadRequest, "user.external_id is required")
	}
	identity := map[string]any{"external_id": *req.User.ExternalID, "id_claim": "external", "type": "external", "data": map[string]any{}}
	user, status, err := createOrganizationUser(ctx, req.CreatedBy, req.OwnerID, req.ClaimID, req.User, identity, req.Reason)
	if err != nil {
		return respond.Error(ctx, status, err.Error())
	}
	return respond.JSON(ctx, status, user, 1)
}

func PostUserGuest(ctx echo.Context) error {
	var req SamnaUserGuest
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	if req.ClaimID == nil || req.OwnerID == nil || req.EndsAt == nil || req.EndsAt.IsZero() || req.User == nil {
		return respond.Error(ctx, http.StatusBadRequest, "user, claim_id, owner_id and ends_at are required")
	}
	if appID, _ := ctx.Get("app_id").(uuid.UUID); appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if req.CreatedBy == nil || *req.CreatedBy == uuid.Nil {
		return respond.Error(ctx, http.StatusBadRequest, "created_by is required")
	}
	raw, err := json.Marshal(req.User)
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	body["claim_id"] = *req.ClaimID
	userJSON, err := json.Marshal(body)
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	var payload []byte
	if err := sdb.Instance().NewRaw(
		"SELECT claimius.insert_organization_guest(?, ?, ?::jsonb, ?, ?)",
		*req.CreatedBy, *req.OwnerID, string(userJSON), *req.EndsAt, req.Reason,
	).Scan(ctx.Request().Context(), &payload); err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) {
			switch pgErr.Field('C') {
			case "P0001":
				return respond.Error(ctx, http.StatusBadRequest, pgErr.Field('M'))
			case "P0403":
				return respond.Error(ctx, http.StatusForbidden, pgErr.Field('M'))
			}
		}
		ctx.Logger().Errorf("insert_organization_guest: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	var result struct {
		SamnaUser *SamnaUser `json:"samna_user"`
		Created   bool       `json:"created"`
	}
	if err := json.Unmarshal(payload, &result); err != nil || result.SamnaUser == nil {
		ctx.Logger().Errorf("unmarshal insert_organization_guest: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	return respond.JSON(ctx, status, result.SamnaUser, 1)
}

func getUser(ctx echo.Context, userID uuid.UUID) (*SamnaUser, error) {

	var user SamnaUser
	err := sdb.Instance().
		NewSelect().
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Model(&user).
		ExcludeColumn("sa_deleted_at").
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("getUser: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	return &user, nil
}

func getUserConnections(ctx echo.Context, userID uuid.UUID) ([]*SamnaUser, error) {
	appID := ctx.Get("app_id").(uuid.UUID)
	users := make([]*SamnaUser, 0)
	err := sdb.Instance().
		NewSelect().
		Model(&users).
		ExcludeColumn("sa_deleted_at").
		Where("user_id != ?", userID).
		Where("? IN (SELECT claimius.apps_for_object(?, user_id))", appID, OBJECT_TYPE_USER).
		Where("sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []*SamnaUser{}, nil
		}
		ctx.Logger().Errorf("getUserConnections: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return users, nil
}

func getUserClaims(ctx echo.Context, userID uuid.UUID, paginate func(*bun.SelectQuery) *bun.SelectQuery) ([]Claim, int, error) {
	claims := make([]Claim, 0)
	query := sdb.Instance().
		NewSelect().
		Model(&claims).
		ExcludeColumn("sa_deleted_at").
		Distinct().
		Relation("ClaimObjects").
		Relation("ClaimUsers").
		Where("claim.sa_deleted_at IS NULL").
		Where("EXISTS (SELECT 1 FROM claimius.user_claim uc WHERE uc.claim_id = claim.id AND uc.user_id = ? AND uc.sa_deleted_at IS NULL)", userID)

	count, err := query.Count(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("getUserClaims count: %v", err)
		return nil, 0, errorsx.ErrDbUnexpected
	}

	if paginate != nil {
		query = query.Apply(paginate)
	}

	if err := query.Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("getUserClaims scan: %v", err)
		return nil, 0, errorsx.ErrDbUnexpected
	}

	return claims, count, nil
}

func getUserAudit(ctx echo.Context, objectType string, objectID *uuid.UUID, operation string) ([]Audit, error) {
	appID := ctx.Get("app_id").(uuid.UUID)

	audits := make([]Audit, 0)
	query := sdb.Instance().NewSelect().
		Model(&audits).
		Where("app_id = ?", appID).
		OrderExpr("sa_created_at DESC").
		Limit(100)

	if objectType != "" {
		query = query.Where("object_type = ?", objectType)
	}
	if objectID != nil {
		query = query.Where("object_id = ?", *objectID)
	}
	if operation != "" {
		query = query.Where("operation = ?", operation)
	}

	if err := query.Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("getUserAudit: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	return audits, nil
}

func getUserRelations(ctx echo.Context, userID uuid.UUID) ([]UserRelation, error) {
	relations := make([]UserRelation, 0)
	appID := ctx.Get("app_id").(uuid.UUID)
	err := sdb.Instance().NewSelect().
		Model(&relations).
		ExcludeColumn("sa_deleted_at").
		Where("?TableAlias.user_id = ?", userID).
		Where("?TableAlias.app_id = ?", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		OrderExpr("?TableAlias.priority DESC, ?TableAlias.used_at DESC NULLS LAST").
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("getUserRelations: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return relations, nil
}

func getUserOrganizations(ctx echo.Context, userID uuid.UUID) ([]Organization, error) {
	appID := ctx.Get("app_id").(uuid.UUID)
	organizations := make([]Organization, 0)
	err := sdb.Instance().NewSelect().
		Model(&organizations).
		ExcludeColumn("sa_deleted_at").
		Where("organization.id IN (SELECT claimius.app_object_ids(?, ?))", appID, OBJECT_TYPE_ORGANIZATION).
		Where("sa_deleted_at IS NULL").
		Where("EXISTS (SELECT 1 FROM claimius.user_object uo WHERE uo.object_id = organization.id AND uo.object_type = 'claimius.organization' AND uo.user_id = ?)", userID).
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("getUserOrganizations: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return organizations, nil
}

func getUserLocations(ctx echo.Context, userID uuid.UUID) ([]Location, error) {
	appID := ctx.Get("app_id").(uuid.UUID)
	locations := make([]Location, 0)
	err := sdb.Instance().NewSelect().
		Model(&locations).
		ExcludeColumn("sa_deleted_at").
		Where("location.id IN (SELECT claimius.app_object_ids(?, ?))", appID, OBJECT_TYPE_LOCATION).
		Where("sa_deleted_at IS NULL").
		Where("EXISTS (SELECT 1 FROM claimius.user_object uo WHERE uo.object_id = location.id AND uo.object_type = 'claimius.location' AND uo.user_id = ?)", userID).
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("getUserLocations: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return locations, nil
}

func postUser(ctx echo.Context, req BodyUser, userName string) (*SamnaUser, error) {
	var userID uuid.UUID
	err := sdb.Instance().NewRaw(`INSERT INTO claimius.samna_user (user_id, first_name, last_name, user_name, email, phone, external_id)
		VALUES (coalesce(?::uuid, gen_random_uuid()), ?, ?,
		        CASE WHEN NOT EXISTS (
		            SELECT 1 FROM claimius.samna_user u
		            WHERE lower(u.user_name) = lower(?) AND u.sa_deleted_at IS NULL
		        ) THEN nullif(?, '') END,
		        ?, nullif(?, ''), ?)
		ON CONFLICT (user_id) WHERE sa_deleted_at IS NULL DO UPDATE SET
		    first_name = EXCLUDED.first_name,
		    last_name = EXCLUDED.last_name,
		    email = EXCLUDED.email,
		    sa_updated_at = now()
		RETURNING user_id`,
		req.UserID, req.FirstName, req.LastName, userName, userName, req.Email, req.Phone, req.ExternalID,
	).Scan(ctx.Request().Context(), &userID)
	if err != nil {
		ctx.Logger().Errorf("upsert samna_user: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	var user SamnaUser
	err = sdb.Instance().NewSelect().
		Model(&user).
		ExcludeColumn("sa_deleted_at").
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("fetch samna_user: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	return &user, nil
}

func putUser(ctx echo.Context, user *SamnaUser) error {
	res, err := sdb.Instance().
		NewUpdate().
		Model(user).
		Where("sa_deleted_at IS NULL").
		Where("user_id = ?", user.UserID).
		OmitZero().
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("putUser: %v", err)
		return errorsx.ErrDbUnexpected
	}

	affected, err := res.RowsAffected()
	if err != nil {
		ctx.Logger().Errorf("putUser: %v", err)
		return errorsx.ErrDbUnexpected
	}

	if affected == 0 {
		return errorsx.ErrNotFound
	}

	return nil
}

func createOrganizationUser(ctx echo.Context, createdBy, ownerID, claimID *uuid.UUID, user any, identity map[string]any, reason *string) (*SamnaUser, int, error) {
	appID := ctx.Get("app_id").(uuid.UUID)
	if appID == uuid.Nil {
		return nil, http.StatusUnauthorized, errors.New("Unauthorized")
	}
	if createdBy == nil || *createdBy == uuid.Nil {
		return nil, http.StatusBadRequest, errors.New("created_by is required")
	}
	if ownerID == nil || claimID == nil {
		return nil, http.StatusBadRequest, errors.New("claim_id and owner_id are required")
	}
	raw, err := json.Marshal(user)
	if err != nil {
		return nil, http.StatusBadRequest, errors.New("Malformed request")
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, http.StatusBadRequest, errors.New("Malformed request")
	}
	body["claim_id"] = *claimID
	userJSON, err := json.Marshal(body)
	if err != nil {
		return nil, http.StatusBadRequest, errors.New("Malformed request")
	}
	identityJSON, err := json.Marshal(identity)
	if err != nil {
		return nil, http.StatusBadRequest, errors.New("Malformed request")
	}

	var payload []byte
	if err := sdb.Instance().NewRaw(
		"SELECT claimius.insert_organization_user(?, ?, ?::jsonb, ?::jsonb, ?)",
		*createdBy, *ownerID, string(userJSON), string(identityJSON), reason,
	).Scan(ctx.Request().Context(), &payload); err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) {
			switch pgErr.Field('C') {
			case "23505":
				return nil, http.StatusConflict, errors.New("user already exists")
			case "P0001":
				return nil, http.StatusBadRequest, errors.New(pgErr.Field('M'))
			}
		}
		ctx.Logger().Errorf("insert_organization_user: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}

	var result struct {
		SamnaUser *SamnaUser `json:"samna_user"`
		Created   bool       `json:"created"`
	}
	if err := json.Unmarshal(payload, &result); err != nil || result.SamnaUser == nil {
		ctx.Logger().Errorf("unmarshal insert_organization_user: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	return result.SamnaUser, status, nil
}
