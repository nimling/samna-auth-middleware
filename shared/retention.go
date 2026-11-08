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

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	RetentionLockKey  = "claimius.retention"
	ColdExportPrefix  = "export/"
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

type ColdSink struct {
	Storage ColdStorage
	Prefix  string
}

type ColdStore interface {
	GetColdSink(ctx context.Context, rootID uuid.UUID) (ColdSink, error)
}

type SharedColdStorage struct {
	Storage ColdStorage
}

func (s SharedColdStorage) GetColdSink(_ context.Context, _ uuid.UUID) (ColdSink, error) {
	return ColdSink{Storage: s.Storage}, nil
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
	Key    string          `bun:"key"`
	Data   json.RawMessage `bun:"data,type:jsonb"`
	RootID string          `bun:"root_id"`
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
	if d.ColdStore == nil {
		return 0, ErrNoColdStorage
	}
	var deleted int64
	sinks := map[string]ColdSink{}
	skipped := map[string]error{}
	defer func() {
		for root, err := range skipped {
			d.Logger.Warnf("retention %s: rows of root %s kept, cold storage not resolved: %v", kind, root, err)
		}
	}()
	first := true
	after := ""
	for {
		batch := make([]retentionExpired, 0, retentionBatch)
		if err := d.NewRaw("SELECT key, data, coalesce(data->>'sa_root_id', '') AS root_id FROM claimius.get_expired(?, ?, ?)", kind, after, retentionBatch).Scan(ctx, &batch); err != nil {
			return deleted, err
		}
		if len(batch) == 0 {
			return deleted, nil
		}
		roots := make([]string, 0)
		groups := map[string][]retentionExpired{}
		for _, row := range batch {
			if _, ok := groups[row.RootID]; !ok {
				roots = append(roots, row.RootID)
			}
			groups[row.RootID] = append(groups[row.RootID], row)
		}
		for _, root := range roots {
			if _, failed := skipped[root]; failed {
				continue
			}
			sink, ok := sinks[root]
			if !ok {
				var err error
				if root == "" {
					sink = ColdSink{Storage: d.ColdStorage}
				} else if id, parseErr := uuid.Parse(root); parseErr != nil {
					err = parseErr
				} else {
					sink, err = d.ColdStore.GetColdSink(ctx, id)
				}
				if err == nil && sink.Storage == nil {
					err = ErrNoColdStorage
				}
				if err != nil {
					skipped[root] = err
					continue
				}
				sinks[root] = sink
			}
			n, err := d.putColdObject(ctx, kind, root, sink, groups[root], first)
			if err != nil {
				return deleted, err
			}
			deleted += n
			first = false
		}
		after = batch[len(batch)-1].Key
		if len(batch) < retentionBatch {
			return deleted, nil
		}
	}
}

func (d *DB) putColdObject(ctx context.Context, kind, root string, sink ColdSink, rows []retentionExpired, first bool) (int64, error) {
	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	keys := make([]string, len(rows))
	for i := range rows {
		keys[i] = rows[i].Key
		if _, err := zw.Write(rows[i].Data); err != nil {
			return 0, err
		}
		if _, err := zw.Write([]byte{'\n'}); err != nil {
			return 0, err
		}
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	sum := sha256.Sum256(body.Bytes())
	now := time.Now().UTC()
	var rootID any
	folder := "none"
	if root != "" {
		rootID = root
		folder = root
	}
	path := fmt.Sprintf("%s%s/%s/%s/%04d/%02d/%s-%s.ndjson.gz", sink.Prefix, utils.Cfg().AppSlug, folder, kind, now.Year(), int(now.Month()), keys[0], keys[len(keys)-1])
	if err := sink.Storage.PutObject(ctx, path, body.Bytes()); err != nil {
		return 0, fmt.Errorf("export %s: %w", path, err)
	}
	var n int64
	if err := d.NewRaw("SELECT claimius.delete_exported(?, ?, ?, ?, ?, ?)", kind, rootID, path, hex.EncodeToString(sum[:]), pgdialect.Array(keys), first).Scan(ctx, &n); err != nil {
		return 0, err
	}
	return n, nil
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
			if row.Export != nil && d.ColdStore == nil {
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
