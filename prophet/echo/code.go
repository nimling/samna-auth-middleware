package echo

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func respondCodeError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, shared.ErrCodeInvalid), errors.Is(err, shared.ErrNotFound):
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	case errors.Is(err, shared.ErrNoAccess):
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	case errors.Is(err, shared.ErrBadCode):
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	ctx.Logger().Errorf("code: %v", err)
	return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
}

func GetCodes(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.QueryParam("claim_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id is required")
	}
	codes, err := sdb.GetCodes(ctx.Request().Context(), appID, claimID)
	if err != nil {
		return respondCodeError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, codes, len(codes))
}

func PostCode(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	var body shared.BodySamCode
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	if body.ClaimID == uuid.Nil || body.UserID == uuid.Nil || body.Hash == "" {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id, user_id and hash are required")
	}
	raw, err := sdb.PostCode(ctx.Request().Context(), appID, body)
	if err != nil {
		return respondCodeError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusCreated, raw, 1)
}

func DeleteCode(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	codeID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	var body shared.BodySamCodeActor
	if err := ctx.Bind(&body); err != nil || body.UserID == uuid.Nil {
		return respond.Error(ctx, http.StatusBadRequest, "user_id is required")
	}
	raw, err := sdb.DeleteCode(ctx.Request().Context(), appID, body.UserID, codeID)
	if err != nil {
		return respondCodeError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, raw, 1)
}

func PutCodeUsed(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	var body shared.BodySamCodeHash
	if err := ctx.Bind(&body); err != nil || body.Hash == "" {
		return respond.Error(ctx, http.StatusBadRequest, "hash is required")
	}
	raw, err := sdb.PutCodeUsed(ctx.Request().Context(), appID, body.Hash)
	if err != nil {
		return respondCodeError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, raw, 1)
}

func PostCodeRedeem(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	var body shared.BodySamCodeHash
	if err := ctx.Bind(&body); err != nil || body.Hash == "" || body.UserID == uuid.Nil {
		return respond.Error(ctx, http.StatusBadRequest, "hash and user_id are required")
	}
	raw, err := sdb.PostCodeRedeem(ctx.Request().Context(), appID, body.Hash, body.UserID)
	if err != nil {
		return respondCodeError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, raw, 1)
}

func PutClaimGuest(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	claimID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	var body shared.BodySamClaimGuest
	if err := ctx.Bind(&body); err != nil || body.UserID == uuid.Nil {
		return respond.Error(ctx, http.StatusBadRequest, "user_id is required")
	}
	raw, err := sdb.PutClaimGuest(ctx.Request().Context(), appID, body.UserID, claimID, body.Mode)
	if err != nil {
		return respondCodeError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, raw, 1)
}
