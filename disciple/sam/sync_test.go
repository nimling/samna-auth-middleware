package sam_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/client"
	sam "github.com/nimling/samna-auth-middleware/disciple/sam"
	"github.com/uptrace/bun"
)

func TestApplyPayload_SkipsSamnaApp(t *testing.T) {
	payload := sam.SyncPayload{
		SyncCursor: client.SyncCursor{Txid: 3, Seq: 42},
		Operation:  "upsert",
		EventType:  "claimius.samna_app",
		Payload:    json.RawMessage(`{"id":"10000000-0000-4000-8000-000000000001","slug":"nimployer"}`),
	}
	if err := sam.ApplyPayload(context.Background(), bun.Tx{}, payload); err != nil {
		t.Fatalf("samna_app must be skipped without touching the database, got %v", err)
	}
}

func TestApplyPayload_SkipsSamnaAppDelete(t *testing.T) {
	payload := sam.SyncPayload{
		SyncCursor: client.SyncCursor{Txid: 3, Seq: 43},
		Operation:  "delete",
		EventType:  "claimius.samna_app",
		Payload:    json.RawMessage(`{"id":"10000000-0000-4000-8000-000000000001"}`),
	}
	if err := sam.ApplyPayload(context.Background(), bun.Tx{}, payload); err != nil {
		t.Fatalf("samna_app delete must be skipped without touching the database, got %v", err)
	}
}

func TestClaimAreas_ClaimNamesItself(t *testing.T) {
	areas, err := sam.ClaimAreas("claimius.claim", json.RawMessage(`{"id":"20000000-0000-4000-8000-000000000001"}`))
	if err != nil {
		t.Fatalf("resolve claim area: %v", err)
	}
	if len(areas) != 1 || areas[0].String() != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("a claim payload moves its own area, got %v", areas)
	}
}

func TestClaimAreas_BindingNamesItsClaim(t *testing.T) {
	raw := json.RawMessage(`{"id":"30000000-0000-4000-8000-000000000009","claim_id":"20000000-0000-4000-8000-000000000002","user_id":"40000000-0000-4000-8000-000000000003"}`)
	areas, err := sam.ClaimAreas("claimius.user_claim", raw)
	if err != nil {
		t.Fatalf("resolve binding area: %v", err)
	}
	if len(areas) != 1 || areas[0].String() != "20000000-0000-4000-8000-000000000002" {
		t.Fatalf("a binding moves the claim it binds, got %v", areas)
	}
}

func TestClaimAreas_ClaimCompositionMovesBoth(t *testing.T) {
	raw := json.RawMessage(`{"claim_id":"20000000-0000-4000-8000-000000000004","object_type":"claimius.claim","object_id":"20000000-0000-4000-8000-000000000005"}`)
	areas, err := sam.ClaimAreas("claimius.claim_object", raw)
	if err != nil {
		t.Fatalf("resolve composition areas: %v", err)
	}
	if len(areas) != 2 {
		t.Fatalf("a claim linked to a claim moves both areas, got %v", areas)
	}
	if areas[0].String() != "20000000-0000-4000-8000-000000000004" || areas[1].String() != "20000000-0000-4000-8000-000000000005" {
		t.Fatalf("both the binding claim and the linked claim move, got %v", areas)
	}
}

func TestClaimAreas_ObjectBindingMovesOnlyTheClaim(t *testing.T) {
	raw := json.RawMessage(`{"claim_id":"20000000-0000-4000-8000-000000000006","object_type":"public.article","object_id":"50000000-0000-4000-8000-000000000007"}`)
	areas, err := sam.ClaimAreas("claimius.claim_object", raw)
	if err != nil {
		t.Fatalf("resolve object binding area: %v", err)
	}
	if len(areas) != 1 || areas[0].String() != "20000000-0000-4000-8000-000000000006" {
		t.Fatalf("a claim bound to an ordinary object moves one area, got %v", areas)
	}
}

func TestClaimAreas_UnrelatedEventMovesNothing(t *testing.T) {
	areas, err := sam.ClaimAreas("claimius.samna_user", json.RawMessage(`{"id":"60000000-0000-4000-8000-000000000008"}`))
	if err != nil {
		t.Fatalf("resolve unrelated event: %v", err)
	}
	if len(areas) != 0 {
		t.Fatalf("an unrelated event moves no claim area, got %v", areas)
	}
}

func TestGetSyncPageAsksTheProphetToHoldAndNamesTheConsumer(t *testing.T) {
	var path, wait, consumer string
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, wait, consumer = r.URL.Path, r.URL.Query().Get("wait"), r.URL.Query().Get("consumer")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"txid":4,"seq":8,"operation":"upsert","event_type":"claimius.claim","payload":{}}],"count":1}`))
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)

	state := sam.State{ID: uuid.MustParse("70000000-0000-4000-8000-000000000001"), Cursor: client.SyncCursor{Txid: 3, Seq: 7}}
	payloads, _, err := (&sam.Replica{}).GetSyncPage(context.Background(), state)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(payloads) != 1 || payloads[0].SyncCursor != (client.SyncCursor{Txid: 4, Seq: 8}) {
		t.Fatalf("the page must decode, got %v", payloads)
	}
	if path != "/sam/sync/3.7" || wait != "25" || consumer != "70000000-0000-4000-8000-000000000001" {
		t.Fatalf("the pull must name the cursor, the hold and the consumer, got %s wait=%s consumer=%s", path, wait, consumer)
	}
}

func TestGetSyncPageTurnsAConflictIntoASnapshot(t *testing.T) {
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)

	if _, _, err := (&sam.Replica{}).GetSyncPage(context.Background(), sam.State{Cursor: client.SyncCursor{Seq: 7}}); !errors.Is(err, sam.ErrSnapshotRequired) {
		t.Fatalf("a 409 from the prophet must require the snapshot, got %v", err)
	}
}

func TestGetSyncPageNamesTheBoundProphetAndWhoAnswered(t *testing.T) {
	var bound string
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bound = r.URL.Query().Get("prophet")
		w.Header().Set(client.HeaderProphet, "home")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[],"count":0}`))
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)

	_, answered, err := (&sam.Replica{}).GetSyncPage(context.Background(), sam.State{Prophet: "home", Endpoint: prophet.URL})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if bound != "home" || answered != "home" {
		t.Fatalf("the pull must name the bound prophet and report who answered, got %q and %q", bound, answered)
	}
}

func TestGetSyncPageKeepsARefusalNamingTheHomeApartFromASnapshot(t *testing.T) {
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(client.HeaderHome, "next")
		w.WriteHeader(http.StatusConflict)
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)

	_, _, err := (&sam.Replica{}).GetSyncPage(context.Background(), sam.State{Prophet: "old"})
	if !errors.Is(err, client.ErrNotHome) || errors.Is(err, sam.ErrSnapshotRequired) {
		t.Fatalf("a prophet refusing the disciple must not send it to the snapshot, got %v", err)
	}
}
