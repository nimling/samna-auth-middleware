package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/shared"
)

type SnapshotResponse struct {
	Records []SyncPayload `json:"records"`
	Seq     int64         `json:"seq"`
}

func PullSnapshot(ctx context.Context) error {
	var snapshot SnapshotResponse
	if err := client.Get(ctx, "sync/snapshot", &snapshot); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return shared.ErrSyncUnreachable
		}
		return fmt.Errorf("fetch snapshot: %w", err)
	}

	for _, payload := range snapshot.Records {
		if err := ApplyPayload(ctx, payload); err != nil {
			return fmt.Errorf("apply %s: %w", payload.EventType, err)
		}
	}

	if snapshot.Seq > 0 {
		if err := UpdateSeq(ctx, snapshot.Seq); err != nil {
			return fmt.Errorf("update seq: %w", err)
		}
	}

	return nil
}

func PullSync(ctx context.Context) error {
	seq, err := GetSeq(ctx)
	if err != nil {
		return fmt.Errorf("get current seq: %w", err)
	}

	var payloads []SyncPayload
	if err := client.Get(ctx, fmt.Sprintf("sync/%d", seq), &payloads); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return shared.ErrSyncUnreachable
		}
		return fmt.Errorf("fetch sync: %w", err)
	}

	var maxSeq int64
	for _, payload := range payloads {
		if err := ApplyPayload(ctx, payload); err != nil {
			return fmt.Errorf("apply %s seq %d: %w", payload.EventType, payload.Seq, err)
		}
		if payload.Seq > maxSeq {
			maxSeq = payload.Seq
		}
	}

	if maxSeq > 0 {
		if err := UpdateSeq(ctx, maxSeq); err != nil {
			return fmt.Errorf("update seq: %w", err)
		}
	}

	return nil
}
