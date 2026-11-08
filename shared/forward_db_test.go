//go:build dbtest

package shared_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestResolveExternalUserOnlyWithinTheCallersOrganizations(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	for externalID, owner := range map[string]uuid.UUID{"ext-root-member": orgRoot, "ext-kept-member": orgKept} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO claimius.user_token (user_id, type, external_id, sa_owner_id)
			SELECT ?, 'external', ?, ?
			WHERE NOT EXISTS (SELECT 1 FROM claimius.user_token WHERE external_id = ?)
		`, userB, externalID, owner, externalID); err != nil {
			t.Fatalf("seed user_token %s: %v", externalID, err)
		}
	}

	user, err := db.ResolveExternalUser(ctx, userA, "ext-root-member")
	if err != nil || user == nil || user.UserID != userB {
		t.Fatalf("external id owned by an organization the caller reads: want %s, got %v %v", userB, user, err)
	}

	user, err = db.ResolveExternalUser(ctx, userA, "ext-kept-member")
	if err != nil || user != nil {
		t.Fatalf("external id owned by an organization the caller cannot read: want no user, got %v %v", user, err)
	}
}
