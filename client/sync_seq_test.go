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
		w.Header().Set(sclient.HeaderSyncSeq, "42")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{},"count":1}`))
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)

	var seq int64
	if err := sclient.Post(sclient.WithSyncSeq(context.Background(), &seq), "organization", map[string]string{}, nil); err != nil {
		t.Fatalf("post: %v", err)
	}
	if seq != 42 {
		t.Fatalf("the write must record the prophet's sequence, got %d", seq)
	}
}
