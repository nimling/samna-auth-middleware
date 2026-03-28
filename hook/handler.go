package hook

import (
	"context"

	"github.com/nimling/samna-auth-middleware/db"
)

func SyncOrganization(ctx context.Context, org *db.Organization) error {
	return upsert(ctx, org, string(db.OBJECT_TYPE_ORGANIZATION))
}

func SyncLocation(ctx context.Context, loc *db.Location) error {
	return upsert(ctx, loc, string(db.OBJECT_TYPE_LOCATION))
}

func SyncSamnaUser(ctx context.Context, user *db.SamnaUser) error {
	return upsert(ctx, user, string(db.OBJECT_TYPE_USER))
}

func SyncClaim(ctx context.Context, claim *db.Claim) error {
	return upsert(ctx, claim, string(db.OBJECT_TYPE_CLAIM))
}

func SyncUserClaim(ctx context.Context, uc *db.UserClaim) error {
	return upsert(ctx, uc, string(db.OBJECT_TYPE_USER_CLAIM))
}

func SyncClaimObject(ctx context.Context, co *db.ClaimObject) error {
	return upsert(ctx, co, string(db.OBJECT_TYPE_CLAIM_OBJECT))
}

func upsert[T any](ctx context.Context, entity *T, objectType string) error {
	_, err := db.Instance().NewInsert().
		Model(entity).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	return err
}
