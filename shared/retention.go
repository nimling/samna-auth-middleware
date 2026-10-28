package shared

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	RetentionLockKey  = "claimius.retention"
	retentionInterval = 24 * time.Hour
)

var ErrBadRetention = errors.New("bad retention request")

type Retention struct {
	bun.BaseModel `bun:"table:claimius.retention" json:"-"`
	Kind          string     `bun:"kind,pk" json:"kind"`
	Class         string     `bun:"class" json:"class"`
	Days          int        `bun:"days" json:"days"`
	Prune         string     `bun:"prune" json:"prune"`
	Pruned        *int64     `bun:"pruned" json:"pruned"`
	PrunedAt      *time.Time `bun:"pruned_at" json:"pruned_at"`
	UpdatedAt     time.Time  `bun:"updated_at" json:"updated_at"`
}

type retentionWritten struct {
	Retention *Retention `json:"retention"`
}

type BodyRetention struct {
	Days int `json:"days"`
}

func (d *DB) GetRetentions(ctx context.Context) ([]Retention, error) {
	rows := make([]Retention, 0)
	if err := d.NewRaw("SELECT * FROM claimius.get_retention()").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (d *DB) PutRetention(ctx context.Context, kind string, days int) (*Retention, error) {
	var raw string
	if err := d.NewRaw("SELECT claimius.update_retention(?, ?)", kind, days).Scan(ctx, &raw); err != nil {
		return nil, retentionError(err)
	}
	var wrapper retentionWritten
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Retention, nil
}

func (d *DB) DeleteExpired(ctx context.Context, kind string) (int64, error) {
	var deleted int64
	err := d.NewRaw("SELECT claimius.delete_expired(?)", kind).Scan(ctx, &deleted)
	return deleted, err
}

func (d *DB) DeleteExpiredLoop(ctx context.Context) {
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		kinds := make([]string, 0)
		if err := d.NewSelect().Model((*Retention)(nil)).Column("kind").Order("kind").Scan(ctx, &kinds); err != nil && ctx.Err() == nil {
			d.Logger.Errorf("retention: read kinds: %v", err)
		}
		for _, kind := range kinds {
			deleted, err := d.DeleteExpired(ctx, kind)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				d.Logger.Errorf("retention %s: %v", kind, err)
				continue
			}
			if deleted > 0 {
				d.Logger.Infof("retention %s: %d rows deleted", kind, deleted)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func retentionError(err error) error {
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		switch pgErr.Field('C') {
		case "P0404":
			return fmt.Errorf("%w: %w", ErrNotFound, err)
		case "22023":
			return fmt.Errorf("%w: %s", ErrBadRetention, pgErr.Field('M'))
		}
	}
	return err
}
