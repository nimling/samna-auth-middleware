package smw

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/nimtel"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/utils"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

func TelemetryMiddleware() echo.MiddlewareFunc {
	return otelecho.Middleware(utils.Cfg().AppSlug)
}

func StampOrganization(ctx echo.Context, rootID, ownerID uuid.UUID) {
	req := ctx.Request()
	ctx.SetRequest(req.WithContext(nimtel.Organization(req.Context(), rootID, ownerID)))
}

func stamped(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		stampActor(ctx)
		return next(ctx)
	}
}

func stampActor(ctx echo.Context) {
	userID, ok := ctx.Get(UserIDKey).(uuid.UUID)
	if !ok || sdb.Instance() == nil {
		return
	}
	var row struct {
		RootID  uuid.UUID `bun:"root_id"`
		OwnerID uuid.UUID `bun:"owner_id"`
	}
	err := sdb.Instance().NewSelect().
		TableExpr("claimius.user_claim AS uc").
		Join("JOIN claimius.claim AS c ON c.id = uc.claim_id").
		ColumnExpr("c.sa_root_id AS root_id, c.sa_owner_id AS owner_id").
		Where("uc.user_id = ?", userID).
		Where("uc.sa_deleted_at IS NULL").
		Where("c.sa_deleted_at IS NULL").
		Where("(uc.starts_at IS NULL OR uc.starts_at <= now())").
		Where("(uc.ends_at IS NULL OR uc.ends_at > now())").
		OrderExpr("uc.sa_created_at").
		Limit(1).
		Scan(ctx.Request().Context(), &row)
	if err != nil {
		return
	}
	StampOrganization(ctx, row.RootID, row.OwnerID)
}
