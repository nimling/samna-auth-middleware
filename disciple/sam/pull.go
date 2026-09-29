package sam

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
	seq, err := GetSeq(ctx)
	if err != nil {
		return fmt.Errorf("get current seq: %w", err)
	}

	var snapshot SnapshotResponse
	if err := client.Get(ctx, "sync/snapshot", &snapshot); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return shared.ErrSyncUnreachable
		}
		return fmt.Errorf("fetch snapshot: %w", err)
	}

	return ApplyPage(ctx, seq, snapshot.Seq, snapshot.Records)
}

func PullSync(ctx context.Context) (int, error) {
	seq, err := GetSeq(ctx)
	if err != nil {
		return 0, fmt.Errorf("get current seq: %w", err)
	}

	var payloads []SyncPayload
	if err := client.Get(ctx, fmt.Sprintf("sync/%d", seq), &payloads); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return 0, shared.ErrSyncUnreachable
		}
		return 0, fmt.Errorf("fetch sync: %w", err)
	}
	if len(payloads) == 0 {
		return 0, nil
	}

	maxSeq := seq
	for _, payload := range payloads {
		if payload.Seq > maxSeq {
			maxSeq = payload.Seq
		}
	}

	if err := ApplyPage(ctx, seq, maxSeq, payloads); err != nil {
		return 0, err
	}

	return len(payloads), nil
}
