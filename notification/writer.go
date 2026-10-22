package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/gommon/log"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

type Notification struct {
	Title       string
	Description *string
	Style       json.RawMessage
	Type        string
	Link        *string
	AppID       *uuid.UUID
	CreatedBy   *uuid.UUID
	OwnerID     *uuid.UUID
	RepeatsAt   *string
}

type job struct {
	in         Notification
	objectType string
	objectID   *uuid.UUID
	userIDs    []uuid.UUID
}

func Write(in Notification, objectType string, objectID uuid.UUID) {
	qualified, err := shared.ResolveObjectType(objectType)
	if err != nil {
		log.Errorf("notification Write: %v", err)
		return
	}
	id := objectID
	enqueue(job{in: in, objectType: string(qualified), objectID: &id})
}

func WriteUser(in Notification, userIDs ...uuid.UUID) {
	if len(userIDs) == 0 {
		return
	}
	enqueue(job{in: in, userIDs: userIDs})
}

func enqueue(j job) {
	r := current()
	if r == nil {
		log.Errorf("notification %q dropped: %v", j.in.Title, ErrNotStarted)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		r.logger.Errorf("notification %q dropped: the runner has ended", j.in.Title)
		return
	}
	select {
	case r.jobs <- j:
	default:
		r.logger.Errorf("notification %q dropped: job queue full", j.in.Title)
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
	in := j.in
	style := in.Style
	if len(style) == 0 {
		style = json.RawMessage("{}")
	}
	var count int
	if len(j.userIDs) > 0 {
		placeholders := make([]string, len(j.userIDs))
		args := []interface{}{
			in.AppID, in.Title, in.Description, style, in.Type, in.Link,
			(*string)(nil), (*uuid.UUID)(nil), in.CreatedBy, in.OwnerID, in.RepeatsAt,
		}
		for i, u := range j.userIDs {
			placeholders[i] = "?"
			args = append(args, u)
		}
		query := fmt.Sprintf(
			"SELECT claimius.notify_user(?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?, ?::interval, ARRAY[%s]::uuid[])",
			strings.Join(placeholders, ", "),
		)
		if err := r.db.NewRaw(query, args...).Scan(ctx, &count); err != nil {
			r.logger.Errorf("notification WriteUser exec: %v", err)
		}
		return
	}
	if err := r.db.NewRaw(
		"SELECT claimius.notify_object(?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?, ?::interval)",
		in.AppID, in.Title, in.Description, style, in.Type, in.Link,
		j.objectType, j.objectID, in.CreatedBy, in.OwnerID, in.RepeatsAt,
	).Scan(ctx, &count); err != nil {
		r.logger.Errorf("notification Write exec: %v", err)
	}
}
