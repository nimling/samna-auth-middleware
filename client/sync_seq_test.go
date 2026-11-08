package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sclient "github.com/nimling/samna-auth-middleware/v2/client"
)

func TestWithSyncSeqRecordsTheSequenceOfAWrite(t *testing.T) {
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(sclient.HeaderSyncSeq, "7.42")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{},"count":1}`))
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)

	var seq sclient.SyncCursor
	if err := sclient.Post(sclient.WithSyncSeq(context.Background(), &seq), "organization", map[string]string{}, nil); err != nil {
		t.Fatalf("post: %v", err)
	}
	if seq != (sclient.SyncCursor{Txid: 7, Seq: 42}) {
		t.Fatalf("the write must record the prophet's position, got %s", seq)
	}
}

func TestSyncCursorOrdersByTransactionThenSequence(t *testing.T) {
	early := sclient.SyncCursor{Txid: 10, Seq: 900}
	late := sclient.SyncCursor{Txid: 11, Seq: 5}
	if !early.Less(late) || late.Less(early) {
		t.Fatal("a lower transaction comes first whatever its sequence")
	}
	if !(sclient.SyncCursor{Txid: 11, Seq: 4}).Less(late) {
		t.Fatal("within one transaction the sequence orders")
	}
	got, err := sclient.ParseSyncCursor(late.String())
	if err != nil || got != late {
		t.Fatalf("the cursor must round trip through its string, got %v %v", got, err)
	}
	for _, bad := range []string{"", "12", "a.1", "1.b", "-1.2"} {
		if _, err := sclient.ParseSyncCursor(bad); err == nil {
			t.Fatalf("%q must not parse", bad)
		}
	}
}

func TestWithTargetReachesAnotherProphetAndRecordsWhoAnswered(t *testing.T) {
	var slug, secret string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug, secret = r.Header.Get("SAuth-App-Slug"), r.Header.Get("SAuth-App-Secret")
		w.Header().Set(sclient.HeaderProphet, "peer")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[],"count":0}`))
	}))
	defer peer.Close()
	t.Setenv("SAUTH_ENDPOINT", "http://127.0.0.1:1")

	var prophet string
	ctx := sclient.WithProphet(sclient.WithTarget(context.Background(), sclient.Target{Endpoint: peer.URL, Slug: "mirrored", Secret: "s"}), &prophet)
	if err := sclient.Get(ctx, "sync/0.0", nil); err != nil {
		t.Fatalf("get: %v", err)
	}
	if slug != "mirrored" || secret != "s" || prophet != "peer" {
		t.Fatalf("the request must go to the target as its app and record who answered, got %q %q %q", slug, secret, prophet)
	}
}

func TestARefusalNamingTheHomeIsNotHome(t *testing.T) {
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(sclient.HeaderHome, "elsewhere")
		w.WriteHeader(http.StatusConflict)
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)

	err := sclient.Post(context.Background(), "claim", map[string]string{}, nil)
	if !errors.Is(err, sclient.ErrNotHome) || !errors.Is(err, sclient.ErrConflict) || !strings.Contains(err.Error(), "elsewhere") {
		t.Fatalf("a 409 naming a home must be ErrNotHome carrying it, got %v", err)
	}
}

func TestBindPointsRequestsAtTheBoundProphet(t *testing.T) {
	var hit bool
	bound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{},"count":1}`))
	}))
	defer bound.Close()
	t.Setenv("SAUTH_ENDPOINT", "http://127.0.0.1:1")
	sclient.Bind(bound.URL, "")
	defer sclient.Bind("", "")

	if err := sclient.Post(context.Background(), "organization", map[string]string{}, nil); err != nil || !hit {
		t.Fatalf("a request must go to the bound prophet, got %v hit=%v", err, hit)
	}
}

func TestARefusedWriteCarriesTheProphetsReason(t *testing.T) {
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"data":[],"error":{"Message":"type invalid"},"count":0}`))
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)

	err := sclient.Post(context.Background(), "claim", map[string]string{}, nil)
	if !errors.Is(err, sclient.ErrBadRequest) || !strings.Contains(err.Error(), "type invalid") {
		t.Fatalf("a refused write must stay ErrBadRequest and carry the prophet's reason, got %v", err)
	}
}
