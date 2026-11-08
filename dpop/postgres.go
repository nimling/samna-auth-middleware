package dpop

import (
	"context"
	"math/rand"
	"time"

	"github.com/uptrace/bun"
)

type dpopJTI struct {
	bun.BaseModel `bun:"table:claimius.dpop_jti"`
	JTI           string    `bun:"jti,pk"`
	ExpiresAt     time.Time `bun:"expires_at"`
}

type PostgresReplayStore struct {
	DB bun.IDB
}

func (s PostgresReplayStore) CheckAndStore(ctx context.Context, jti string, ttl time.Duration) (bool, error) {
	res, err := s.DB.NewInsert().
		Model(&dpopJTI{JTI: jti, ExpiresAt: time.Now().Add(ttl)}).
		On("CONFLICT (jti) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return false, err
	}

	if rand.Intn(100) == 0 {
		_, _ = s.DB.NewDelete().Model((*dpopJTI)(nil)).Where("expires_at < now()").Exec(ctx)
	}

	n, _ := res.RowsAffected()
	return n > 0, nil
}
