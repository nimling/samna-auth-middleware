//go:build dbtest

package echo_test

import (
	"context"
	"testing"

	"github.com/labstack/echo/v4"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

func TestUsageFlushCountsThePairChecked(t *testing.T) {
	getGrantFixture(t)
	db, err := psdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	ctx := context.Background()
	var settled int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.get_access(?, ?, 'claimius.organization', 4)`, grantUser, grantRoot).Scan(ctx, &settled); err != nil || settled != 1 {
		t.Fatalf("the reader holds the root: %d %v", settled, err)
	}
	pair := func() (used int64) {
		if err := db.NewRaw(`SELECT used_count FROM claimius.user_object WHERE user_id = ? AND object_type = 'claimius.organization' AND object_id = ?`,
			grantUser, grantRoot).Scan(ctx, &used); err != nil {
			t.Fatalf("pair: %v", err)
		}
		return used
	}
	before := pair()
	db.Usage.Add(grantUser, "claimius.organization", grantRoot)
	db.Usage.Add(grantUser, "claimius.organization", grantRoot)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if db, err = psdb.Init(echo.New().Logger); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT count(*) FROM claimius.get_objects(?, 'claimius.organization')`, grantUser); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := pair() - before; got != 2 {
		t.Fatalf("expected two uses counted on the pair, got %d", got)
	}
	var active int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.user_activity WHERE user_id = ?`, grantUser).Scan(ctx, &active); err != nil || active != 1 {
		t.Fatalf("expected the user's activity: %d %v", active, err)
	}
}
