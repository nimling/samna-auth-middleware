//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/uptrace/bun/driver/pgdriver"

	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

type levelFixture struct {
	app         peerApp
	child       uuid.UUID
	writer      uuid.UUID
	writerGrant uuid.UUID
}

func getLevelFixture(t *testing.T) levelFixture {
	t.Helper()
	getGrantFixture(t)
	ctx := context.Background()
	db := psdb.Instance()
	f := levelFixture{app: postPeerApp(t, grantUser), child: uuid.New(), writer: uuid.New()}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Level child', 'company', ?, ?, 1, ?)
	`, f.child, f.app.Org, f.app.Org, grantUser); err != nil {
		t.Fatalf("child: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_app_organization(?, ?, TRUE, ?)`, f.app.ID, f.app.Org, grantUser); err != nil {
		t.Fatalf("link tree: %v", err)
	}
	postLevelUser(t, f.app, f.writer, "Writer")
	writerClaim := postLevelClaim(t, f.app, "Level writer", 15, 0)
	postLevelBinding(t, f.app, writerClaim, &f.app.Org, "claimius.organization", true, "", grantUser)
	f.writerGrant = postLevelGrant(t, f.app, writerClaim, f.writer, grantUser)
	return f
}

func postLevelUser(t *testing.T, app peerApp, user uuid.UUID, name string) {
	t.Helper()
	if _, err := psdb.Instance().ExecContext(context.Background(),
		`SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => ?)`, user, app.ID, name); err != nil {
		t.Fatalf("user %s: %v", name, err)
	}
}

func postLevelClaim(t *testing.T, app peerApp, name string, access, level int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := psdb.Instance().NewRaw(`
		SELECT (claimius.insert_claim(p_app_id => ?, p_name => ?, p_description => NULL, p_sa_access => ?,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?, p_level => ?) -> 'claim' ->> 'id')::UUID
	`, app.ID, name, access, app.Org, app.Org, grantUser, level).Scan(context.Background(), &id); err != nil {
		t.Fatalf("claim %s: %v", name, err)
	}
	return id
}

func postLevelBinding(t *testing.T, app peerApp, claim uuid.UUID, object *uuid.UUID, objectType string, inherits bool, scope string, createdBy uuid.UUID) {
	t.Helper()
	var scopeArg interface{}
	if scope != "" {
		scopeArg = scope
	}
	var typeArg interface{}
	if object != nil {
		typeArg = objectType
	}
	if _, err := psdb.Instance().ExecContext(context.Background(), `
		SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => ?, p_inherits => ?, p_scope => ?::jsonb)
	`, app.ID, claim, app.Org, app.Org, createdBy, object, typeArg, inherits, scopeArg); err != nil {
		t.Fatalf("binding: %v", err)
	}
}

func postLevelGrant(t *testing.T, app peerApp, claim, user, createdBy uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := getLevelGrant(app, claim, user, createdBy)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	return id
}

func getLevelGrant(app peerApp, claim, user, createdBy uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := psdb.Instance().NewRaw(`
		SELECT (claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		        -> 'user_claim' ->> 'id')::UUID
	`, app.ID, claim, user, app.Org, createdBy).Scan(context.Background(), &id)
	return id, err
}

func getSQLState(err error) string {
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		return pgErr.Field('C')
	}
	return ""
}

func TestALevelHandOutCannotLiftAnyDenyTheWriterFaces(t *testing.T) {
	f := getLevelFixture(t)
	noRead := postLevelClaim(t, f.app, "No read level 2", 20, 2)
	postLevelBinding(t, f.app, noRead, &f.child, "claimius.organization", true, "", grantUser)
	postLevelGrant(t, f.app, noRead, f.writer, grantUser)
	noWrite := postLevelClaim(t, f.app, "No write level 5", 18, 5)
	postLevelBinding(t, f.app, noWrite, &f.child, "claimius.organization", true, "", grantUser)
	postLevelGrant(t, f.app, noWrite, f.writer, grantUser)
	readWrite := postLevelClaim(t, f.app, "Read write level 4", 6, 4)
	postLevelBinding(t, f.app, readWrite, &f.child, "claimius.organization", true, "", grantUser)
	other := uuid.New()
	postLevelUser(t, f.app, other, "Other")
	if _, err := getLevelGrant(f.app, readWrite, other, f.writerGrant); getSQLState(err) != "P0403" {
		t.Fatalf("a level 4 grant of read and write must not lift the level 2 read deny the writer faces, got %v", err)
	}
}

func TestALevelHandOutSeesADenyThroughALink(t *testing.T) {
	f := getLevelFixture(t)
	linker := postLevelClaim(t, f.app, "Linker", 4, 0)
	postLevelGrant(t, f.app, linker, f.writer, grantUser)
	deny := postLevelClaim(t, f.app, "No read through a link", 20, 0)
	postLevelBinding(t, f.app, deny, &f.child, "claimius.organization", true, "", grantUser)
	postLevelBinding(t, f.app, linker, &deny, "claimius.claim", true, "", f.writerGrant)
	read := postLevelClaim(t, f.app, "Read level 3", 4, 3)
	postLevelBinding(t, f.app, read, &f.child, "claimius.organization", true, "", grantUser)
	other := uuid.New()
	postLevelUser(t, f.app, other, "Other")
	if _, err := getLevelGrant(f.app, read, other, f.writerGrant); getSQLState(err) != "P0403" {
		t.Fatalf("a level 3 grant must not lift the deny the writer faces through a link, got %v", err)
	}
}

