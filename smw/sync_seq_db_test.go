//go:build dbtest

package smw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func routingDisciple(t *testing.T) client.SyncCursor {
	t.Helper()
	if sdb.Instance() == nil {
		if _, err := sdb.Init(echo.New().Logger); err != nil {
			t.Fatalf("connect: %v", err)
		}
	}
	db := sdb.Instance()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO claimius.disciple_state (last_applied_txid, last_applied_seq) SELECT 0, 0 WHERE NOT EXISTS (SELECT 1 FROM claimius.disciple_state)`); err != nil {
		t.Fatalf("cursor row: %v", err)
	}
	routes := db.RouteToProphet
	db.RouteToProphet = true
	t.Cleanup(func() { db.RouteToProphet = routes })
	from, err := db.GetSeq(context.Background())
	if err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	return from
}

func seqProphet(t *testing.T, seq client.SyncCursor) {
	t.Helper()
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(client.HeaderSyncSeq, seq.String())
		w.Write([]byte(`{"data":null,"count":0}`))
	}))
	t.Cleanup(prophet.Close)
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)
}

func serve(t *testing.T, timeout time.Duration, write bool) (int, time.Duration) {
	t.Helper()
	e := echo.New()
	handler := smw.WaitForSeq(timeout)(func(c echo.Context) error {
		if write {
			if _, err := sdb.Instance().NewDelete().Model(&shared.SamnaClient{ID: uuid.New()}).Exec(c.Request().Context()); err != nil {
				t.Errorf("routed delete: %v", err)
			}
		}
		return c.NoContent(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	started := time.Now()
	if err := handler(e.NewContext(httptest.NewRequest(http.MethodDelete, "/", nil), rec)); err != nil {
		t.Fatalf("serve: %v", err)
	}
	return rec.Code, time.Since(started)
}

func TestARoutedWriteAnswersOnceTheDiscipleHasAppliedIt(t *testing.T) {
	from := routingDisciple(t)
	target := client.SyncCursor{Txid: from.Txid, Seq: from.Seq + 3}
	seqProphet(t, target)

	applied := make(chan time.Time, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := sam.Disciple().ApplyPage(context.Background(), from, sam.State{Cursor: target}, nil, ""); err != nil {
			t.Errorf("apply page: %v", err)
		}
		applied <- time.Now()
	}()
	code, took := serve(t, 5*time.Second, true)
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	if took < 300*time.Millisecond {
		t.Fatalf("the answer must wait for the cursor, it came after %s", took)
	}
	seq, err := sdb.Instance().GetSeq(context.Background())
	if err != nil || seq.Less(target) {
		t.Fatalf("the answer must leave the cursor at %s or later, got %s, %v", target, seq, err)
	}
	<-applied
}

func TestARequestWithoutARoutedWriteDoesNotWait(t *testing.T) {
	from := routingDisciple(t)
	seqProphet(t, client.SyncCursor{Txid: from.Txid, Seq: from.Seq + 1000})

	code, took := serve(t, 5*time.Second, false)
	if code != http.StatusOK || took > 200*time.Millisecond {
		t.Fatalf("a request with no routed write must answer at once, got %d after %s", code, took)
	}
}

func TestARoutedWriteTheCursorNeverReachesStillAnswers(t *testing.T) {
	from := routingDisciple(t)
	seqProphet(t, client.SyncCursor{Txid: from.Txid, Seq: from.Seq + 1000})

	code, took := serve(t, 300*time.Millisecond, true)
	if code != http.StatusOK {
		t.Fatalf("a timed out wait must still answer, got %d", code)
	}
	if took < 300*time.Millisecond || took > 3*time.Second {
		t.Fatalf("a timed out wait must last its bound, it lasted %s", took)
	}
}
