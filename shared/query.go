package shared

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

func modelTableName(db *DB, model any) string {
	if db == nil || model == nil {
		return ""
	}
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	table := db.DB.Dialect().Tables().Get(t)
	if table == nil {
		return ""
	}
	return string(table.Name)
}

func embeddedPtr(model any, target reflect.Type) any {
	v := reflect.ValueOf(model)
	if !v.IsValid() || v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	if v.Type().Elem() == target {
		return model
	}
	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < elem.NumField(); i++ {
		f := elem.Field(i)
		if f.Type() == target && f.CanAddr() {
			return f.Addr().Interface()
		}
	}
	return nil
}

func QueryWithObject(ctx echo.Context) ([]ObjectType, error) {
	seen := make(map[ObjectType]struct{})
	var out []ObjectType
	for _, v := range ctx.QueryParams()["with"] {
		for _, raw := range strings.Split(v, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			qualified, err := ResolveObjectType(name)
			if err != nil {
				return nil, err
			}
			if _, dup := seen[qualified]; dup {
				continue
			}
			seen[qualified] = struct{}{}
			out = append(out, qualified)
		}
	}
	return out, nil
}

func QueryPage(ctx echo.Context) (*uuid.UUID, int, error) {
	var after *uuid.UUID
	if raw := ctx.QueryParam("cursor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("cursor must be a uuid: %w", err)
		}
		after = &id
	}
	limit := 0
	if raw := ctx.QueryParam("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return nil, 0, fmt.Errorf("limit must be a positive integer")
		}
		limit = n
	}
	return after, limit, nil
}

func UserWhere(userID uuid.UUID, requiredAccess ClaimAccess) string {
	return fmt.Sprintf(
		`(samna_user.user_id = '%[1]s' OR (samna_user.user_id IN (SELECT user_id FROM claimius.get_users('%[1]s', %[2]d)) AND samna_user.user_id NOT IN (SELECT user_id FROM claimius.get_users WHERE type = 'system' AND sa_deleted_at IS NULL)))`,
		userID, requiredAccess,
	)
}

func ClaimWhere(userID uuid.UUID, requiredAccess ClaimAccess) string {
	return fmt.Sprintf(
		`(claim.id IN (SELECT id FROM claimius.get_claims('%[1]s', %[2]d)) OR claim.id IN (SELECT object_id FROM claimius.get_objects('%[1]s', 'claimius.claim', %[2]d)))`,
		userID, requiredAccess,
	)
}

func ObjectWhere(q *bun.SelectQuery, userID uuid.UUID, objectType string, requiredAccess ClaimAccess, objectID *uuid.UUID, after *uuid.UUID, limit int) *bun.SelectQuery {
	table := "?TableAlias"
	if _, ok := q.GetModel().(bun.TableModel); !ok {
		table = q.GetTableName()
		if i := strings.LastIndex(strings.ToUpper(table), " AS "); i >= 0 {
			table = strings.TrimSpace(table[i+4:])
		}
	}
	if userID == AnonymousUserID {
		if objectID != nil {
			q = q.Where(table+".id = ?", *objectID)
		}
		q = q.Where("EXISTS (SELECT 1 FROM claimius.get_guest_access(?, "+table+".id, ?))", objectType, int(requiredAccess))
		if after != nil {
			q = q.Where(table+".id > ?", *after)
		}
		if limit > 0 {
			q = q.Limit(limit)
		}
		if after != nil || limit > 0 {
			q = q.OrderExpr(table + ".id")
		}
		return q
	}
	if objectID != nil {
		return q.
			Where(table+".id = ?", *objectID).
			Where("EXISTS (SELECT 1 FROM claimius.get_access(?, ?, ?, ?))", userID, *objectID, objectType, int(requiredAccess))
	}
	var pageLimit any
	if limit > 0 {
		pageLimit = limit
	}
	q = q.Where(table+".id IN (SELECT object_id FROM claimius.get_objects(?, ?, ?, ?, ?))", userID, objectType, int(requiredAccess), after, pageLimit)
	if after != nil || limit > 0 {
		q = q.OrderExpr(table + ".id")
	}
	return q
}

func AuditWhere(userID uuid.UUID, requiredAccess ClaimAccess) string {
	return fmt.Sprintf(
		`audit.id IN (SELECT id FROM claimius.get_audit('%[1]s', %[2]d))`,
		userID, requiredAccess,
	)
}

type endpointSpec struct {
	base    string
	parents []string
}