func TestALevelIncreaseIsCheckedForTheWriter(t *testing.T) {
	f := getLevelFixture(t)
	ctx := context.Background()
	plain := postLevelClaim(t, f.app, "Plain", 4, 0)
	postLevelBinding(t, f.app, plain, &f.child, "claimius.organization", true, "", grantUser)
	var raw string
	err := psdb.Instance().NewRaw(`SELECT claimius.update_claim(p_claim_id => ?, p_level => 3, p_actor_user_claim_id => ?)`, plain, f.writerGrant).Scan(ctx, &raw)
	if getSQLState(err) != "P0403" {
		t.Fatalf("a writer holding level 0 on the bound object must not set the claim to level 3, got %v", err)
	}
	if err := psdb.Instance().NewRaw(`SELECT claimius.update_claim(p_claim_id => ?, p_level => 3)`, plain).Scan(ctx, &raw); err != nil {
		t.Fatalf("the creator stamp of the system sets the level: %v", err)
	}
}

func TestTheWalkSubtractsAScopedDeny(t *testing.T) {
	f := getLevelFixture(t)
	ctx := context.Background()
	viewer := uuid.New()
	postLevelUser(t, f.app, viewer, "Viewer")
	read := postLevelClaim(t, f.app, "Org read", 4, 0)
	postLevelBinding(t, f.app, read, &f.child, "claimius.organization", false, "", grantUser)
	postLevelGrant(t, f.app, read, viewer, grantUser)
	noName := postLevelClaim(t, f.app, "No name", 20, 0)
	postLevelBinding(t, f.app, noName, &f.child, "claimius.organization", false, `{"name": 0}`, grantUser)
	postLevelGrant(t, f.app, noName, viewer, grantUser)
	var name, star *int
	if err := psdb.Instance().NewRaw(`
		SELECT (scope ->> 'name')::INTEGER, (scope ->> '*')::INTEGER
		FROM claimius.get_direct_access(?, ?, ?, 'claimius.organization', 0, FALSE, FALSE)
	`, viewer, f.app.ID, f.child).Scan(ctx, &name, &star); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if name == nil || *name != 0 || star == nil || *star != 4 {
		t.Fatalf("the walk must withhold the denied field and keep read on the rest, got name %v star %v", name, star)
	}
}

func TestCapabilitiesFollowALink(t *testing.T) {
	f := getLevelFixture(t)
	ctx := context.Background()
	viewer := uuid.New()
	postLevelUser(t, f.app, viewer, "Viewer")
	linker := postLevelClaim(t, f.app, "Page linker", 4, 0)
	postLevelGrant(t, f.app, linker, viewer, grantUser)
	pages := postLevelClaim(t, f.app, "Pages", 4, 0)
	postLevelBinding(t, f.app, pages, nil, "", true, `{"page": {"docs": {"_access": "READ"}}}`, grantUser)
	postLevelBinding(t, f.app, linker, &pages, "claimius.claim", true, "", f.writerGrant)
	var access int
	if err := psdb.Instance().NewRaw(`SELECT sa_access FROM claimius.get_scope_access(?, ?, '$.page.docs', 0)`, viewer, f.app.ID).Scan(ctx, &access); err != nil {
		t.Fatalf("a scope grant reached through a link must answer: %v", err)
	}
	if access != 4 {
		t.Fatalf("the composed scope grant must carry read, got %d", access)
	}
}

func TestTheDirectorySubtractsADenyOnAUser(t *testing.T) {
	f := getLevelFixture(t)
	ctx := context.Background()
	viewer, target := uuid.New(), uuid.New()
	postLevelUser(t, f.app, viewer, "Viewer")
	postLevelUser(t, f.app, target, "Target")
	see := postLevelClaim(t, f.app, "See target", 4, 0)
	postLevelBinding(t, f.app, see, &target, "claimius.samna_user", false, "", grantUser)
	postLevelGrant(t, f.app, see, viewer, grantUser)
	var rows int
	if err := psdb.Instance().NewRaw(`SELECT count(*) FROM claimius.get_user_access(?, ?, ?::text, 4)`, viewer, f.app.ID, target).Scan(ctx, &rows); err != nil {
		t.Fatalf("directory: %v", err)
	}
	if rows != 1 {
		t.Fatalf("the grant on the user must answer before the deny, got %d rows", rows)
	}
	not := postLevelClaim(t, f.app, "Not target", 20, 0)
	postLevelBinding(t, f.app, not, &target, "claimius.samna_user", false, "", grantUser)
	postLevelGrant(t, f.app, not, viewer, grantUser)
	if err := psdb.Instance().NewRaw(`SELECT count(*) FROM claimius.get_user_access(?, ?, ?::text, 4)`, viewer, f.app.ID, target).Scan(ctx, &rows); err != nil {
		t.Fatalf("directory after the deny: %v", err)
	}
	if rows != 0 {
		t.Fatalf("a deny on the user at the same level must remove the grant, got %d rows", rows)
	}
}
