package shared

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

type sqlStateError string

func (e sqlStateError) Error() string       { return "sqlstate " + string(e) }
func (e sqlStateError) Field(k byte) string { return map[byte]string{'C': string(e)}[k] }

type executed struct {
	query string
	args  []driver.Value
}

type fakePool struct {
	mu       sync.Mutex
	prepares int
	ran      []executed
	fail     error
	columns  []string
	row      []driver.Value
}

func (p *fakePool) Connect(context.Context) (driver.Conn, error) { return &fakeConn{pool: p}, nil }
func (p *fakePool) Driver() driver.Driver                        { return nil }

func (p *fakePool) queries() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.ran))
	for i, e := range p.ran {
		out[i] = e.query
	}
	return out
}

type fakeConn struct{ pool *fakePool }

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	c.pool.mu.Lock()
	c.pool.prepares++
	c.pool.mu.Unlock()
	return &fakeStmt{pool: c.pool, query: query}, nil
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return nil, errors.New("no transactions") }

type fakeStmt struct {
	pool  *fakePool
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("exec not faked")
}
func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.pool.mu.Lock()
	defer s.pool.mu.Unlock()
	s.pool.ran = append(s.pool.ran, executed{query: s.query, args: args})
	if s.pool.fail != nil {
		return nil, s.pool.fail
	}
	return &fakeRows{columns: s.pool.columns, row: s.pool.row}, nil
}

type fakeRows struct {
	columns []string
	row     []driver.Value
	done    bool
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done || r.row == nil {
		return io.EOF
	}
	r.done = true
	copy(dest, r.row)
	return nil
}

func fakeDB(primary, replica *fakePool) *DB {
	d := &DB{DB: bun.NewDB(sql.OpenDB(primary), pgdialect.New())}
	if replica != nil {
		d.Replica = bun.NewDB(sql.OpenDB(replica), pgdialect.New())
	}
	return d
}

func TestRead_RunsOnThePrimaryWithoutAReplica(t *testing.T) {
	primary := &fakePool{}
	d := fakeDB(primary, nil)
	if _, err := d.ScopeAccess(context.Background(), uuid.New(), `$.page`, ACCESS_READ); err != nil {
		t.Fatalf("scope access: %v", err)
	}
	if got := primary.queries(); len(got) != 1 || !strings.Contains(got[0], "get_scope_access") {
		t.Fatalf("expected the primary to answer the read, got %v", got)
	}
}

func TestRead_SendsAPureReadToTheReplica(t *testing.T) {
	primary, replica := &fakePool{}, &fakePool{}
	d := fakeDB(primary, replica)
	if _, err := d.ScopeAccess(context.Background(), uuid.New(), `$.page`, ACCESS_READ); err != nil {
		t.Fatalf("scope access: %v", err)
	}
	if got := replica.queries(); len(got) != 1 || !strings.Contains(got[0], "get_scope_access") {
		t.Fatalf("expected the replica to answer the scope read, got %v", got)
	}
	if got := primary.queries(); len(got) != 0 {
		t.Fatalf("expected the primary untouched, got %v", got)
	}
}

func TestRead_RetriesOnThePrimaryWhenTheReplicaRefusesAWrite(t *testing.T) {
	primary, replica := &fakePool{}, &fakePool{fail: sqlStateError("25006")}
	d := fakeDB(primary, replica)
	if _, err := d.ScopeAccess(context.Background(), uuid.New(), `$.page`, ACCESS_READ); err != nil {
		t.Fatalf("scope access: %v", err)
	}
	if len(replica.queries()) != 1 || len(primary.queries()) != 1 {
		t.Fatalf("expected one refusal on the replica and one answer on the primary, got replica %v primary %v", replica.queries(), primary.queries())
	}
}

func TestRead_KeepsAnyOtherReplicaError(t *testing.T) {
	primary, replica := &fakePool{}, &fakePool{fail: sqlStateError("57014")}
	d := fakeDB(primary, replica)
	err := d.Read(context.Background(), func(ctx context.Context, db *bun.DB) error {
		return d.prepared(ctx, db, &[]AccessClaim{}, "SELECT 1")
	})
	var state sqlStateError
	if !errors.As(err, &state) || state != "57014" {
		t.Fatalf("expected the replica error back, got %v", err)
	}
	if got := primary.queries(); len(got) != 0 {
		t.Fatalf("expected no retry for an error other than 25006, got %v", got)
	}
}

func TestMutationAccess_StaysOnThePrimary(t *testing.T) {
	primary, replica := &fakePool{}, &fakePool{}
	d := fakeDB(primary, replica)
	if _, err := d.MutationAccess(context.Background(), uuid.New(), uuid.New(), "inventory.device", ACCESS_WRITE); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("expected no access from an empty answer, got %v", err)
	}
	if got := replica.queries(); len(got) != 0 {
		t.Fatalf("a check that may settle must not reach the replica, got %v", got)
	}
	if got := primary.queries(); len(got) != 2 || !strings.Contains(got[0], "mutation_access") || !strings.Contains(got[1], "get_guest_access") {
		t.Fatalf("expected the check and the guest union on the primary, got %v", got)
	}
}

func TestPrepared_ParsesAStatementOncePerConnection(t *testing.T) {
	primary := &fakePool{}
	d := fakeDB(primary, nil)
	d.SetMaxOpenConns(1)
	for range 3 {
		if _, err := d.MutationAccess(context.Background(), uuid.New(), uuid.New(), "inventory.device", ACCESS_WRITE); !errors.Is(err, ErrNoAccess) {
			t.Fatalf("mutation access: %v", err)
		}
	}
	checks := 0
	for _, q := range primary.queries() {
		if strings.Contains(q, "mutation_access") {
			checks++
		}
	}
	if primary.prepares != 1+3 || checks != 3 {
		t.Fatalf("expected one parse for three checks and one per unprepared guest union, got %d parses and %d checks", primary.prepares, checks)
	}
}

func TestObjectType_ConsumerTypePassesThroughIntoSQLAndBack(t *testing.T) {
	for _, consumer := range []string{"inventory.device", "device"} {
		primary := &fakePool{}
		d := fakeDB(primary, nil)
		if _, err := d.MutationAccess(context.Background(), uuid.New(), uuid.New(), consumer, ACCESS_READ); !errors.Is(err, ErrNoAccess) {
			t.Fatalf("mutation access: %v", err)
		}
		if got := primary.ran[0].args[2]; got != consumer {
			t.Fatalf("expected %q to reach SQL untouched, got %v", consumer, got)
		}
		primary.columns = []string{"id", "claim_id", "object_type"}
		primary.row = []driver.Value{uuid.NewString(), uuid.NewString(), consumer}
		objects := make([]ClaimObject, 0)
		if err := d.prepared(context.Background(), d.DB, &objects, "SELECT id, claim_id, object_type FROM claimius.claim_object"); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(objects) != 1 || objects[0].ObjectType == nil || string(*objects[0].ObjectType) != consumer {
			t.Fatalf("expected %q back out of SQL untouched, got %+v", consumer, objects)
		}
	}
}