var endpointSpecs = map[ObjectType]endpointSpec{
	OBJECT_TYPE_CLAIM:         {base: "claim"},
	OBJECT_TYPE_CLAIM_OBJECT:  {base: "object", parents: []string{"claim"}},
	OBJECT_TYPE_USER_CLAIM:    {base: "user", parents: []string{"claim"}},
	OBJECT_TYPE_USER:          {base: "user"},
	OBJECT_TYPE_ORGANIZATION:  {base: "organization"},
	OBJECT_TYPE_LOCATION:      {base: "location"},
	OBJECT_TYPE_SAMNA_CLIENT:  {base: "samna_client"},
	OBJECT_TYPE_SAMNA_SECRET:  {base: "secret"},
	OBJECT_TYPE_USER_RELATION: {base: "relation"},
	OBJECT_TYPE_OBJECT_ASSET:  {base: "object_asset"},
	OBJECT_TYPE_APP_COST:      {base: "cost"},
}

var payloadEndpoints = map[reflect.Type]string{
	reflect.TypeOf(&SamnaUserToken{}):    "user/token",
	reflect.TypeOf(&SamnaUserExternal{}): "user/external",
	reflect.TypeOf(&AppCostInput{}):      "cost",
}

func (d *DB) routes(model any) bool {
	if !d.RouteToProphet {
		return false
	}
	if _, ok := payloadEndpoints[reflect.TypeOf(model)]; ok {
		return true
	}
	otype, err := ObjectTypeOf(model)
	if err != nil {
		return false
	}
	_, ok := endpointSpecs[otype]
	return ok
}

type writeMarshaler interface{ MarshalWrite() ([]byte, error) }
type writeUnmarshaler interface{ UnmarshalWrite([]byte) error }
type patchBodyer interface{ PatchBody() map[string]any }
type writeResulter interface{ WriteResult() any }

func marshalWrite(model any) ([]byte, error) {
	m, ok := model.(writeMarshaler)
	if !ok {
		return nil, fmt.Errorf("model %T does not implement MarshalWrite", model)
	}
	return m.MarshalWrite()
}

func unmarshalWrite(model any, raw []byte) error {
	m, ok := model.(writeUnmarshaler)
	if !ok {
		return fmt.Errorf("model %T does not implement UnmarshalWrite", model)
	}
	return m.UnmarshalWrite(raw)
}

func patchBody(model any) map[string]any {
	if m, ok := model.(patchBodyer); ok {
		return m.PatchBody()
	}
	return nil
}

func writeResult(model any) any {
	if m, ok := model.(writeResulter); ok {
		return m.WriteResult()
	}
	return model
}

func (d *DB) applyRouted(ctx context.Context, conn bun.IConn, model any, raw json.RawMessage, dest ...any) error {
	result := writeResult(model)
	if len(raw) > 0 {
		if err := unmarshalWrite(result, raw); err != nil {
			return err
		}
		apply := func(c bun.IConn) error {
			if _, err := c.ExecContext(ctx, "SELECT set_config('claimius.replay_mode', 'true', true)"); err != nil {
				return err
			}
			if err := ApplyEvent(ctx, d.DB, c, "upsert", modelTableName(d, result), raw); err != nil {
				return err
			}
			_, err := c.ExecContext(ctx, "SELECT set_config('claimius.replay_mode', 'false', true)")
			return err
		}
		if conn != nil {
			if err := apply(conn); err != nil {
				return err
			}
		} else if err := d.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error { return apply(tx) }); err != nil {
			return err
		}
	}
	return dispatchScan(result, dest...)
}

func EndpointFor(model any, recordID uuid.UUID) (string, error) {
	if path, ok := payloadEndpoints[reflect.TypeOf(model)]; ok {
		return path, nil
	}
	otype, err := ObjectTypeOf(model)
	if err != nil {
		return "", err
	}
	spec, ok := endpointSpecs[otype]
	if !ok {
		return "", fmt.Errorf("no endpoint mapping for object_type %q", otype)
	}
	var parts []string
	for _, p := range spec.parents {
		pid, err := parentIDFromModel(model, p)
		if err != nil {
			return "", fmt.Errorf("endpoint %q: %w", otype, err)
		}
		parts = append(parts, p, pid.String())
	}
	parts = append(parts, spec.base)
	if recordID != uuid.Nil {
		parts = append(parts, recordID.String())
	}
	return strings.Join(parts, "/"), nil
}

