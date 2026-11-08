package sam

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

var ErrSnapshotRequired = errors.New("sync cursor refused by the prophet feed, snapshot required")

type SnapshotResponse struct {
	Records []SyncPayload `json:"records"`
	client.SyncCursor
}

func (r *Replica) target(state State, endpoint string) client.Target {
	if r.Peer != nil {
		return client.Target{Endpoint: r.Peer.Endpoint, Slug: r.Peer.AppSlug, Secret: r.Peer.AppSecret, PeerSecret: r.Peer.PeerSecret}
	}
	if endpoint == "" {
		endpoint = state.Endpoint
	}
	if endpoint == "" {
		endpoint = utils.Cfg().Endpoint
	}
	return client.Target{Endpoint: endpoint, Slug: utils.Cfg().AppSlug, Secret: utils.Cfg().AppSecret}
}

func (r *Replica) query(state State) url.Values {
	q := url.Values{}
	q.Set("consumer", state.ID.String())
	if state.Prophet != "" {
		q.Set("prophet", state.Prophet)
	}
	if r.Peer != nil {
		q.Set("peer", "true")
	}
	return q
}

func (r *Replica) checkProphet(prophet string) error {
	if r.Peer != nil && prophet != r.Peer.Prophet {
		return fmt.Errorf("peer %s at %s answered as prophet %q", r.Peer.Prophet, r.Peer.Endpoint, prophet)
	}
	return nil
}

func (r *Replica) PullSnapshot(ctx context.Context, endpoint, reconcile string) error {
	state, err := r.GetState(ctx)
	if err != nil {
		return fmt.Errorf("get current seq: %w", err)
	}

	target := r.target(state, endpoint)
	var prophet string
	reqCtx := client.WithProphet(client.WithTarget(ctx, target), &prophet)
	var snapshot SnapshotResponse
	if err := client.Get(reqCtx, "sync/snapshot?"+r.query(state).Encode(), &snapshot); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return shared.ErrSyncUnreachable
		}
		return fmt.Errorf("fetch snapshot: %w", err)
	}
	if err := r.checkProphet(prophet); err != nil {
		return err
	}

	to := State{Cursor: snapshot.SyncCursor, Prophet: prophet, Endpoint: target.Endpoint}
	return r.ApplyPage(ctx, state.Cursor, to, snapshot.Records, reconcile)
}

func (r *Replica) PullSync(ctx context.Context) (int, error) {
	state, err := r.GetState(ctx)
	if err != nil {
		return 0, fmt.Errorf("get current seq: %w", err)
	}

	payloads, prophet, err := r.GetSyncPage(ctx, state)
	if err != nil {
		return 0, err
	}
	if err := r.checkProphet(prophet); err != nil {
		return 0, err
	}
	if state.Prophet != "" && prophet != "" && prophet != state.Prophet {
		return 0, ErrSnapshotRequired
	}
	bind := state.Prophet == "" && prophet != ""
	if len(payloads) == 0 && !bind {
		return 0, nil
	}

	to := State{Cursor: state.Cursor, Prophet: prophet, Endpoint: r.target(state, "").Endpoint}
	for _, payload := range payloads {
		if to.Cursor.Less(payload.SyncCursor) {
			to.Cursor = payload.SyncCursor
		}
	}

	if err := r.ApplyPage(ctx, state.Cursor, to, payloads, ""); err != nil {
		return 0, err
	}

	return len(payloads), nil
}

func (r *Replica) GetSyncPage(ctx context.Context, state State) ([]SyncPayload, string, error) {
	var prophet string
	reqCtx := client.WithProphet(client.WithTarget(client.WithTimeout(ctx, syncHold+syncHoldMargin), r.target(state, "")), &prophet)
	q := r.query(state)
	q.Set("wait", fmt.Sprint(int(syncHold.Seconds())))
	var payloads []SyncPayload
	if err := client.Get(reqCtx, fmt.Sprintf("sync/%s?%s", state.Cursor, q.Encode()), &payloads); err != nil {
		switch {
		case errors.Is(err, client.ErrNotFound):
			return nil, "", shared.ErrSyncUnreachable
		case errors.Is(err, client.ErrNotHome):
			return nil, "", err
		case errors.Is(err, client.ErrConflict):
			return nil, "", ErrSnapshotRequired
		}
		return nil, "", fmt.Errorf("fetch sync: %w", err)
	}
	return payloads, prophet, nil
}
