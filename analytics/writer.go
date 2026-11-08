package analytics

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func PostSession(ctx context.Context, appID uuid.UUID, anonymousID, sessionKey string, context_ json.RawMessage, userID *uuid.UUID, ip, userAgent string) (bool, error) {
	if run == nil {
		return false, nil
	}
	body := context_
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}
	var result string
	if err := run.db.NewRaw(
		"SELECT claimius.post_analytics_session(?, ?, ?, ?::jsonb, ?, ?::inet, ?)",
		appID, anonymousID, sessionKey, body, userID, nullString(ip), nullString(userAgent),
	).Scan(ctx, &result); err != nil {
		return false, err
	}
	var wrapper struct {
		Session *shared.AnalyticsSession `json:"analytics_session"`
	}
	if err := json.Unmarshal([]byte(result), &wrapper); err != nil {
		return false, err
	}
	return wrapper.Session != nil, nil
}

func Post(appID uuid.UUID, sessionKey string, events []shared.AnalyticsEventInput) {
	if run == nil || len(events) == 0 {
		return
	}
	select {
	case run.jobs <- job{appID: appID, sessionKey: sessionKey, events: events}:
	default:
		run.logger.Errorf("analytics: job queue full, dropped %d events", len(events))
	}
}

func (r *runner) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopChan:
			return
		case j := <-r.jobs:
			r.exec(ctx, j)
		}
	}
}

func (r *runner) exec(ctx context.Context, j job) {
	body, err := json.Marshal(j.events)
	if err != nil {
		r.logger.Errorf("analytics exec marshal: %v", err)
		return
	}
	var written int
	if err := r.db.NewRaw(
		"SELECT claimius.post_analytics_events(?, ?, ?::jsonb)",
		j.appID, j.sessionKey, json.RawMessage(body),
	).Scan(ctx, &written); err != nil {
		r.logger.Errorf("analytics exec: %v", err)
	}
}

// foldLoop keeps the heat grid current and the raw tier bounded. A pointer is
// sampled every 100ms, so the raw table grows far faster than anything reads
// it, while the field a reader wants is the same field a coarse grid holds.
func (r *runner) foldLoop(ctx context.Context) {
	ticker := time.NewTicker(foldInterval)
	defer ticker.Stop()
	r.fold(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopChan:
			return
		case <-ticker.C:
			r.fold(ctx)
		}
	}
}

// fold refolds yesterday and today, then drops raw beyond the retention
// window. Today is refolded on every tick because a cell is replaced rather
// than added to, so folding the whole of the day so far is always right.
func (r *runner) fold(ctx context.Context) {
	now := time.Now().UTC()

	var cells int64
	if err := r.db.NewRaw(
		"SELECT claimius.post_analytics_heat(?::date, ?::date)",
		day(now.AddDate(0, 0, -1)), day(now),
	).Scan(ctx, &cells); err != nil {
		r.logger.Errorf("analytics fold: %v", err)
		return
	}

	var dropped int64
	if err := r.db.NewRaw(
		"SELECT claimius.delete_analytics_event(?::date)",
		day(now.AddDate(0, 0, -rawRetentionDays)),
	).Scan(ctx, &dropped); err != nil {
		r.logger.Errorf("analytics expire: %v", err)
		return
	}
	if cells > 0 || dropped > 0 {
		r.logger.Infof("analytics fold: %d cells written, %d raw rows dropped", cells, dropped)
	}
}

func day(t time.Time) string {
	return t.Format("2006-01-02")
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
