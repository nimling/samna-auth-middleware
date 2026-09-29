package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sclient "github.com/nimling/samna-auth-middleware/client"
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
