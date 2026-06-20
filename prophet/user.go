package prophet

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
	"github.com/uptrace/bun"
)

type BodyUser struct {
	UserID     *uuid.UUID `json:"user_id"`
	FirstName  string     `json:"first_name"`
	LastName   string     `json:"last_name"`
	UserName   *string    `json:"user_name"`
	UserImage  *string    `json:"user_image"`
	Phone      string     `json:"phone"`
	Email      string     `json:"email"`
	ExternalID *string    `json:"external_id"`
	ClaimID    *uuid.UUID `json:"claim_id"`
	AppID      *uuid.UUID `json:"app_id"`
	Reason     *string    `json:"reason"`
}

type SamnaUserToken struct {
	Token   string     `json:"token"`
	IDClaim *string    `json:"id_claim"`
	User    *SamnaUser `json:"user"`
	ClaimID *uuid.UUID `json:"claim_id"`
	OwnerID *uuid.UUID `json:"owner_id"`
	Reason  *string    `json:"reason"`
}

type SamnaUserExternal struct {
	User    *SamnaUser `json:"user"`
	ClaimID *uuid.UUID `json:"claim_id"`
	OwnerID *uuid.UUID `json:"owner_id"`
	Reason  *string    `json:"reason"`
}

type BodyUserTokenValidate struct {
	Token string `json:"token"`
}

func GetUsers(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	params, err := utils.ValidateQueryParams(ctx, true, []string{"id", "email"})
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	users := make([]*SamnaUser, 0)
	query := shared.Instance().
		NewSelect().
		Model(&users).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
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

	app, _ := ctx.Get("app").(SamnaApp)
	populateUserImage(user, app.RedirectURI)

	return respond.JSON(ctx, http.StatusOK, user, 1)
}

func GetUserAssets(ctx echo.Context) error {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "")
	}

	var objectAssets []ObjectAsset
	err = shared.Instance().NewSelect().
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
			oa.Asset.AssetURL = app.RedirectURI + "/api/asset/" + oa.Asset.ID.String() + "/data"
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
	appID := ctx.Get("app_id").(uuid.UUID)

	var req BodyUser
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}

	if req.Email == "" {
		return respond.Error(ctx, http.StatusBadRequest, "email required")
	}

	req.AppID = &appID

	userName := ""
	if req.UserName != nil {
		userName = *req.UserName
	}
	userImage := ""
	if req.UserImage != nil {
		userImage = *req.UserImage
	}

	user, err := postUser(ctx, req, userName, userImage)
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	if req.ClaimID != nil {
		userClaim := UserClaim{
			AppID:   appID,
			ClaimID: *req.ClaimID,
			UserID:  user.UserID,
			Reason:  req.Reason,
		}
		if _, err := shared.Instance().NewInsert().
			Model(&userClaim).
			Exec(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("assign claim: %v", err)
		}
	}

	return respond.JSON(ctx, http.StatusCreated, user, 1)
}

func PutUser(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

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
	updUser.AppID = appID

	if err := putUser(ctx, updUser); err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}

	return respond.JSON(ctx, http.StatusOK, updUser, 1)
}

