package shared

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

const RetentionLockKey = "claimius.retention"

type Retention map[string]int

func RetentionEnv() Retention {
	r := Retention{}
	if v := os.Getenv("SAUTH_RETENTION"); v != "" {
		json.Unmarshal([]byte(v), &r)
	}
	return r
}

func (d *DB) Prune(ctx context.Context, r Retention) (int64, error) {
	settings, err := json.Marshal(r)
	if err != nil {
		return 0, err
	}
	var n int64
	err = d.NewRaw("SELECT claimius.prune(?::jsonb)", string(settings)).Scan(ctx, &n)
	return n, err
}

func (d *DB) PruneLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		r := RetentionEnv()
		if d.Retention != nil {
			for key, v := range d.Retention(ctx) {
				r[key] = v
			}
		}
		n, err := d.Prune(ctx, r)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			d.Logger.Errorf("retention: %v", err)
		} else if n > 0 {
			d.Logger.Infof("retention: %d rows deleted", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
