package analytics

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/gommon/log"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func PostSession(ctx context.Context, appID uuid.UUID, anonymousID, sessionKey string, context_ json.RawMessage, userID *uuid.UUID, ip, userAgent string) (bool, error) {
	r := current()
	if r == nil {
		return false, ErrNotStarted
	}
	body := context_
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}
	var result string
	if err := r.db.NewRaw(
		"SELECT claimius.upsert_analytics_session(?, ?, ?, ?::jsonb, ?, ?::inet, ?)",
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
	if len(events) == 0 {
		return
	}
	r := current()
	if r == nil {
		log.Errorf("analytics: %d events dropped: %v", len(events), ErrNotStarted)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		r.logger.Errorf("analytics: %d events dropped: the runner has ended", len(events))
		return
	}
	select {
	case r.jobs <- job{appID: appID, sessionKey: sessionKey, events: events}:
	default:
		r.logger.Errorf("analytics: job queue full, dropped %d events", len(events))
	}
}

func (r *runner) writeLoop(ctx context.Context) {
	work := context.WithoutCancel(ctx)
	for {
		select {
		case <-ctx.Done():
			r.drain(work)
			return
		case j := <-r.jobs:
			r.exec(work, j)
		}
	}
}

func (r *runner) drain(ctx context.Context) {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	drainCtx, cancel := context.WithTimeout(ctx, drainTimeout)
	defer cancel()
	for {
		select {
		case j := <-r.jobs:
			r.exec(drainCtx, j)
		default:
			return
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
		"SELECT claimius.insert_analytics_events(?, ?, ?::jsonb)",
		j.appID, j.sessionKey, json.RawMessage(body),
	).Scan(ctx, &written); err != nil {
		r.logger.Errorf("analytics exec: %v", err)
	}
}

func (r *runner) foldLoop(ctx context.Context) {
	ticker := time.NewTicker(foldInterval)
	defer ticker.Stop()
	r.fold(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.fold(ctx)
		}
	}
}

func (r *runner) fold(ctx context.Context) {
	now := time.Now().UTC()

	var cells int64
	if err := r.db.NewRaw(
		"SELECT claimius.upsert_analytics_heat(?::date, ?::date)",
		day(now.AddDate(0, 0, -1)), day(now),
	).Scan(ctx, &cells); err != nil {
		r.logger.Errorf("analytics fold: %v", err)
		return
	}
	if cells > 0 {
		r.logger.Infof("analytics fold: %d cells written", cells)
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