func DeleteUser(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	userID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var user SamnaUser
	res, err := shared.Instance().
		NewUpdate().
		Model(&user).
		Set("sa_deleted_at = NOW()").
		Where("user_id = ?", userID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("DeleteUser: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal Server Error")
	}

	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return respond.Error(ctx, http.StatusNotFound, "Not Found")
	}

	return respond.NoContent(ctx, http.StatusOK)
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
	var userID uuid.UUID
	if req.User != nil && req.User.UserID != uuid.Nil {
		userID = req.User.UserID
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		ctx.Logger().Errorf("marshal token claims: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	user, status, err := upsertOrganizationUser(ctx, *req.OwnerID, *req.ClaimID, userID, idClaim, externalID, "token", claimsJSON, req.User, req.Reason)
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
	if req.Token == "" {
		return respond.Error(ctx, http.StatusBadRequest, "token is required")
	}

	parsed, _, err := jwt.NewParser().ParseUnverified(req.Token, jwt.MapClaims{})
	if err != nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		ctx.Logger().Errorf("marshal token claims: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	var user SamnaUser
	err = shared.Instance().NewRaw(
		"SELECT u.* FROM claimius.samna_user u JOIN claimius.user_token t ON t.user_id = u.user_id AND t.app_id = u.app_id "+
			"WHERE u.app_id = ? AND u.sa_deleted_at IS NULL AND t.sa_deleted_at IS NULL AND t.external_id = (?::jsonb ->> t.id_claim) "+
			"ORDER BY t.sa_created_at DESC LIMIT 1",
		appID, string(claimsJSON),
	).Scan(ctx.Request().Context(), &user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
		}
		ctx.Logger().Errorf("validate token lookup: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusOK, user, 1)
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
	user, status, err := upsertOrganizationUser(ctx, *req.OwnerID, *req.ClaimID, req.User.UserID, "external", *req.User.ExternalID, "external", json.RawMessage("{}"), req.User, req.Reason)
	if err != nil {
		return respond.Error(ctx, status, err.Error())
	}
	return respond.JSON(ctx, status, user, 1)
}

func getUser(ctx echo.Context, userID uuid.UUID) (*SamnaUser, error) {
	appID := ctx.Get("app_id").(uuid.UUID)

	var user SamnaUser
	err := shared.Instance().
		NewSelect().
		Where("user_id = ?", userID).
		Where("app_id = ?", appID).
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
	err := shared.Instance().
		NewSelect().
		Model(&users).
		ExcludeColumn("sa_deleted_at").
		Where("user_id != ?", userID).
		Where("app_id = ?", appID).
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
	appID := ctx.Get("app_id").(uuid.UUID)
	claims := make([]Claim, 0)
	query := shared.Instance().
		NewSelect().
		Model(&claims).
		ExcludeColumn("sa_deleted_at").
		Distinct().
		Relation("ClaimObjects").
		Relation("ClaimUsers").
		Where("claim.app_id = ?", appID).
		Where("claim.sa_deleted_at IS NULL").
		Where("EXISTS (SELECT 1 FROM claimius.user_claim uc WHERE uc.claim_id = claim.id AND uc.user_id = ? AND uc.app_id = ? AND uc.sa_deleted_at IS NULL)", userID, appID)

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
	query := shared.Instance().NewSelect().
		Model(&audits).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
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
	err := shared.Instance().NewSelect().
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
	err := shared.Instance().NewSelect().
		Model(&organizations).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Where("EXISTS (SELECT 1 FROM claimius.user_object uo WHERE uo.object_id = organization.id AND uo.object_type = 'claimius.organization' AND uo.user_id = ? AND uo.app_id = ?)", userID, appID).
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
	err := shared.Instance().NewSelect().
		Model(&locations).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Where("EXISTS (SELECT 1 FROM claimius.user_object uo WHERE uo.object_id = location.id AND uo.object_type = 'claimius.location' AND uo.user_id = ? AND uo.app_id = ?)", userID, appID).
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("getUserLocations: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return locations, nil
}

func postUser(ctx echo.Context, req BodyUser, userName, userImage string) (*SamnaUser, error) {
	var userID uuid.UUID
	err := shared.Instance().NewRaw(
		"SELECT (claimius.ensure_app_user(?, ?, ?, ?, ?, ?, ?, ?, ?) -> 'samna_user' ->> 'user_id')::uuid",
		req.UserID, *req.AppID, req.FirstName, req.LastName, userName, userImage, req.Email, req.Phone, req.ExternalID,
	).Scan(ctx.Request().Context(), &userID)
	if err != nil {
		ctx.Logger().Errorf("ensure_app_user: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	if _, err := shared.Instance().NewUpdate().
		Model((*SamnaUser)(nil)).
		Set("first_name = ?", req.FirstName).
		Set("last_name = ?", req.LastName).
		Set("email = ?", req.Email).
		Where("app_id = ?", *req.AppID).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("update samna_user: %v", err)
	}

	var user SamnaUser
	err = shared.Instance().NewSelect().
		Model(&user).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", *req.AppID).
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
	res, err := shared.Instance().
		NewUpdate().
		Model(user).
		Where("sa_deleted_at IS NULL").
		Where("user_id = ?", user.UserID).
		Where("app_id = ?", user.AppID).
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

func populateUserImage(appUser *SamnaUser, baseURL string) {
	if appUser == nil || appUser.UserImage != nil {
		return
	}
	q := appUser.Email
	if q == "" {
		q = appUser.FirstName + " " + appUser.LastName
	}
	if strings.TrimSpace(q) == "" {
		return
	}
	placeholder := baseURL + "/api/placeholder?q=" + url.QueryEscape(q)
	appUser.UserImage = &placeholder
}

func upsertOrganizationUser(ctx echo.Context, ownerID, claimID, userID uuid.UUID, idClaim, externalID, identType string, data json.RawMessage, in *SamnaUser, reason *string) (*SamnaUser, int, error) {
	appID := ctx.Get("app_id").(uuid.UUID)
	if appID == uuid.Nil {
		return nil, http.StatusUnauthorized, errors.New("Unauthorized")
	}
	reqctx := ctx.Request().Context()

	var rootID uuid.UUID
	if err := shared.Instance().NewRaw(
		"SELECT sa_root_id FROM claimius.organization WHERE app_id = ? AND id = ? AND sa_deleted_at IS NULL LIMIT 1",
		appID, ownerID,
	).Scan(reqctx, &rootID); err != nil {
		ctx.Logger().Errorf("organization lookup: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}

	var existingUserID uuid.UUID
	err := shared.Instance().NewRaw(
		"SELECT user_id FROM claimius.user_token WHERE app_id = ? AND sa_owner_id = ? AND id_claim = ? AND external_id = ? AND sa_deleted_at IS NULL LIMIT 1",
		appID, ownerID, idClaim, externalID,
	).Scan(reqctx, &existingUserID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ctx.Logger().Errorf("user_token lookup: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}
	created := existingUserID == uuid.Nil
	if existingUserID != uuid.Nil {
		userID = existingUserID
	}
	if userID == uuid.Nil {
		userID = uuid.New()
	}

	firstName, lastName, userName, userImage, email, phone := "", "", "", "", "", ""
	if in != nil {
		firstName, lastName, email = in.FirstName, in.LastName, in.Email
		if in.UserName != nil {
			userName = *in.UserName
		}
		if in.UserImage != nil {
			userImage = *in.UserImage
		}
		if in.Phone != nil {
			phone = *in.Phone
		}
	}

	var user SamnaUser
	err = shared.Instance().NewRaw(
		"INSERT INTO claimius.samna_user (user_id, app_id, first_name, last_name, user_name, user_image, email, phone, external_id, status, type) "+
			"VALUES (?, ?, NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), 'active', 'user') "+
			"ON CONFLICT (user_id, app_id) WHERE sa_deleted_at IS NULL DO UPDATE SET "+
			"first_name = COALESCE(EXCLUDED.first_name, claimius.samna_user.first_name), "+
			"last_name = COALESCE(EXCLUDED.last_name, claimius.samna_user.last_name), "+
			"user_name = COALESCE(EXCLUDED.user_name, claimius.samna_user.user_name), "+
			"user_image = COALESCE(EXCLUDED.user_image, claimius.samna_user.user_image), "+
			"email = COALESCE(EXCLUDED.email, claimius.samna_user.email), "+
			"phone = COALESCE(EXCLUDED.phone, claimius.samna_user.phone), "+
			"external_id = COALESCE(EXCLUDED.external_id, claimius.samna_user.external_id), "+
			"sa_updated_at = now() RETURNING *",
		userID, appID, firstName, lastName, userName, userImage, email, phone, externalID,
	).Scan(reqctx, &user)
	if err != nil {
		ctx.Logger().Errorf("upsert samna_user: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}
	userID = user.UserID

	var hasClaim bool
	if err := shared.Instance().NewRaw("SELECT EXISTS (SELECT 1 FROM claimius.user_claim WHERE app_id = ? AND claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL)", appID, claimID, userID).Scan(reqctx, &hasClaim); err != nil {
		ctx.Logger().Errorf("lookup user_claim: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}
	if !hasClaim {
		if _, err := shared.Instance().NewRaw("SELECT claimius.assign_claim_user(?, ?, ?, NULL, ?)", claimID, userID, ownerID, reason).Exec(reqctx); err != nil {
			ctx.Logger().Errorf("assign_claim_user: %v", err)
			return nil, http.StatusInternalServerError, errors.New("Internal server error")
		}
	}

	virtualName := "user:" + userID.String()
	var virtualID uuid.UUID
	err = shared.Instance().NewRaw("SELECT id FROM claimius.claim WHERE app_id = ? AND name = ? AND type = 'virtual' AND sa_deleted_at IS NULL LIMIT 1", appID, virtualName).Scan(reqctx, &virtualID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ctx.Logger().Errorf("lookup virtual claim: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}
	if virtualID == uuid.Nil {
		if err := shared.Instance().NewRaw("SELECT (claimius.create_claim(?, NULL, 15, ?, ?, NULL, TRUE, 'virtual') -> 'claim' ->> 'id')::uuid", virtualName, ownerID, rootID).Scan(reqctx, &virtualID); err != nil {
			ctx.Logger().Errorf("create virtual claim: %v", err)
			return nil, http.StatusInternalServerError, errors.New("Internal server error")
		}
	}

	var hasObj bool
	if err := shared.Instance().NewRaw("SELECT EXISTS (SELECT 1 FROM claimius.claim_object WHERE app_id = ? AND claim_id = ? AND object_id = ? AND object_type = 'claimius.samna_user' AND sa_deleted_at IS NULL)", appID, virtualID, userID).Scan(reqctx, &hasObj); err != nil {
		ctx.Logger().Errorf("lookup virtual claim_object: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}
	if !hasObj {
		if _, err := shared.Instance().NewRaw("SELECT claimius.assign_claim_object(?, ?, ?, NULL, ?, 'claimius.samna_user')", virtualID, ownerID, rootID, userID).Exec(reqctx); err != nil {
			ctx.Logger().Errorf("assign virtual claim_object: %v", err)
			return nil, http.StatusInternalServerError, errors.New("Internal server error")
		}
	}

	if _, err := shared.Instance().NewRaw(
		"INSERT INTO claimius.user_token (app_id, user_id, id_claim, external_id, type, claim_id, data, sa_owner_id) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?) "+
			"ON CONFLICT (app_id, sa_owner_id, id_claim, external_id) WHERE sa_deleted_at IS NULL DO UPDATE SET "+
			"user_id = EXCLUDED.user_id, type = EXCLUDED.type, claim_id = EXCLUDED.claim_id, data = EXCLUDED.data, sa_updated_at = now()",
		appID, userID, idClaim, externalID, identType, claimID, string(data), ownerID,
	).Exec(reqctx); err != nil {
		ctx.Logger().Errorf("upsert user_token: %v", err)
		return nil, http.StatusInternalServerError, errors.New("Internal server error")
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return &user, status, nil
}