func parentIDFromModel(model any, parent string) (uuid.UUID, error) {
	switch parent {
	case "claim":
		if m, ok := embeddedPtr(model, reflect.TypeOf(ClaimObject{})).(*ClaimObject); ok && m != nil {
			if m.ClaimID == uuid.Nil {
				return uuid.Nil, fmt.Errorf("model.claim_id required")
			}
			return m.ClaimID, nil
		}
		if m, ok := embeddedPtr(model, reflect.TypeOf(UserClaim{})).(*UserClaim); ok && m != nil {
			if m.ClaimID == uuid.Nil {
				return uuid.Nil, fmt.Errorf("model.claim_id required")
			}
			return m.ClaimID, nil
		}
	case "user":
		if m, ok := embeddedPtr(model, reflect.TypeOf(UserRelation{})).(*UserRelation); ok && m != nil {
			if m.UserID == uuid.Nil {
				return uuid.Nil, fmt.Errorf("model.user_id required")
			}
			return m.UserID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("parent %q not derivable from model", parent)
}

type conflictSet struct {
	expr string
	args []any
}

func dispatchScan(src any, dest ...any) error {
	for _, d := range dest {
		if d == nil {
			continue
		}
		if err := CopyInto(src, d); err != nil {
			return err
		}
	}
	return nil
}

func CopyInto(src, dst any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

type InsertQuery struct {
	db          *DB
	conn        bun.IConn
	model       any
	accessClaim *AccessClaim
	excludes    []string
	conflict    string
	confSet     []conflictSet
	returns     string
	seq         client.SyncCursor
	created     bool
}

func (d *DB) NewInsert() *InsertQuery {
	return &InsertQuery{db: d}
}

func (q *InsertQuery) Model(model any) *InsertQuery {
	q.model = model
	return q
}

func (q *InsertQuery) Conn(db bun.IConn) *InsertQuery {
	q.conn = db
	return q
}

func (q *InsertQuery) Access(ac *AccessClaim) *InsertQuery {
	q.accessClaim = ac
	return q
}

func (q *InsertQuery) Returning(s string) *InsertQuery {
	q.returns = s
	return q
}

func (q *InsertQuery) ExcludeColumn(cols ...string) *InsertQuery {
	q.excludes = append(q.excludes, cols...)
	return q
}

func (q *InsertQuery) On(expr string) *InsertQuery {
	q.conflict = expr
	return q
}

func (q *InsertQuery) Set(expr string, args ...any) *InsertQuery {
	q.confSet = append(q.confSet, conflictSet{expr: expr, args: args})
	return q
}

func (q *InsertQuery) buildBun() *bun.InsertQuery {
	bq := q.db.DB.NewInsert().Model(q.model).Conn(q.conn)
	if len(q.excludes) > 0 {
		bq = bq.ExcludeColumn(q.excludes...)
	}
	if q.returns != "" {
		bq = bq.Returning(q.returns)
	}
	if q.conflict != "" {
		bq = bq.On(q.conflict)
		for _, s := range q.confSet {
			bq = bq.Set(s.expr, s.args...)
		}
	}
	return bq
}

func (q *InsertQuery) Exec(ctx context.Context, dest ...any) (sql.Result, error) {
	if q.db.routes(q.model) {
		return nil, q.route(ctx, dest...)
	}
	if handled, err := q.execClaimius(ctx, dest...); handled {
		return nil, err
	}
	return q.buildBun().Exec(ctx, dest...)
}

func (q *InsertQuery) Scan(ctx context.Context, dest ...any) error {
	if q.db.routes(q.model) {
		return q.route(ctx, dest...)
	}
	if handled, err := q.execClaimius(ctx, dest...); handled {
		return err
	}
	return q.buildBun().Scan(ctx, dest...)
}

func (q *InsertQuery) route(ctx context.Context, dest ...any) error {
	if q.accessClaim != nil && q.accessClaim.UserID != uuid.Nil {
		ctx = client.WithForward(ctx, client.Forward{UserID: q.accessClaim.UserID.String()})
	}
	path, err := EndpointFor(q.model, uuid.Nil)
	if err != nil {
		return err
	}
	StampAccessClaim(q.model, q.accessClaim)
	if err := StampRoot(ctx, q.db, q.model, q.accessClaim); err != nil {
		return err
	}
	if co, ok := embeddedPtr(q.model, reflect.TypeOf(ClaimObject{})).(*ClaimObject); ok && co != nil && co.ObjectCreatedBy == nil &&
		co.ObjectID != nil && co.ObjectType != nil && !strings.HasPrefix(string(*co.ObjectType), "claimius.") {
		types, err := q.db.GetObjectTypes(ctx)
		if err != nil {
			return err
		}
		if slices.Contains(types, *co.ObjectType) {
			var createdBy uuid.NullUUID
			err := q.db.NewSelect().Conn(q.conn).
				ColumnExpr("sa_created_by").
				TableExpr("?", bun.Ident(string(*co.ObjectType))).
				Where("id = ?", *co.ObjectID).
				Where("sa_deleted_at IS NULL").
				Scan(ctx, &createdBy)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if createdBy.Valid {
				co.ObjectCreatedBy = &createdBy.UUID
			}
		}
	}
	body, err := marshalWrite(q.model)
	if err != nil {
		return err
	}
	var raw json.RawMessage
	var status int
	if err := client.Post(client.WithStatus(client.WithSyncSeq(ctx, &q.seq), &status), path, json.RawMessage(body), &raw); err != nil {
		if errors.Is(err, client.ErrForbidden) {
			return fmt.Errorf("%w: %w", ErrNoAccess, err)
		}
		if errors.Is(err, client.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrNotFound, err)
		}
		return err
	}
	q.created = status == http.StatusCreated
	return q.db.applyRouted(ctx, q.conn, q.model, raw, dest...)
}

func (q *InsertQuery) Seq() client.SyncCursor { return q.seq }

func (q *InsertQuery) Created() bool { return q.created }

func (q *InsertQuery) execClaimius(ctx context.Context, dest ...any) (handled bool, err error) {
	defer func() {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "P0403" {
			err = fmt.Errorf("%w: %w", ErrNoAccess, err)
		} else if errors.As(err, &pgErr) && pgErr.Field('C') == "P0404" {
			err = fmt.Errorf("%w: %w", ErrNotFound, err)
		}
	}()
	switch m := q.model.(type) {
	case *SamnaUserToken:
		result, err := q.db.postUserFromToken(ctx, q.conn, q.accessClaim, *m)
		if err != nil {
			return true, err
		}
		q.created = result.Created
		return true, dispatchScan(result.SamnaUser, dest...)
	case *AppCostInput:
		result, created, err := q.db.putAppCost(ctx, q.conn, q.accessClaim, m)
		if err != nil {
			return true, err
		}
		q.created = created
		return true, dispatchScan(result, dest...)
	case *SamnaUserExternal:
		result, err := q.db.postUserFromExternal(ctx, q.conn, q.accessClaim, *m)
		if err != nil {
			return true, err
		}
		q.created = result.Created
		return true, dispatchScan(result.SamnaUser, dest...)
	}
	switch modelTableName(q.db, q.model) {
	case "claimius.claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Claim{})).(*Claim)
		if !ok {
			return false, nil
		}
		result, err := q.db.insertClaim(ctx, q.conn, q.accessClaim, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.claim_object":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(ClaimObject{})).(*ClaimObject)
		if !ok {
			return false, nil
		}
		if m.ClaimID == uuid.Nil {
			return true, fmt.Errorf("insert claim_object: claim_id required on model")
		}
		result, err := q.db.insertClaimObject(ctx, q.conn, q.accessClaim, m.ClaimID, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.user_claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(UserClaim{})).(*UserClaim)
		if !ok {
			return false, nil
		}
		if m.ClaimID == uuid.Nil {
			return true, fmt.Errorf("insert user_claim: claim_id required on model")
		}
		result, err := q.db.insertClaimUser(ctx, q.conn, q.accessClaim, m.ClaimID, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_user":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(SamnaUser{})).(*SamnaUser)
		if !ok {
			return false, nil
		}
		result, created, err := q.db.insertUser(ctx, q.conn, q.accessClaim, m)
		if err != nil {
			return true, err
		}
		q.created = created
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.user_identity":
		return true, fmt.Errorf("user_identity creation reserved; use PostUserFromToken")
	case "claimius.organization":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Organization{})).(*Organization)
		if !ok {
			return false, nil
		}
		result, err := q.db.insertOrganization(ctx, q.conn, q.accessClaim, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.location":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Location{})).(*Location)
		if !ok {
			return false, nil
		}
		result, err := q.db.insertLocation(ctx, q.conn, q.accessClaim, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.user_relation":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(UserRelation{})).(*UserRelation)
		if !ok {
			return false, nil
		}
		result, err := q.db.insertRelation(ctx, q.conn, q.accessClaim, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_secret":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(SamnaSecret{})).(*SamnaSecret)
		if !ok {
			return false, nil
		}
		body := BodySecret{Key: &m.Key, Value: &m.Value, OwnerID: &m.OwnerID, ExpiresAt: m.ExpiresAt}
		result, err := q.db.insertSecret(ctx, q.conn, q.accessClaim, body)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	}
	return false, nil
}

func StampAccessClaim(model any, ac *AccessClaim) {
	if ac == nil {
		return
	}
	v := structValue(model)
	if !v.IsValid() {
		return
	}
	if ac.UserClaimID != nil {
		setStructUUID(v, "CreatedBy", *ac.UserClaimID, true)
	}
	setStructUUID(v, "OwnerID", ac.OwnerID, true)
}

func StampRoot(ctx context.Context, db *DB, model any, ac *AccessClaim) error {
	if loc, ok := embeddedPtr(model, reflect.TypeOf(Location{})).(*Location); ok {
		if loc.ParentID == nil || *loc.ParentID == uuid.Nil {
			if loc.ID == uuid.Nil {
				loc.ID = uuid.New()
			}
			loc.RootID = loc.ID
			loc.ParentID = &loc.ID
			return nil
		}
		if loc.RootID == uuid.Nil {
			var root uuid.UUID
			if err := db.NewRaw(`SELECT sa_root_id FROM claimius.location WHERE id = ? AND sa_deleted_at IS NULL`, *loc.ParentID).Scan(ctx, &root); err != nil {
				return err
			}
			loc.RootID = root
		}
		return nil
	}
	if ac != nil {
		v := structValue(model)
		if v.IsValid() {
			setStructUUID(v, "RootID", ac.RootID, true)
		}
	}
	return nil
}

func structValue(model any) reflect.Value {
	v := reflect.ValueOf(model)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return v
}

func setStructUUID(v reflect.Value, field string, val uuid.UUID, onlyIfZero bool) {
	if val == uuid.Nil {
		return
	}
	f := v.FieldByName(field)
	if !f.IsValid() || !f.CanSet() {
		return
	}
	switch f.Type() {
	case reflect.TypeOf(uuid.UUID{}):
		if onlyIfZero && f.Interface().(uuid.UUID) != uuid.Nil {
			return
		}
		f.Set(reflect.ValueOf(val))
	case reflect.TypeOf(&uuid.UUID{}):
		if onlyIfZero && !f.IsNil() && *f.Interface().(*uuid.UUID) != uuid.Nil {
			return
		}
		f.Set(reflect.ValueOf(&val))
	}
}

type UpdateQuery struct {
	db          *DB
	conn        bun.IConn
	model       any
	accessClaim *AccessClaim
	recordID    uuid.UUID
	excludes    []string
	columns     []string
	omitZero    bool
	returns     string
	setCols     []conflictSet
	whereExpr   []conflictSet
	seq         client.SyncCursor
}

func (d *DB) NewUpdate() *UpdateQuery {
	return &UpdateQuery{db: d}
}

func (q *UpdateQuery) Model(model any) *UpdateQuery {
	q.model = model
	return q
}

func (q *UpdateQuery) Conn(db bun.IConn) *UpdateQuery {
	q.conn = db
	return q
}

func (q *UpdateQuery) Access(ac *AccessClaim) *UpdateQuery {
	q.accessClaim = ac
	return q
}

func (q *UpdateQuery) ID(id uuid.UUID) *UpdateQuery {
	q.recordID = id
	return q
}

func (q *UpdateQuery) Returning(s string) *UpdateQuery {
	q.returns = s
	return q
}

func (q *UpdateQuery) ExcludeColumn(cols ...string) *UpdateQuery {
	q.excludes = append(q.excludes, cols...)
	return q
}

func (q *UpdateQuery) Column(cols ...string) *UpdateQuery {
	q.columns = append(q.columns, cols...)
	return q
}

func (q *UpdateQuery) OmitZero() *UpdateQuery {
	q.omitZero = true
	return q
}

func (q *UpdateQuery) Set(expr string, args ...any) *UpdateQuery {
	q.setCols = append(q.setCols, conflictSet{expr: expr, args: args})
	return q
}

func (q *UpdateQuery) Where(expr string, args ...any) *UpdateQuery {
	q.whereExpr = append(q.whereExpr, conflictSet{expr: expr, args: args})
	return q
}

func (q *UpdateQuery) buildBun() *bun.UpdateQuery {
	bq := q.db.DB.NewUpdate().Model(q.model).Conn(q.conn)
	if len(q.excludes) > 0 {
		bq = bq.ExcludeColumn(q.excludes...)
	}
	if len(q.columns) > 0 {
		bq = bq.Column(q.columns...)
	}
	if q.omitZero {
		bq = bq.OmitZero()
	}
	if q.returns != "" {
		bq = bq.Returning(q.returns)
	}
	for _, s := range q.setCols {
		bq = bq.Set(s.expr, s.args...)
	}
	for _, w := range q.whereExpr {
		bq = bq.Where(w.expr, w.args...)
	}
	return bq
}

func (q *UpdateQuery) Exec(ctx context.Context, dest ...any) (sql.Result, error) {
	if q.recordID == uuid.Nil {
		q.recordID = recordIDFromModel(q.db, q.model)
	}
	if q.db.routes(q.model) {
		return nil, q.route(ctx, dest...)
	}
	if handled, err := q.execClaimius(ctx, dest...); handled {
		return nil, err
	}
	return q.buildBun().Exec(ctx, dest...)
}

func (q *UpdateQuery) Scan(ctx context.Context, dest ...any) error {
	if q.recordID == uuid.Nil {
		q.recordID = recordIDFromModel(q.db, q.model)
	}
	if q.db.routes(q.model) {
		return q.route(ctx, dest...)
	}
	if handled, err := q.execClaimius(ctx, dest...); handled {
		return err
	}
	return q.buildBun().Scan(ctx, dest...)
}

func (q *UpdateQuery) route(ctx context.Context, dest ...any) error {
	if q.accessClaim != nil && q.accessClaim.UserID != uuid.Nil {
		ctx = client.WithForward(ctx, client.Forward{UserID: q.accessClaim.UserID.String()})
	}
	if q.recordID == uuid.Nil {
		return fmt.Errorf("%w: %T", ErrRoutedNoRecord, q.model)
	}
	if len(q.setCols) > 0 {
		return fmt.Errorf("%w: %T", ErrRoutedSet, q.model)
	}
	if m, ok := embeddedPtr(q.model, reflect.TypeOf(Organization{})).(*Organization); ok {
		if err := q.db.validateOrganizationParent(ctx, q.accessClaim, q.recordID, m.OwnerID); err != nil {
			return err
		}
	}
	path, err := EndpointFor(q.model, q.recordID)
	if err != nil {
		return err
	}
	var payload any
	if body := patchBody(q.model); body != nil {
		payload = body
	} else {
		body, err := marshalWrite(q.model)
		if err != nil {
			return err
		}
		payload = json.RawMessage(body)
		if len(q.excludes) > 0 || len(q.columns) > 0 || q.omitZero {
			fields := map[string]json.RawMessage{}
			if err := json.Unmarshal(body, &fields); err != nil {
				return err
			}
			strct := reflect.Indirect(reflect.ValueOf(q.model))
			for _, f := range q.db.Dialect().Tables().Get(reflect.TypeOf(q.model)).Fields {
				key, _, _ := strings.Cut(f.StructField.Tag.Get("json"), ",")
				if slices.Contains(q.excludes, f.Name) || (len(q.columns) > 0 && !slices.Contains(q.columns, f.Name)) || (q.omitZero && f.HasZeroValue(strct)) {
					delete(fields, key)
					delete(fields, f.Name)
					delete(fields, f.StructField.Tag.Get("sauth"))
				}
			}
			payload = fields
		}
	}
	var raw json.RawMessage
	if err := client.Patch(client.WithSyncSeq(ctx, &q.seq), path, payload, &raw); err != nil {
		if errors.Is(err, client.ErrForbidden) {
			return fmt.Errorf("%w: %w", ErrNoAccess, err)
		}
		if errors.Is(err, client.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrNotFound, err)
		}
		return err
	}
	return q.db.applyRouted(ctx, q.conn, q.model, raw, dest...)
}

func (q *UpdateQuery) Seq() client.SyncCursor { return q.seq }

func (q *UpdateQuery) execClaimius(ctx context.Context, dest ...any) (handled bool, err error) {
	defer func() {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "P0403" {
			err = fmt.Errorf("%w: %w", ErrNoAccess, err)
		} else if errors.As(err, &pgErr) && pgErr.Field('C') == "P0404" {
			err = fmt.Errorf("%w: %w", ErrNotFound, err)
		}
	}()
	if q.recordID == uuid.Nil {
		return false, nil
	}
	switch modelTableName(q.db, q.model) {
	case "claimius.claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Claim{})).(*Claim)
		if !ok {
			return false, nil
		}
		result, err := q.db.updateClaim(ctx, q.conn, q.accessClaim, q.recordID, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_user":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(SamnaUser{})).(*SamnaUser)
		if !ok {
			return false, nil
		}
		result, err := q.db.updateUser(ctx, q.conn, q.accessClaim, q.recordID, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.organization":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Organization{})).(*Organization)
		if !ok {
			return false, nil
		}
		result, err := q.db.updateOrganization(ctx, q.conn, q.accessClaim, q.recordID, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.location":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Location{})).(*Location)
		if !ok {
			return false, nil
		}
		result, err := q.db.updateLocation(ctx, q.conn, q.accessClaim, q.recordID, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.user_relation":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(UserRelation{})).(*UserRelation)
		if !ok {
			return false, nil
		}
		result, err := q.db.updateRelation(ctx, q.conn, q.accessClaim, q.recordID, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_secret":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(SamnaSecret{})).(*SamnaSecret)
		if !ok {
			return false, nil
		}
		body := BodySecret{Key: &m.Key, Value: &m.Value, OwnerID: &m.OwnerID, ExpiresAt: m.ExpiresAt}
		result, err := q.db.updateSecret(ctx, q.conn, q.accessClaim, q.recordID, body)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	}
	return false, nil
}

type DeleteQuery struct {
	db          *DB
	conn        bun.IConn
	model       any
	accessClaim *AccessClaim
	recordID    uuid.UUID
	whereExpr   []conflictSet
	returns     string
	seq         client.SyncCursor
}

func (d *DB) NewDelete() *DeleteQuery {
	return &DeleteQuery{db: d}
}

func (q *DeleteQuery) Model(model any) *DeleteQuery {
	q.model = model
	return q
}

func (q *DeleteQuery) Conn(db bun.IConn) *DeleteQuery {
	q.conn = db
	return q
}

func (q *DeleteQuery) Access(ac *AccessClaim) *DeleteQuery {
	q.accessClaim = ac
	return q
}

func (q *DeleteQuery) ID(id uuid.UUID) *DeleteQuery {
	q.recordID = id
	return q
}

func (q *DeleteQuery) Returning(s string) *DeleteQuery {
	q.returns = s
	return q
}

func (q *DeleteQuery) Where(expr string, args ...any) *DeleteQuery {
	q.whereExpr = append(q.whereExpr, conflictSet{expr: expr, args: args})
	return q
}

func (q *DeleteQuery) buildBun() *bun.DeleteQuery {
	bq := q.db.DB.NewDelete().Model(q.model).Conn(q.conn)
	if q.returns != "" {
		bq = bq.Returning(q.returns)
	}
	for _, w := range q.whereExpr {
		bq = bq.Where(w.expr, w.args...)
	}
	return bq
}

func recordIDFromModel(d *DB, model any) uuid.UUID {
	if model == nil {
		return uuid.Nil
	}
	switch modelTableName(d, model) {
	case "claimius.samna_user":
		if m, ok := embeddedPtr(model, reflect.TypeOf(SamnaUser{})).(*SamnaUser); ok {
			return m.UserID
		}
	case "claimius.user_claim":
		if m, ok := embeddedPtr(model, reflect.TypeOf(UserClaim{})).(*UserClaim); ok {
			return m.ID
		}
	case "claimius.claim_object":
		if m, ok := embeddedPtr(model, reflect.TypeOf(ClaimObject{})).(*ClaimObject); ok {
			return m.ID
		}
	case "claimius.claim":
		if m, ok := embeddedPtr(model, reflect.TypeOf(Claim{})).(*Claim); ok {
			return m.ID
		}
	case "claimius.organization":
		if m, ok := embeddedPtr(model, reflect.TypeOf(Organization{})).(*Organization); ok {
			return m.ID
		}
	case "claimius.location":
		if m, ok := embeddedPtr(model, reflect.TypeOf(Location{})).(*Location); ok {
			return m.ID
		}
	case "claimius.user_relation":
		if m, ok := embeddedPtr(model, reflect.TypeOf(UserRelation{})).(*UserRelation); ok {
			return m.ID
		}
	case "claimius.samna_secret":
		if m, ok := embeddedPtr(model, reflect.TypeOf(SamnaSecret{})).(*SamnaSecret); ok {
			return m.ID
		}
	case "claimius.object_asset":
		if m, ok := embeddedPtr(model, reflect.TypeOf(ObjectAsset{})).(*ObjectAsset); ok {
			return m.ID
		}
	case "claimius.app_cost":
		if m, ok := embeddedPtr(model, reflect.TypeOf(AppCost{})).(*AppCost); ok {
			return m.ID
		}
	case "claimius.samna_client":
		if m, ok := embeddedPtr(model, reflect.TypeOf(SamnaClient{})).(*SamnaClient); ok {
			return m.ID
		}
	}
	return uuid.Nil
}

func (q *DeleteQuery) Exec(ctx context.Context, dest ...any) (sql.Result, error) {
	if q.recordID == uuid.Nil {
		q.recordID = recordIDFromModel(q.db, q.model)
	}
	if q.db.routes(q.model) {
		return nil, q.route(ctx, dest...)
	}
	if handled, err := q.execClaimius(ctx, dest...); handled {
		return nil, err
	}
	return q.buildBun().Exec(ctx, dest...)
}

func (q *DeleteQuery) Scan(ctx context.Context, dest ...any) error {
	if q.recordID == uuid.Nil {
		q.recordID = recordIDFromModel(q.db, q.model)
	}
	if q.db.routes(q.model) {
		return q.route(ctx, dest...)
	}
	if handled, err := q.execClaimius(ctx, dest...); handled {
		return err
	}
	return q.buildBun().Scan(ctx, dest...)
}

func (q *DeleteQuery) route(ctx context.Context, dest ...any) error {
	if q.accessClaim != nil && q.accessClaim.UserID != uuid.Nil {
		ctx = client.WithForward(ctx, client.Forward{UserID: q.accessClaim.UserID.String()})
	}
	if q.recordID == uuid.Nil {
		return fmt.Errorf("%w: %T", ErrRoutedNoRecord, q.model)
	}
	path, err := EndpointFor(q.model, q.recordID)
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := client.Delete(client.WithSyncSeq(ctx, &q.seq), path, nil, &raw); err != nil {
		if errors.Is(err, client.ErrForbidden) {
			return fmt.Errorf("%w: %w", ErrNoAccess, err)
		}
		if errors.Is(err, client.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrNotFound, err)
		}
		return err
	}
	return q.db.applyRouted(ctx, q.conn, q.model, raw, dest...)
}

func (q *DeleteQuery) Seq() client.SyncCursor { return q.seq }

func (q *DeleteQuery) execClaimius(ctx context.Context, dest ...any) (handled bool, err error) {
	defer func() {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "P0403" {
			err = fmt.Errorf("%w: %w", ErrNoAccess, err)
		} else if errors.As(err, &pgErr) && pgErr.Field('C') == "P0404" {
			err = fmt.Errorf("%w: %w", ErrNotFound, err)
		}
	}()
	if q.recordID == uuid.Nil {
		return false, nil
	}
	switch modelTableName(q.db, q.model) {
	case "claimius.claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Claim{})).(*Claim)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteClaim(ctx, q.conn, q.accessClaim, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.claim_object":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(ClaimObject{})).(*ClaimObject)
		if !ok {
			return false, nil
		}
		if m.ClaimID == uuid.Nil {
			return true, fmt.Errorf("delete claim_object: claim_id required on model")
		}
		result, err := q.db.deleteClaimObject(ctx, q.conn, q.accessClaim, m.ClaimID, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.user_claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(UserClaim{})).(*UserClaim)
		if !ok {
			return false, nil
		}
		if m.ClaimID == uuid.Nil {
			return true, fmt.Errorf("delete user_claim: claim_id required on model")
		}
		result, err := q.db.deleteClaimUser(ctx, q.conn, q.accessClaim, m.ClaimID, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_user":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(SamnaUser{})).(*SamnaUser)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteUser(ctx, q.conn, q.accessClaim, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.organization":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Organization{})).(*Organization)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteOrganization(ctx, q.conn, q.accessClaim, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.location":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Location{})).(*Location)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteLocation(ctx, q.conn, q.accessClaim, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.user_relation":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(UserRelation{})).(*UserRelation)
		if !ok {
			return false, nil
		}
		if m.UserID == uuid.Nil {
			return true, fmt.Errorf("delete user_relation: user_id required on model")
		}
		result, err := q.db.deleteRelation(ctx, q.conn, q.accessClaim, m.UserID, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_secret":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(SamnaSecret{})).(*SamnaSecret)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteSecret(ctx, q.conn, q.accessClaim, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.object_asset":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(ObjectAsset{})).(*ObjectAsset)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteObjectAsset(ctx, q.conn, q.accessClaim, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.app_cost":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(AppCost{})).(*AppCost)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteAppCost(ctx, q.conn, q.accessClaim, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	}
	return false, nil
}

func (q *InsertQuery) Built() (any, *AccessClaim) { return q.model, q.accessClaim }

func (q *UpdateQuery) Built() (any, *AccessClaim, uuid.UUID) {
	if q.recordID == uuid.Nil {
		q.recordID = recordIDFromModel(q.db, q.model)
	}
	return q.model, q.accessClaim, q.recordID
}

func (q *DeleteQuery) Built() (any, *AccessClaim, uuid.UUID) {
	if q.recordID == uuid.Nil {
		q.recordID = recordIDFromModel(q.db, q.model)
	}
	return q.model, q.accessClaim, q.recordID
}

type claimBindingEnvelope struct {
	ID         *uuid.UUID `json:"id"`
	ClaimID    *uuid.UUID `json:"claim_id"`
	UserID     *uuid.UUID `json:"user_id"`
	ObjectType *string    `json:"object_type"`
	ObjectID   *uuid.UUID `json:"object_id"`
}

func ClaimAreas(eventType string, raw json.RawMessage) ([]uuid.UUID, error) {
	var envelope claimBindingEnvelope
	switch eventType {
	case "claimius.claim":
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("unmarshal claim envelope: %w", err)
		}
		envelope.ClaimID = envelope.ID
	case "claimius.claim_object", "claimius.user_claim":
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("unmarshal claim binding envelope: %w", err)
		}
	default:
		return nil, nil
	}

	areas := make([]uuid.UUID, 0, 2)
	if eventType == "claimius.claim_object" && envelope.ObjectID != nil &&
		envelope.ObjectType != nil && *envelope.ObjectType != "claimius.claim" {
		return areas, nil
	}
	if envelope.ClaimID != nil {
		areas = append(areas, *envelope.ClaimID)
	}
	if eventType == "claimius.claim_object" &&
		envelope.ObjectType != nil && *envelope.ObjectType == "claimius.claim" &&
		envelope.ObjectID != nil {
		areas = append(areas, *envelope.ObjectID)
	}
	return areas, nil
}

func RecomputeExternal(ctx context.Context, tx bun.IConn, eventType string, raw json.RawMessage) error {
	areas, err := ClaimAreas(eventType, raw)
	if err != nil {
		return err
	}
	var assignee *uuid.UUID
	if eventType == "claimius.user_claim" || eventType == "claimius.claim_object" {
		var envelope claimBindingEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("unmarshal binding envelope: %w", err)
		}
		if eventType == "claimius.user_claim" {
			assignee = envelope.UserID
		} else if envelope.ObjectID != nil && envelope.ObjectType != nil && *envelope.ObjectType != "claimius.claim" {
			if _, err := tx.ExecContext(ctx, "SELECT claimius._raise_object_diff(?, ?, TRUE)", *envelope.ObjectType, *envelope.ObjectID); err != nil {
				return fmt.Errorf("raise object diff for %s: %w", *envelope.ObjectID, err)
			}
		}
	}
	for _, claimID := range areas {
		if _, err := tx.ExecContext(ctx, "SELECT claimius._raise_claim_diff(?, ?)", claimID, assignee); err != nil {
			return fmt.Errorf("raise claim diff for %s: %w", claimID, err)
		}
	}
	return nil
}
