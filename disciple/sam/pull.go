package sam

import (
	"context"
	"errors"
	"fmt"

	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/shared"
)

// ErrSnapshotRequired is the prophet's answer to a pull at a cursor its feed
// cannot serve: above every position it issued, below events it trimmed, or
// behind by more events than the app has source rows.
var ErrSnapshotRequired = errors.New("sync cursor refused by the prophet feed, snapshot required")

type SnapshotResponse struct {
	Records []SyncPayload `json:"records"`
	client.SyncCursor
}

// PullSnapshot replaces the replica with the prophet's snapshot. reconcile,
// when set, is audited in the snapshot's transaction.
func PullSnapshot(ctx context.Context, reconcile string) error {
	state, err := GetState(ctx)
	if err != nil {
		return fmt.Errorf("get current seq: %w", err)
	}

	var snapshot SnapshotResponse
	if err := client.Get(ctx, fmt.Sprintf("sync/snapshot?consumer=%s", state.ID), &snapshot); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return shared.ErrSyncUnreachable
		}
		return fmt.Errorf("fetch snapshot: %w", err)
	}

	return ApplyPage(ctx, state.Cursor(), snapshot.SyncCursor, snapshot.Records, reconcile)
}

func PullSync(ctx context.Context) (int, error) {
	state, err := GetState(ctx)
	if err != nil {
		return 0, fmt.Errorf("get current seq: %w", err)
	}
	from := state.Cursor()

	payloads, err := GetSyncPage(ctx, state.ID.String(), from)
	if err != nil {
		return 0, err
	}
	if len(payloads) == 0 {
		return 0, nil
	}

	to := from
	for _, payload := range payloads {
		if to.Less(payload.SyncCursor) {
			to = payload.SyncCursor
		}
	}

	if err := ApplyPage(ctx, from, to, payloads, ""); err != nil {
		return 0, err
	}

	return len(payloads), nil
}

// GetSyncPage fetches the page after the cursor for consumer. The prophet
// holds an empty answer open up to syncHold and answers as soon as an event
// commits, so the request timeout covers the hold.
func GetSyncPage(ctx context.Context, consumer string, cursor client.SyncCursor) ([]SyncPayload, error) {
	ctx = client.WithTimeout(ctx, syncHold+syncHoldMargin)
	path := fmt.Sprintf("sync/%s?wait=%d&consumer=%s", cursor, int(syncHold.Seconds()), consumer)
	var payloads []SyncPayload
	if err := client.Get(ctx, path, &payloads); err != nil {
		switch {
		case errors.Is(err, client.ErrNotFound):
			return nil, shared.ErrSyncUnreachable
		case errors.Is(err, client.ErrConflict):
			return nil, ErrSnapshotRequired
		}
		return nil, fmt.Errorf("fetch sync: %w", err)
	}
	return payloads, nil
}
