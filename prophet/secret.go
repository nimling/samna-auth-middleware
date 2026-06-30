package prophet

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/encryption"
	"github.com/nimling/samna-auth-middleware/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	prophetdb "github.com/nimling/samna-auth-middleware/prophet/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetSecrets(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	secrets, err := secretSelectAll(ctx, appID)
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "secret list query failed")
	}
	return respond.JSON(ctx, http.StatusOK, secrets, len(secrets))
}

func GetSecret(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	secret, err := secretSelect(ctx, secretID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, secret, 1)
}

func PostSecret(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	masterKey := ctx.Get("app_key").(encryption.MasterKeyPair)

	secret := &SamnaSecret{}
	if err := secret.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := secret.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	encrypted, err := utils.AesEncrypt(secret.Value, masterKey.Seed)
	if err != nil {
		ctx.Logger().Errorf("PostSecret encrypt: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "secret encryption failed")
	}

	secret.AppID = appID
	secret.Value = hex.EncodeToString(encrypted)

	created, err := secretInsert(ctx, secret)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusCreated, created, 1)
}

func PutSecret(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	masterKey := ctx.Get("app_key").(encryption.MasterKeyPair)

	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}

	existing, err := secretSelect(ctx, secretID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}

	patch := &SamnaSecret{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	if patch.HasKey("key") {
		existing.Key = patch.Key
	}
	if patch.HasKey("value") {
		encrypted, encErr := utils.AesEncrypt(patch.Value, masterKey.Seed)
		if encErr != nil {
			ctx.Logger().Errorf("PutSecret encrypt: %v", encErr)
			return respond.Error(ctx, http.StatusInternalServerError, "secret encryption failed")
		}
		existing.Value = hex.EncodeToString(encrypted)
	}
	if patch.HasKey("expires_at") {
		existing.ExpiresAt = patch.ExpiresAt
	}

	if err := secretUpdate(ctx, existing, secretID, appID); err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusOK, existing, 1)
}

func DeleteSecret(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	secret, err := secretDelete(ctx, secretID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, secret, 1)
}

func secretSelectAll(ctx echo.Context, appID uuid.UUID) ([]SamnaSecret, error) {
	secrets := make([]SamnaSecret, 0)
	err := prophetdb.Instance().NewSelect().
		Model(&secrets).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("secretSelectAll: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return secrets, nil
}

func secretSelect(ctx echo.Context, secretID, appID uuid.UUID) (*SamnaSecret, error) {
	var secret SamnaSecret
	err := prophetdb.Instance().NewSelect().
		Model(&secret).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", secretID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("secretSelect: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return &secret, nil
}

func secretInsert(ctx echo.Context, secret *SamnaSecret) (*SamnaSecret, error) {
	if _, err := prophetdb.Instance().NewInsert().
		Model(secret).
		Returning("*").
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("secretInsert: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return secret, nil
}

func secretUpdate(ctx echo.Context, secret *SamnaSecret, secretID, appID uuid.UUID) error {
	res, err := prophetdb.Instance().NewUpdate().
		Model(secret).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", secretID).
		Where("app_id = ?", appID).
		OmitZero().
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("secretUpdate: %v", err)
		return errorsx.ErrDbUnexpected
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return errorsx.ErrNotFound
	}
	return nil
}

func secretDelete(ctx echo.Context, secretID, appID uuid.UUID) (*SamnaSecret, error) {
	var secret SamnaSecret
	res, err := prophetdb.Instance().
		NewUpdate().
		Model(&secret).
		Where("id = ?", secretID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("secretDelete: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return nil, errorsx.ErrNotFound
	}
	return &secret, nil
}
