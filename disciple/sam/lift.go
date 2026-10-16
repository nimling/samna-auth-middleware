package sam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/sauth"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

type LiftRequest struct {
	Records []SyncPayload `json:"records"`
}

type LiftCollision struct {
	EventType string    `json:"event_type"`
	ID        uuid.UUID `json:"id"`
}

type LiftResult struct {
	Lifted     map[string]int  `json:"lifted"`
	Collisions []LiftCollision `json:"collisions,omitempty"`
	Message    string          `json:"message,omitempty"`
}

var ErrLiftDebug = errors.New("lift: a disciple in debug mode holds its own data and lifts nothing")

const liftTimeout = 30 * time.Minute

var liftTables = []struct {
	eventType string
	rows      func() any
	order     []string
}{
	{"claimius.organization", func() any { return &[]shared.Organization{} }, []string{"sa_level", "id"}},
	{"claimius.location", func() any { return &[]shared.Location{} }, []string{"sa_level", "id"}},
	{"claimius.samna_user", func() any { return &[]shared.SamnaUser{} }, []string{"sa_created_at", "id"}},
	{"claimius.samna_client", func() any { return &[]shared.SamnaClient{} }, []string{"sa_created_at", "id"}},
	{"claimius.samna_secret", func() any { return &[]shared.SamnaSecret{} }, []string{"sa_created_at", "id"}},
	{"claimius.claim", func() any { return &[]shared.Claim{} }, []string{"sa_created_at", "id"}},
	{"claimius.user_claim", func() any { return &[]shared.UserClaim{} }, []string{"sa_created_at", "id"}},
	{"claimius.claim_object", func() any { return &[]shared.ClaimObject{} }, []string{"sa_created_at", "id"}},
	{"claimius.user_token", func() any { return &[]shared.UserToken{} }, []string{"sa_created_at", "id"}},
	{"claimius.user_relation", func() any { return &[]shared.UserRelation{} }, []string{"sa_created_at", "id"}},
	{"claimius.object_field", func() any { return &[]shared.ObjectField{} }, []string{"sa_created_at", "id"}},
	{"claimius.user_field", func() any { return &[]shared.UserField{} }, []string{"sa_created_at", "id"}},
	{"claimius.object_asset", func() any { return &[]shared.ObjectAsset{} }, []string{"sa_created_at", "id"}},
}

func (r *Replica) Lift(ctx context.Context) (*LiftResult, error) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return nil, ErrLiftDebug
	}
	if r.Peer != nil {
		return nil, fmt.Errorf("lift: a peer mirror lifts nothing")
	}
	state, err := r.GetState(ctx)
	if err != nil {
		return nil, fmt.Errorf("lift: read state: %w", err)
	}
	records := make([]SyncPayload, 0)
	for _, t := range liftTables {
		rows := t.rows()
		if err := r.DB.NewSelect().Model(rows).Order(t.order...).Scan(ctx); err != nil {
			return nil, fmt.Errorf("lift: read %s: %w", t.eventType, err)
		}
		items, err := sauth.MarshalRows(rows)
		if err != nil {
			return nil, fmt.Errorf("lift: marshal %s: %w", t.eventType, err)
		}
		for _, item := range items {
			records = append(records, SyncPayload{Operation: "upsert", EventType: t.eventType, Payload: item})
		}
	}
	var seq client.SyncCursor
	var prophet string
	target := r.target(state, "")
	reqCtx := client.WithProphet(client.WithSyncSeq(client.WithTarget(client.WithTimeout(ctx, liftTimeout), target), &seq), &prophet)
	var result LiftResult
	if err := client.Post(reqCtx, "lift", LiftRequest{Records: records}, &result); err != nil {
		return nil, fmt.Errorf("lift: %w", err)
	}
	to := State{Cursor: seq, Prophet: prophet, Endpoint: target.Endpoint}
	if err := r.ApplyPage(ctx, state.Cursor, to, nil, "lift"); err != nil {
		return &result, fmt.Errorf("lift: move cursor to %s: %w", seq, err)
	}
	return &result, nil
}
