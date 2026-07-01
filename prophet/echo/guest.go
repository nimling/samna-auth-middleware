package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
)

type BodyGuest struct {
	ClaimID uuid.UUID `json:"claim_id"`
}

func PostGuestToken(ctx echo.Context) error {
	var body BodyGuest
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	if body.ClaimID == uuid.Nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id is required")
	}

	token, err := sdb.IssueGuestToken(ctx.Request().Context(), body.ClaimID, "")
	if err != nil {
		ctx.Logger().Errorf("PostGuestToken: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusOK, token, 1)
}
