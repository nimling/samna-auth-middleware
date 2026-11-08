//go:build dbtest

package shared_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestDiscipleRetentionIsReadAndEditedPerKind(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `UPDATE claimius.retention SET days = 90 WHERE kind = 'notification'`); err != nil {
			t.Errorf("restore notification days: %v", err)
		}
	})
	rows, err := db.GetRetentions(ctx)
	if err != nil {
		t.Fatalf("read retention: %v", err)
	}
	kinds := map[string]int{}
	for _, r := range rows {
		kinds[r.Kind] = r.Days
	}
	if len(kinds) != 3 || kinds["access_diff"] == 0 || kinds["audit"] == 0 || kinds["notification"] == 0 {
		t.Fatalf("a disciple keeps retention for access_diff, audit and notification, got %v", kinds)
	}
	row, err := db.PutRetention(ctx, "notification", 3)
	if err != nil || row.Days != 3 {
		t.Fatalf("edit retention: %+v %v", row, err)
	}
	if _, err := db.PutRetention(ctx, "notification", 0); !errors.Is(err, sdb.ErrBadRetention) {
		t.Fatalf("a retention below a day is refused: %v", err)
	}
	if _, err := db.PutRetention(ctx, "gotest_unknown", 3); !errors.Is(err, sdb.ErrNotFound) {
		t.Fatalf("an unknown kind is not found: %v", err)
	}
}

func TestDiscipleRetentionDeletesExpiredRowsAndTrimsWithNoActiveReader(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	old, fresh := uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.NewRaw(query, args...).Scan(ctx, &n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}

	exec(`INSERT INTO claimius.notification (id, user_id, title, type, closed_at) VALUES
		(?, ?, 'old', 'test', now() - INTERVAL '100 days'), (?, ?, 'fresh', 'test', now())`, old, userA, fresh, userA)
	if _, err := db.DeleteExpired(ctx, "notification"); err != nil {
		t.Fatalf("notification: %v", err)
	}
	if n := count(`SELECT count(*) FROM claimius.notification WHERE id IN (?, ?)`, old, fresh); n != 1 {
		t.Fatalf("notification keeps only the row closed within the retention, found %d", n)
	}

	exec(`SELECT claimius.upsert_audit_partition(now() - INTERVAL '3 years')`)
	exec(`INSERT INTO claimius.audit (id, operation, type, object_type, object_id, sa_owner_id, sa_created_at) VALUES
		(?, 'update', 'test', 'claimius.organization', ?, ?, now() - INTERVAL '3 years'),
		(?, 'update', 'test', 'claimius.organization', ?, ?, now())`, old, orgRoot, orgRoot, fresh, orgRoot, orgRoot)
	if _, err := db.DeleteExpired(ctx, "audit"); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if n := count(`SELECT count(*) FROM claimius.audit WHERE id IN (?, ?)`, old, fresh); n != 1 {
		t.Fatalf("audit keeps only the months within the retention, found %d", n)
	}

	before := count(`SELECT count(*) FROM claimius.get_organizations(?::UUID, 4)`, userA)
	exec(`UPDATE claimius.user_activity SET last_used_at = now() - INTERVAL '30 days'`)
	if _, err := db.DeleteExpired(ctx, "access_diff"); err != nil {
		t.Fatalf("access_diff: %v", err)
	}
	if n := count(`SELECT count(*) FROM claimius.access_diff WHERE txid < pg_snapshot_xmin(pg_current_snapshot())`); n != 0 {
		t.Fatalf("with no reader active within the retention every finished area is trimmed, %d left", n)
	}
	if n := count(`SELECT count(*) FROM claimius.disciple_state WHERE access_trimmed_txid = '0'::XID8`); n != 0 {
		t.Fatalf("the trim records its position")
	}
	if after := count(`SELECT count(*) FROM claimius.get_organizations(?::UUID, 4)`, userA); after != before {
		t.Fatalf("a reader below the trimmed position resettles to the same answer, %d before and %d after", before, after)
	}
}
