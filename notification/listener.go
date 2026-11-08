package notification

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func (r *runner) listenLoop(ctx context.Context, notify <-chan *pq.Notification) {
	for n := range notify {
		if n == nil {
			continue
		}
		r.dispatch(ctx, n.Extra)
	}
}

func (r *runner) dispatch(ctx context.Context, payload string) {
	var msg struct {
		UserID uuid.UUID `json:"user_id"`
		ID     uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		r.logger.Errorf("notification dispatch decode: %v", err)
		return
	}
	if !r.hub.has(msg.UserID) {
		return
	}
	var n shared.Notification
	if err := r.db.NewSelect().
		Model(&n).
		Where("id = ?", msg.ID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx); err != nil {
		r.logger.Errorf("notification dispatch load: %v", err)
		return
	}
	data, err := json.Marshal(&n)
	if err != nil {
		r.logger.Errorf("notification dispatch marshal: %v", err)
		return
	}
	r.hub.push(msg.UserID, data)
}
