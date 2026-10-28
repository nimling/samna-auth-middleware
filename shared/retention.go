package shared

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	RetentionLockKey  = "claimius.retention"
	retentionInterval = 24 * time.Hour
	retentionBatch    = 5000
)

var (
	ErrBadRetention  = errors.New("bad retention request")
	ErrNoColdStorage = errors.New("no cold storage configured")
)

type ColdStorage interface {
	PutObject(ctx context.Context, path string, body []byte) error
}

type Retention struct {
	bun.BaseModel `bun:"table:claimius.retention" json:"-"`
	Kind          string     `bun:"kind,pk" json:"kind"`
	Class         string     `bun:"class" json:"class"`
	Days          int        `bun:"days" json:"days"`
	Prune         string     `bun:"prune" json:"prune"`
	Export        *string    `bun:"export" json:"export"`
	Pruned        *int64     `bun:"pruned" json:"pruned"`
	PrunedAt      *time.Time `bun:"pruned_at" json:"pruned_at"`
	UpdatedAt     time.Time  `bun:"updated_at" json:"updated_at"`
}

type retentionWritten struct {
	Retention *Retention `json:"retention"`
}

type retentionExpired struct {
	Key  string          `bun:"key"`
	Data json.RawMessage `bun:"data,type:jsonb"`
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

func (d *DB) DeleteExported(ctx context.Context, kind string) (int64, error) {
	if d.ColdStorage == nil {
		return 0, ErrNoColdStorage
	}
	var deleted int64
	after := ""
	for {
		batch := make([]retentionExpired, 0, retentionBatch)
		if err := d.NewRaw("SELECT key, data FROM claimius.get_expired(?, ?, ?)", kind, after, retentionBatch).Scan(ctx, &batch); err != nil {
			return deleted, err
		}
		if len(batch) == 0 {
			return deleted, nil
		}
		var body bytes.Buffer
		zw := gzip.NewWriter(&body)
		keys := make([]string, len(batch))
		for i := range batch {
			keys[i] = batch[i].Key
			if _, err := zw.Write(batch[i].Data); err != nil {
				return deleted, err
			}
			if _, err := zw.Write([]byte{'\n'}); err != nil {
				return deleted, err
			}
		}
		if err := zw.Close(); err != nil {
			return deleted, err
		}
		sum := sha256.Sum256(body.Bytes())
		now := time.Now().UTC()
		path := fmt.Sprintf("%s/%s/%04d/%02d/%s-%s.ndjson.gz", utils.Cfg().AppSlug, kind, now.Year(), int(now.Month()), keys[0], keys[len(keys)-1])
		if err := d.ColdStorage.PutObject(ctx, path, body.Bytes()); err != nil {
			return deleted, fmt.Errorf("export %s: %w", path, err)
		}
		var n int64
		if err := d.NewRaw("SELECT claimius.delete_exported(?, ?, ?, ?, ?)", kind, path, hex.EncodeToString(sum[:]), pgdialect.Array(keys), after == "").Scan(ctx, &n); err != nil {
			return deleted, err
		}
		deleted += n
		after = keys[len(keys)-1]
		if len(batch) < retentionBatch {
			return deleted, nil
		}
	}
}

func (d *DB) DeleteExpiredLoop(ctx context.Context) {
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		rows, err := d.GetRetentions(ctx)
		if err != nil && ctx.Err() == nil {
			d.Logger.Errorf("retention: read kinds: %v", err)
		}
		kept := make([]string, 0)
		for _, row := range rows {
			if row.Export != nil && d.ColdStorage == nil {
				kept = append(kept, row.Kind)
				continue
			}
			var deleted int64
			if row.Export != nil {
				deleted, err = d.DeleteExported(ctx, row.Kind)
			} else {
				deleted, err = d.DeleteExpired(ctx, row.Kind)
			}
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				d.Logger.Errorf("retention %s: %v", row.Kind, err)
				continue
			}
			if deleted > 0 {
				d.Logger.Infof("retention %s: %d rows deleted", row.Kind, deleted)
			}
		}
		if len(kept) > 0 {
			d.Logger.Warnf("retention: %v, %s kept and not deleted", ErrNoColdStorage, strings.Join(kept, ", "))
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
