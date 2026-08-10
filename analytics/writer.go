package analytics

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/shared"
)

// PostSession opens or touches the session row for a visit and returns whether
// the visitor granted the analytics category. It runs on the request path
// because the recorder needs the answer before it starts capturing.
func PostSession(ctx context.Context, appID uuid.UUID, anonymousID, sessionKey string, context_ json.RawMessage, userID *uuid.UUID, ip, userAgent string) (bool, error) {
	if run == nil {
		return false, nil
	}
	body := context_
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}
	var result json.RawMessage
	if err := run.db.NewRaw(
		"SELECT claimius.post_analytics_session(?, ?, ?, ?::jsonb, ?, ?::inet, ?)",
		appID, anonymousID, sessionKey, body, userID, nullString(ip), nullString(userAgent),
	).Scan(ctx, &result); err != nil {
		return false, err
	}
	var wrapper struct {
		Session *shared.AnalyticsSession `json:"analytics_session"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		return false, err
	}
	return wrapper.Session != nil, nil
}

// Post queues a batch against an open session. It returns immediately. A full
// queue drops the batch rather than blocking the caller.
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

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
