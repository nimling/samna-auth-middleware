//go:build dbtest

package echo_test

import (
	"context"
	"testing"

	"github.com/labstack/echo/v4"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

const usageSeed = `
DO $$
DECLARE
    v_other UUID := '50000000-0000-4000-8000-0000000000c9';
    v_user  UUID := 'a0000000-0000-4000-8000-0000000000c1';
    v_root  UUID := '10000000-0000-4000-8000-0000000000c1';
    v_claim UUID;
BEGIN
    IF EXISTS (SELECT 1 FROM claimius.claim WHERE name = 'Other reader') THEN
        RETURN;
    END IF;
    PERFORM claimius.ensure_app_user(p_user_id => v_user, p_app_id => v_other, p_first_name => 'Ada');
    PERFORM claimius.link_app_object(v_other, 'claimius.organization', v_root);
    v_claim := (claimius.create_claim(
        p_app_id => v_other, p_name => 'Other reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_user, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(
        p_app_id => v_other, p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_user,
        p_object_id => v_root, p_object_type => 'claimius.organization', p_inherits => TRUE
    );
    PERFORM claimius.assign_claim_user(p_app_id => v_other, p_claim_id => v_claim, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_user);
END $$;
`

func TestUsageFlushCountsTheAppChecked(t *testing.T) {
	getGrantFixture(t)
	db, err := psdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, usageSeed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var settled int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.get_access(?, ?, ?, 'claimius.organization', 4)`, grantUser, otherApp, grantRoot).Scan(ctx, &settled); err != nil || settled != 1 {
		t.Fatalf("the reader holds the root in the other app: %d %v", settled, err)
	}
	pair := func() (used int64) {
		if err := db.NewRaw(`SELECT used_count FROM claimius.user_object WHERE user_id = ? AND app_id = ? AND object_type = 'claimius.organization' AND object_id = ?`,
			grantUser, otherApp, grantRoot).Scan(ctx, &used); err != nil {
			t.Fatalf("pair: %v", err)
		}
		return used
	}
	before := pair()
	app := otherApp
	db.Usage.Add(&app, grantUser, "claimius.organization", grantRoot)
	db.Usage.Add(&app, grantUser, "claimius.organization", grantRoot)
	db.Usage.Stop()
	if got := pair() - before; got != 2 {
		t.Fatalf("expected two uses counted on the pair in the app checked, got %d", got)
	}
	var active int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.user_activity WHERE user_id = ? AND app_id = ?`, grantUser, otherApp).Scan(ctx, &active); err != nil || active != 1 {
		t.Fatalf("expected the user's activity in the app checked: %d %v", active, err)
	}
}
