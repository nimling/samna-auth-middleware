//go:build dbtest

package echo_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

func TestPutRelationClearsAFieldSentAsNull(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	relationID := uuid.New()
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.user_relation (id, app_id, user_id, object_type, object_id, priority)
		VALUES (?, ?, ?, 'claimius.organization', ?, 9)
	`, relationID, appID, grantUser, uuid.New()); err != nil {
		t.Fatalf("relation: %v", err)
	}
	if code, body := getPatchStatus(pecho.PutRelation, appID, claimID, relationID, `{"priority":null}`); code != http.StatusOK {
		t.Fatalf("clear priority: want 200, got %d %s", code, body)
	}
	var priority *int
	if err := psdb.Instance().NewRaw(`SELECT priority FROM claimius.user_relation WHERE id = ?`, relationID).Scan(ctx, &priority); err != nil {
		t.Fatalf("read priority: %v", err)
	}
	if priority != nil {
		t.Fatalf("a priority sent as null must be cleared, got %d", *priority)
	}
}
