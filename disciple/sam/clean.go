package sam

import (
	"context"
	"fmt"
	"time"

	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const (
	cleanInterval = 10 * time.Minute
	cleanGrace    = 10 * time.Minute
)

func (r *Replica) CleanOrphans(ctx context.Context) (int, error) {
	bindings := make([]shared.ClaimObject, 0)
	if err := r.DB.NewRaw("SELECT * FROM claimius.get_orphan_bindings(?::interval)", cleanGrace.String()).Scan(ctx, &bindings); err != nil {
		return 0, fmt.Errorf("orphan bindings: %w", err)
	}
	removed := 0
	for i := range bindings {
		if _, err := r.DB.NewDelete().Model(&bindings[i]).Exec(ctx); err != nil {
			return removed, fmt.Errorf("remove binding %s on %s: %w", bindings[i].ID, bindings[i].ClaimID, err)
		}
		removed++
	}
	claims := make([]shared.Claim, 0)
	if err := r.DB.NewRaw("SELECT * FROM claimius.get_orphan_claims(?::interval)", cleanGrace.String()).Scan(ctx, &claims); err != nil {
		return removed, fmt.Errorf("orphan claims: %w", err)
	}
	for i := range claims {
		if _, err := r.DB.NewDelete().Model(&claims[i]).Exec(ctx); err != nil {
			return removed, fmt.Errorf("remove claim %s: %w", claims[i].ID, err)
		}
		removed++
	}
	return removed, nil
}
