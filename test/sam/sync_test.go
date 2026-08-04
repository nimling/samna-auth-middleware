package sam

import (
	"context"
	"encoding/json"
	"testing"

	sam "github.com/nimling/samna-auth-middleware/disciple/sam"
)

func TestApplyPayload_SkipsSamnaApp(t *testing.T) {
	payload := sam.SyncPayload{
		Seq:       42,
		Operation: "upsert",
		EventType: "claimius.samna_app",
		Payload:   json.RawMessage(`{"id":"10000000-0000-4000-8000-000000000001","slug":"nimployer"}`),
	}
	if err := sam.ApplyPayload(context.Background(), payload); err != nil {
		t.Fatalf("samna_app must be skipped without touching the database, got %v", err)
	}
}

func TestApplyPayload_SkipsSamnaAppDelete(t *testing.T) {
	payload := sam.SyncPayload{
		Seq:       43,
		Operation: "delete",
		EventType: "claimius.samna_app",
		Payload:   json.RawMessage(`{"id":"10000000-0000-4000-8000-000000000001"}`),
	}
	if err := sam.ApplyPayload(context.Background(), payload); err != nil {
		t.Fatalf("samna_app delete must be skipped without touching the database, got %v", err)
	}
}
