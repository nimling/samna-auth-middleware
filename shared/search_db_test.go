//go:build dbtest

package shared_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
)

type searchRow struct {
	ObjectID   uuid.UUID `bun:"object_id"`
	ObjectType string    `bun:"object_type"`
	Name       *string   `bun:"name"`
	Rank       float32   `bun:"rank"`
	TotalCount int       `bun:"total_count"`
}

func scopedSearch(t *testing.T, db *sdb.DB, userID uuid.UUID, query string, subtree []uuid.UUID) []searchRow {
	t.Helper()
	var subtreeArg any
	if subtree != nil {
		subtreeArg = uuidArray(subtree)
	}
	rows := make([]searchRow, 0)
	err := db.NewRaw(
		`SELECT object_id, object_type, name, rank, total_count
		   FROM claimius.search(?, ?, ARRAY['claimius.organization']::text[], 4, NULL, NULL, ?::uuid[], NULL, NULL, 50, 0)`,
		userID, query, subtreeArg,
	).Scan(context.Background(), &rows)
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	return rows
}

func uuidArray(ids []uuid.UUID) string {
	out := "{"
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id.String()
	}
	return out + "}"
}

func searchIDs(rows []searchRow) map[uuid.UUID]bool {
	ids := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		ids[r.ObjectID] = true
	}
	return ids
}

func TestSearchEmptyQueryListsTheScope(t *testing.T) {
	db := database(t)
	rows := scopedSearch(t, db, userA, "", []uuid.UUID{orgAlpha})
	ids := searchIDs(rows)
	if !ids[orgAlpha] || !ids[orgGrand] {
		t.Fatalf("expected alpha and grand under alpha, got %+v", rows)
	}
	if ids[orgRoot] {
		t.Fatalf("root sits above the scope, got %+v", rows)
	}
	if rows[0].TotalCount != len(rows) {
		t.Fatalf("total_count %d, rows %d", rows[0].TotalCount, len(rows))
	}
}

func TestSearchEmptyQueryWithoutScopeListsTheReach(t *testing.T) {
	db := database(t)
	ids := searchIDs(scopedSearch(t, db, userA, "", nil))
	for _, id := range []uuid.UUID{orgRoot, orgAlpha, orgGrand} {
		if !ids[id] {
			t.Fatalf("expected %s in the reader's listing, got %v", id, ids)
		}
	}
}

func TestSearchQueryInsideTheScope(t *testing.T) {
	db := database(t)
	rows := scopedSearch(t, db, userA, "grand", []uuid.UUID{orgRoot})
	if len(rows) != 1 || rows[0].ObjectID != orgGrand {
		t.Fatalf("expected only grand, got %+v", rows)
	}
}

func TestSearchScopeWithoutPairsReturnsNothing(t *testing.T) {
	db := database(t)
	outsider := uuid.MustParse("d0000000-0000-4000-8000-0000000000d5")
	if _, err := db.ExecContext(context.Background(), `SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Outsider')`, outsider); err != nil {
		t.Fatalf("outsider: %v", err)
	}
	if rows := scopedSearch(t, db, outsider, "", []uuid.UUID{orgRoot}); len(rows) != 0 {
		t.Fatalf("the outsider holds no pair under root, got %+v", rows)
	}
}

func TestSearchScopeSmallerPicksTheSmallerSet(t *testing.T) {
	db := database(t)
	var smaller bool
	err := db.NewRaw(
		`SELECT claimius._search_scope_smaller(?, ARRAY['claimius.organization']::text[], 'ownership', 'claimius.organization', ARRAY[?]::uuid[])`,
		userA, orgGrand,
	).Scan(context.Background(), &smaller)
	if err != nil {
		t.Fatalf("scope smaller: %v", err)
	}
	if !smaller {
		t.Fatal("a one object scope is smaller than the reader's three pairs")
	}
}

func TestSearchSourceRowsPassTheReadersPair(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION public.test_org_source(p_query TEXT, p_scope_ids UUID[])
		RETURNS TABLE(object_id UUID, score REAL) AS $f$
			SELECT o.id, 0.9::REAL FROM claimius.organization o
			WHERE p_query = 'zeta' AND o.id IN ('10000000-0000-4000-8000-000000000111', '30000000-0000-4000-8000-000000000001')
		$f$ LANGUAGE sql STABLE`); err != nil {
		t.Fatalf("source function: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_search_source('claimius.organization', 'public.test_org_source(text, uuid[])'::regprocedure)`); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `SELECT claimius.delete_search_source('claimius.organization')`)
	})

	rows := scopedSearch(t, db, userA, "zeta", nil)
	if len(rows) != 1 || rows[0].ObjectID != orgGrand {
		t.Fatalf("expected grand from the source, kept by user A's pair, got %+v", rows)
	}
	if rows[0].Rank < 0.9 {
		t.Fatalf("expected the source score in the rank, got %v", rows[0].Rank)
	}
	if rows := scopedSearch(t, db, userA, "zeta", []uuid.UUID{orgKept}); len(rows) != 0 {
		t.Fatalf("user A holds no pair under kept, got %+v", rows)
	}
}

func TestSearchSourceRejectsTheWrongSignature(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION public.test_bad_source(p_query TEXT)
		RETURNS TABLE(object_id UUID, score REAL) AS $f$ SELECT NULL::UUID, 0::REAL WHERE FALSE $f$ LANGUAGE sql STABLE`); err != nil {
		t.Fatalf("source function: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_search_source('claimius.organization', 'public.test_bad_source(text)'::regprocedure)`); err == nil {
		t.Fatal("expected a source without the scope argument to be refused")
	}
}

func TestSearchQueryWithoutScopeMatchesTheReach(t *testing.T) {
	db := database(t)
	rows := scopedSearch(t, db, userA, "alpha", nil)
	if len(rows) != 1 || rows[0].ObjectID != orgAlpha {
		t.Fatalf("expected only alpha, got %+v", rows)
	}
	outsider := uuid.MustParse("d0000000-0000-4000-8000-0000000000d6")
	if _, err := db.ExecContext(context.Background(), `SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Stranger')`, outsider); err != nil {
		t.Fatalf("outsider: %v", err)
	}
	if rows := scopedSearch(t, db, outsider, "alpha", nil); len(rows) != 0 {
		t.Fatalf("the outsider holds no pair on alpha, got %+v", rows)
	}
}
