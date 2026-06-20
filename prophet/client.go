package prophet

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
	"golang.org/x/crypto/bcrypt"
)

func GetClients(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	var clients []SamnaClient
	err := shared.Instance().NewSelect().
		Model(&clients).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		OrderExpr("sa_created_at DESC").
		Scan(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "samna_client list query failed")
	}

	if clients == nil {
		clients = []SamnaClient{}
	}

	return respond.JSON(ctx, http.StatusOK, clients, len(clients))
}

func GetClient(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var client SamnaClient
	err = shared.Instance().NewSelect().
		Model(&client).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", clientID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusOK, client, 1)
}

func PostClient(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	client := &SamnaClient{}
	if err := client.Bind(ctx); err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	if err := client.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	rawSecret := base64.RawURLEncoding.EncodeToString(secretBytes)
	hashedSecret, err := bcrypt.GenerateFromPassword([]byte(rawSecret), bcrypt.DefaultCost)
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	client.AppID = appID
	client.SecretHash = string(hashedSecret)

	_, err = shared.Instance().NewInsert().
		Model(client).
		Returning("*").
		Exec(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	client.Secret = &rawSecret
	return respond.JSON(ctx, http.StatusCreated, client, 1)
}

func PutClient(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}

	var client SamnaClient
	err = shared.Instance().NewSelect().
		Model(&client).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", clientID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "client not found")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "client update failed")
	}

	patch := &SamnaClient{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	if patch.HasKey("name") {
		client.Name = patch.Name
	}
	if patch.HasKey("description") {
		client.Description = patch.Description
	}
	if patch.HasKey("expires_at") {
		client.ExpiresAt = patch.ExpiresAt
	}
	if patch.HasKey("status") {
		client.Status = patch.Status
	}

	err = shared.Instance().NewUpdate().
		Model(&client).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Where("id = ?", clientID).
		Where("app_id = ?", appID).
		Returning("*").
		Scan(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Warn(err)
		return respond.Error(ctx, http.StatusInternalServerError, "client update failed")
	}

	return respond.JSON(ctx, http.StatusOK, client, 1)
}

func DeleteClient(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var client SamnaClient
	err = shared.Instance().NewUpdate().
		Model(&client).
		Where("id = ?", clientID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusOK, client, 1)
}

func PostClientRotate(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var client SamnaClient
	err = shared.Instance().NewSelect().
		Model(&client).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", clientID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	rawSecret := base64.RawURLEncoding.EncodeToString(secretBytes)
	hashedSecret, err := bcrypt.GenerateFromPassword([]byte(rawSecret), bcrypt.DefaultCost)
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	err = shared.Instance().NewUpdate().
		Model((*SamnaClient)(nil)).
		Where("id = ?", clientID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("secret_hash = ?", string(hashedSecret)).
		Set("sa_updated_at = NOW()").
		Returning("*").
		Scan(ctx.Request().Context(), &client)

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	client.Secret = &rawSecret
	return respond.JSON(ctx, http.StatusOK, client, 1)
}

func PostClientClaim(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var client SamnaClient
	err = shared.Instance().NewSelect().
		Model(&client).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", clientID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "Client not found")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	userClaim := &UserClaim{}
	if err := userClaim.Bind(ctx); err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	if err := userClaim.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	userClaim.AppID = appID

	_, err = shared.Instance().NewInsert().
		Model(userClaim).
		Returning("*").
		Exec(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusCreated, userClaim, 1)
}

func DeleteClientClaim(ctx echo.Context) error {
	appID := ctx.Get("app_id").(uuid.UUID)

	clientID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	claimID, err := uuid.Parse(ctx.Param("claim_id"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	var userClaim UserClaim
	err = shared.Instance().NewUpdate().
		Model(&userClaim).
		Where("id = ?", claimID).
		Where("user_id = ?", clientID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Scan(ctx.Request().Context())

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return respond.Error(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusOK, userClaim, 1)
}
