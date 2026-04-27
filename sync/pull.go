package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/nimling/samna-auth-middleware/db"
)

type responseEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type SnapshotResponse struct {
	Records []SyncPayload `json:"records"`
	Seq     int64         `json:"seq"`
}

func PullSnapshot(ctx context.Context) error {
	endpoint := os.Getenv("SAUTH_ENDPOINT")
	slug := os.Getenv("SAUTH_APP_SLUG")
	secret := os.Getenv("SAUTH_APP_SECRET")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/sam/sync/snapshot", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("SAuth-App-Slug", slug)
	req.Header.Set("SAuth-App-Secret", secret)

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("fetch snapshot: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return db.ErrSyncUnreachable
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("snapshot returned %d: %s", resp.StatusCode, string(body))
	}

	var envelope responseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}

	var snapshot SnapshotResponse
	if err := json.Unmarshal(envelope.Data, &snapshot); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
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
	endpoint := os.Getenv("SAUTH_ENDPOINT")
	slug := os.Getenv("SAUTH_APP_SLUG")
	secret := os.Getenv("SAUTH_APP_SECRET")

	seq, err := GetSeq(ctx)
	if err != nil {
		return fmt.Errorf("get current seq: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/sam/sync/%d", endpoint, seq), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("SAuth-App-Slug", slug)
	req.Header.Set("SAuth-App-Secret", secret)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("fetch sync: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return db.ErrSyncUnreachable
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sync returned %d: %s", resp.StatusCode, string(body))
	}

	var syncEnvelope responseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&syncEnvelope); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}

	var payloads []SyncPayload
	if err := json.Unmarshal(syncEnvelope.Data, &payloads); err != nil {
		return fmt.Errorf("decode sync: %w", err)
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
