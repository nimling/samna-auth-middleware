package audit

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func TestWriteHoldsTheCallerOnceEveryWriterIsBusy(t *testing.T) {
	db := &shared.DB{DB: bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithAddr("127.0.0.1:1"))), pgdialect.New())}
	for range writers {
		Write(db, "update", "test", "claimius.organization", uuid.New(), uuid.New(), nil, "busy")
	}
	if len(slots) != writers {
		t.Fatalf("every write in flight holds a writer, %d of %d held", len(slots), writers)
	}
	written := make(chan struct{})
	go func() {
		Write(db, "update", "test", "claimius.organization", uuid.New(), uuid.New(), nil, "waits")
		close(written)
	}()
	select {
	case <-written:
		t.Fatal("a write past the bound started while every writer was busy")
	case <-time.After(200 * time.Millisecond):
	}
	select {
	case <-written:
	case <-time.After(30 * time.Second):
		t.Fatal("a held write never started once a writer was free")
	}
}
