package echo

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/utils"
)

type BodyCode struct {
	ClaimID   *uuid.UUID `json:"claim_id"`
	UserID    *uuid.UUID `json:"user_id"`
	Count     *int       `json:"count"`
	StartsAt  *time.Time `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	OwnerID   *uuid.UUID `json:"owner_id"`
}

func GetCodes(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	codes, err := codeGetAll(ctx, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusOK, codes, len(codes))
}

func GetCode(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	param := ctx.Param("code")

	id, uuidErr := uuid.Parse(param)
	if uuidErr == nil {
		c, err := codeGet(ctx, id, appID)
		if err != nil {
			return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
		}
		return respond.JSON(ctx, http.StatusOK, c, 1)
	}

	c, err := codeGetValue(ctx, param, appID)
	if err != nil {
		return respond.Redirect(ctx, http.StatusTemporaryRedirect, "/login")
	}

	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if ok {
		if err := codeRedeem(ctx, c, userID); err != nil {
			ctx.Logger().Errorf("GetCode redeem: %v", err)
			return respond.Redirect(ctx, http.StatusTemporaryRedirect, "/")
		}
		return respond.Redirect(ctx, http.StatusTemporaryRedirect, "/")
	}

	return respond.Redirect(ctx, http.StatusTemporaryRedirect, "/login")
}

func PostCode(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)

	var req BodyCode
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}

	if req.OwnerID == nil {
		return respond.Error(ctx, http.StatusBadRequest, "owner_id required")
	}

	if req.UserID != nil && req.ClaimID != nil {
		return respond.Error(ctx, http.StatusBadRequest, "code must carry either user_id or claim_id, not both")
	}

	c := Code{
		AppID:     appID,
		ClaimID:   req.ClaimID,
		UserID:    req.UserID,
		StartsAt:  req.StartsAt,
		EndsAt:    req.EndsAt,
		ExpiresAt: req.ExpiresAt,
		OwnerID:   *req.OwnerID,
	}

	if req.Count != nil {
		c.Count = *req.Count
	}

	created, err := codePost(ctx, &c)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusCreated, created, 1)
}

func PutCode(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}

	existing, err := codeGet(ctx, id, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}

	var req BodyCode
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}

	if req.ClaimID != nil {
		existing.ClaimID = req.ClaimID
	}
	if req.UserID != nil {
		existing.UserID = req.UserID
	}
	if req.StartsAt != nil {
		existing.StartsAt = req.StartsAt
	}
	if req.EndsAt != nil {
		existing.EndsAt = req.EndsAt
	}
	if req.ExpiresAt != nil {
		existing.ExpiresAt = req.ExpiresAt
	}
	if req.OwnerID != nil {
		existing.OwnerID = *req.OwnerID
	}
	if req.Count != nil {
		existing.Count = *req.Count
	}

	if err := codePut(ctx, existing, appID); err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, existing, 1)
}

func DeleteCode(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}
	deleted, err := codeDelete(ctx, id, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, deleted, 1)
}

func codeGet(ctx echo.Context, id, appID uuid.UUID) (*Code, error) {
	var c Code
	err := sdb.Instance().
		NewSelect().
		Model(&c).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", id).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("codeGet: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return &c, nil
}

func codeGetAll(ctx echo.Context, appID uuid.UUID) ([]Code, error) {
	codes := make([]Code, 0)
	err := sdb.Instance().
		NewSelect().
		Model(&codes).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		OrderExpr("sa_created_at DESC").
		Scan(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("codeGetAll: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return codes, nil
}

func codeGetValue(ctx echo.Context, value string, appID uuid.UUID) (*Code, error) {
	var c Code
	err := sdb.Instance().
		NewSelect().
		Model(&c).
		ExcludeColumn("sa_deleted_at").
		Where("code = ?", value).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Where("(count = 0 OR redeemed_count < count)").
		Limit(1).
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("codeGetValue: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	now := time.Now()
	if c.ExpiresAt != nil && now.After(*c.ExpiresAt) {
		return nil, errorsx.ErrNotFound
	}
	if c.StartsAt != nil && now.Before(*c.StartsAt) {
		return nil, errorsx.ErrNotFound
	}
	if c.EndsAt != nil && now.After(*c.EndsAt) {
		return nil, errorsx.ErrNotFound
	}

	return &c, nil
}

func codePost(ctx echo.Context, c *Code) (*Code, error) {
	if _, err := sdb.Instance().
		NewInsert().
		Model(c).
		Returning("*").
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("codePost: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	return c, nil
}

func codePut(ctx echo.Context, c *Code, appID uuid.UUID) error {
	res, err := sdb.Instance().
		NewUpdate().
		Model(c).
		Where("id = ?", c.ID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("codePut: %v", err)
		return errorsx.ErrDbUnexpected
	}

	affected, err := res.RowsAffected()
	if err != nil {
		ctx.Logger().Errorf("codePut: %v", err)
		return errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return errorsx.ErrNotFound
	}

	return nil
}

func codeDelete(ctx echo.Context, id, appID uuid.UUID) (*Code, error) {
	var c Code
	res, err := sdb.Instance().
		NewUpdate().
		Model(&c).
		Set("sa_deleted_at = ?", time.Now().UTC()).
		Where("id = ?", id).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("codeDelete: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}

	affected, err := res.RowsAffected()
	if err != nil {
		ctx.Logger().Errorf("codeDelete: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return nil, errorsx.ErrNotFound
	}

	return &c, nil
}

func codeRedeem(ctx echo.Context, c *Code, userID uuid.UUID) error {
	tx, err := sdb.Instance().BeginTx(ctx.Request().Context(), nil)
	if err != nil {
		ctx.Logger().Errorf("codeRedeem begin tx: %v", err)
		return errorsx.ErrDbUnexpected
	}

	if c.ClaimID != nil && c.UserID == nil {
		ownerID := c.OwnerID
		if ownerID == uuid.Nil {
			var claim Claim
			err = sdb.Instance().
				NewSelect().
				Model(&claim).
				Where("sa_deleted_at IS NULL").
				ExcludeColumn("sa_deleted_at").
				Where("id = ?", *c.ClaimID).
				Column("sa_owner_id").
				Scan(ctx.Request().Context())
			if err != nil {
				_ = tx.Rollback()
				ctx.Logger().Errorf("codeRedeem get claim owner: %v", err)
				return errorsx.ErrDbUnexpected
			}
			ownerID = claim.OwnerID
		}

		if _, err = tx.NewRaw(
			"SELECT claimius.assign_claim_user(?, ?, ?, ?, ?, NULL, ?, ?)",
			c.AppID, *c.ClaimID, userID, ownerID, c.CreatedBy,
			c.StartsAt, c.EndsAt,
		).Exec(ctx.Request().Context()); err != nil {
			_ = tx.Rollback()
			ctx.Logger().Errorf("codeRedeem assign claim: %v", err)
			return errorsx.ErrDbUnexpected
		}
	}

	updateQ := tx.NewUpdate().
		Model((*Code)(nil)).
		Where("sa_deleted_at IS NULL").
		Set("redeemed_at = ?", time.Now().UTC()).
		Set("redeemed_count = redeemed_count + 1").
		Where("id = ?", c.ID)

	if _, err = updateQ.Exec(ctx.Request().Context()); err != nil {
		_ = tx.Rollback()
		ctx.Logger().Errorf("codeRedeem update: %v", err)
		return errorsx.ErrDbUnexpected
	}

	if err = tx.Commit(); err != nil {
		ctx.Logger().Errorf("codeRedeem commit: %v", err)
		return errorsx.ErrDbUnexpected
	}

	return nil
}
