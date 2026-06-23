package shared

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/nimbody"
	"github.com/uptrace/bun"

	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/internal/store"
	"github.com/nimling/samna-auth-middleware/sauth"
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

func UserWhere(userID uuid.UUID, requiredAccess ClaimAccess) string {
	return fmt.Sprintf(
		`(samna_user.user_id = '%[1]s' OR samna_user.user_id IN (SELECT user_id FROM claimius.get_users('%[1]s', %[2]d))) AND samna_user.user_id NOT IN (SELECT user_id FROM claimius.samna_user WHERE type = 'system' AND sa_deleted_at IS NULL)`,
		userID, requiredAccess,
	)
}

func ClaimWhere(userID uuid.UUID, requiredAccess ClaimAccess) string {
	return fmt.Sprintf(
		`claim.id IN (SELECT id FROM claimius.get_claims('%[1]s', %[2]d))`,
		userID, requiredAccess,
	)
}

func ObjectWhere(userID uuid.UUID, objectType string, requiredAccess ClaimAccess) string {
	parts := strings.SplitN(objectType, ".", 2)
	table := parts[len(parts)-1]
	return fmt.Sprintf(
		`%[4]s.id IN (SELECT object_id FROM claimius.get_objects('%[1]s', '%[3]s', %[2]d))`,
		userID, requiredAccess, objectType, table,
	)
}

func ApplyWithAccess(userID uuid.UUID, objectType string, requiredAccess ClaimAccess) func(*bun.SelectQuery) *bun.SelectQuery {
	parts := strings.SplitN(objectType, ".", 2)
	table := parts[len(parts)-1]
	join := fmt.Sprintf(
		`JOIN claimius.get_objects('%[1]s', '%[2]s', %[3]d) ga ON ga.object_id = %[4]s.id`,
		userID, objectType, requiredAccess, table,
	)
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.
			Join(join).
			ColumnExpr("?TableAlias.*").
			ColumnExpr("ga.user_claim_id AS access_user_claim_id").
			ColumnExpr("ga.claim_id AS access_claim_id").
			ColumnExpr("ga.claim_name AS access_claim_name").
			ColumnExpr("ga.sa_access AS access_sa_access").
			ColumnExpr("ga.scope AS access_scope").
			ColumnExpr("ga.sa_owner_id AS access_sa_owner_id").
			ColumnExpr("ga.sa_root_id AS access_sa_root_id").
			ColumnExpr("ga.sa_location_id AS access_sa_location_id").
			ColumnExpr("ga.direct_grant AS access_direct_grant")
	}
}

func ApplyWithCreator(objectType string) func(*bun.SelectQuery) *bun.SelectQuery {
	parts := strings.SplitN(objectType, ".", 2)
	table := parts[len(parts)-1]
	creatorJoin := fmt.Sprintf(`LEFT JOIN claimius.user_claim AS creator_uc ON creator_uc.id = %s.sa_created_by`, table)
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.
			ColumnExpr("?TableAlias.*").
			Join(creatorJoin).
			Join(`LEFT JOIN claimius.samna_user AS creator_su ON creator_su.user_id = creator_uc.user_id`).
			ColumnExpr("creator_su.id AS creator_id").
			ColumnExpr("creator_su.user_id AS creator_user_id").
			ColumnExpr("creator_su.first_name AS creator_first_name").
			ColumnExpr("creator_su.last_name AS creator_last_name").
			ColumnExpr("creator_su.user_name AS creator_user_name").
			ColumnExpr("creator_su.user_image AS creator_user_image").
			ColumnExpr("creator_su.phone AS creator_phone").
			ColumnExpr("creator_su.email AS creator_email").
			ColumnExpr("creator_su.external_id AS creator_external_id").
			ColumnExpr("creator_su.status AS creator_status").
			ColumnExpr("creator_su.sa_created_at AS creator_created_at").
			ColumnExpr("creator_su.sa_updated_at AS creator_updated_at")
	}
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
	OBJECT_TYPE_CLAIM:        {base: "claim"},
	OBJECT_TYPE_CLAIM_OBJECT: {base: "object", parents: []string{"claim"}},
	OBJECT_TYPE_USER_CLAIM:   {base: "user", parents: []string{"claim"}},
	OBJECT_TYPE_USER:         {base: "user"},
	OBJECT_TYPE_ORGANIZATION: {base: "organization"},
	OBJECT_TYPE_LOCATION:     {base: "location"},
	OBJECT_TYPE_SAMNA_CLIENT: {base: "samna_client"},
	OBJECT_TYPE_SAMNA_SECRET: {base: "secret"},
}

func endpointFor(model any, recordID uuid.UUID) (string, error) {
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

func whereID(expr string, args []any) (uuid.UUID, bool) {
	clean := strings.ToLower(strings.TrimSpace(expr))
	if !strings.HasPrefix(clean, "id =") && !strings.HasPrefix(clean, "user_id =") {
		return uuid.Nil, false
	}
	if len(args) == 0 {
		return uuid.Nil, false
	}
	switch v := args[0].(type) {
	case uuid.UUID:
		return v, true
	case *uuid.UUID:
		if v != nil {
			return *v, true
		}
	case string:
		if parsed, err := uuid.Parse(v); err == nil {
			return parsed, true
		}
	}
	return uuid.Nil, false
}

func dispatchScan(src any, dest ...any) error {
	for _, d := range dest {
		if d == nil {
			continue
		}
		if err := copyInto(src, d); err != nil {
			return err
		}
	}
	return nil
}

func copyInto(src, dst any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

type InsertQuery struct {
	db          *DB
	model       any
	accessClaim *AccessClaim
	excludes    []string
	conflict    string
	confSet     []conflictSet
	returns     string
}

func (d *DB) NewInsert() *InsertQuery {
	return &InsertQuery{db: d}
}

func (q *InsertQuery) Model(model any) *InsertQuery {
	q.model = model
	return q
}

func (q *InsertQuery) With(ac *AccessClaim) *InsertQuery {
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
	bq := q.db.DB.NewInsert().Model(q.model)
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
	if writesLocally() {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return nil, err
		}
		return q.buildBun().Exec(ctx, dest...)
	}
	return q.execHTTP(ctx, dest...)
}

func (q *InsertQuery) Scan(ctx context.Context, dest ...any) error {
	if writesLocally() {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return err
		}
		return q.buildBun().Scan(ctx, dest...)
	}
	_, err := q.execHTTP(ctx, dest...)
	return err
}

func (q *InsertQuery) execClaimius(ctx context.Context, dest ...any) (bool, error) {
	switch modelTableName(q.db, q.model) {
	case "claimius.claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Claim{})).(*Claim)
		if !ok {
			return false, nil
		}
		result, err := q.db.insertClaim(ctx, q.accessClaim, m)
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
		result, err := q.db.insertClaimObject(ctx, q.accessClaim, m.ClaimID, m)
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
		result, err := q.db.insertClaimUser(ctx, q.accessClaim, m.ClaimID, m)
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
		result, err := q.db.InsertUser(ctx, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.user_token":
		return true, fmt.Errorf("user_token creation reserved; use IssueUserToken")
	case "claimius.organization":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Organization{})).(*Organization)
		if !ok {
			return false, nil
		}
		result, err := q.db.insertOrganization(ctx, q.accessClaim, m)
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
		result, err := q.db.insertLocation(ctx, q.accessClaim, m)
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
		result, err := q.db.insertRelation(ctx, q.accessClaim, m)
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
		result, err := q.db.insertSecret(ctx, q.accessClaim, body)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	}
	return false, nil
}

func (q *InsertQuery) execHTTP(ctx context.Context, dest ...any) (sql.Result, error) {
	path, err := endpointFor(q.model, uuid.Nil)
	if err != nil {
		return nil, err
	}
	stampAccessClaim(q.model, q.accessClaim)
	if err := stampRoot(ctx, q.db, q.model, q.accessClaim); err != nil {
		return nil, err
	}
	body, err := sauth.Marshal(q.model)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := client.Post(ctx, path, json.RawMessage(body), &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := sauth.Unmarshal(raw, q.model); err != nil {
			return nil, fmt.Errorf("decode insert response: %w", err)
		}
		if err := store.Upsert(ctx, q.db.DB, q.model); err != nil {
			return nil, err
		}
	}
	for _, d := range dest {
		if d == nil {
			continue
		}
		if err := copyInto(q.model, d); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func stampAccessClaim(model any, ac *AccessClaim) {
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

func stampRoot(ctx context.Context, db *DB, model any, ac *AccessClaim) error {
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
	if !f.IsValid() || !f.CanSet() || f.Type() != reflect.TypeOf(uuid.UUID{}) {
		return
	}
	if onlyIfZero && f.Interface().(uuid.UUID) != uuid.Nil {
		return
	}
	f.Set(reflect.ValueOf(val))
}

func boundBody(model any) map[string]any {
	if b, ok := model.(interface {
		Validator() *nimbody.BodyValidator
	}); ok {
		if bv := b.Validator(); bv != nil {
			return bv.Body
		}
	}
	return nil
}

type UpdateQuery struct {
	db          *DB
	model       any
	accessClaim *AccessClaim
	recordID    uuid.UUID
	excludes    []string
	columns     []string
	omitZero    bool
	returns     string
	setCols     []conflictSet
	whereExpr   []conflictSet
}

func (d *DB) NewUpdate() *UpdateQuery {
	return &UpdateQuery{db: d}
}

func (q *UpdateQuery) Model(model any) *UpdateQuery {
	q.model = model
	return q
}

func (q *UpdateQuery) With(ac *AccessClaim) *UpdateQuery {
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
	if q.recordID == uuid.Nil {
		if id, ok := whereID(expr, args); ok {
			q.recordID = id
		}
	}
	return q
}

func (q *UpdateQuery) buildBun() *bun.UpdateQuery {
	bq := q.db.DB.NewUpdate().Model(q.model)
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
	if writesLocally() {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return nil, err
		}
		return q.buildBun().Exec(ctx, dest...)
	}
	return q.execHTTP(ctx, dest...)
}

func (q *UpdateQuery) Scan(ctx context.Context, dest ...any) error {
	if writesLocally() {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return err
		}
		return q.buildBun().Scan(ctx, dest...)
	}
	_, err := q.execHTTP(ctx, dest...)
	return err
}

func (q *UpdateQuery) execClaimius(ctx context.Context, dest ...any) (bool, error) {
	if q.recordID == uuid.Nil {
		return false, nil
	}
	switch modelTableName(q.db, q.model) {
	case "claimius.claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Claim{})).(*Claim)
		if !ok {
			return false, nil
		}
		result, err := q.db.updateClaim(ctx, q.accessClaim, q.recordID, m)
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
		result, err := q.db.updateUser(ctx, q.accessClaim, q.recordID, m)
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
		result, err := q.db.updateOrganization(ctx, q.accessClaim, q.recordID, m)
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
		result, err := q.db.updateLocation(ctx, q.accessClaim, q.recordID, m)
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
		result, err := q.db.updateRelation(ctx, q.accessClaim, q.recordID, m)
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
		result, err := q.db.updateSecret(ctx, q.accessClaim, q.recordID, body)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	}
	return false, nil
}

func (q *UpdateQuery) execHTTP(ctx context.Context, dest ...any) (sql.Result, error) {
	if q.recordID == uuid.Nil {
		return nil, fmt.Errorf("update: record id required (use .ID(id) or .Where(\"id = ?\", id))")
	}
	path, err := endpointFor(q.model, q.recordID)
	if err != nil {
		return nil, err
	}
	var payload any
	if bound := boundBody(q.model); bound != nil {
		payload = bound
	} else {
		body, err := sauth.Marshal(q.model)
		if err != nil {
			return nil, err
		}
		payload = json.RawMessage(body)
	}
	var raw json.RawMessage
	if err := client.Patch(ctx, path, payload, &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := sauth.Unmarshal(raw, q.model); err != nil {
			return nil, fmt.Errorf("decode update response: %w", err)
		}
		if err := store.Upsert(ctx, q.db.DB, q.model); err != nil {
			return nil, err
		}
	}
	for _, d := range dest {
		if d == nil {
			continue
		}
		if err := copyInto(q.model, d); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

type DeleteQuery struct {
	db          *DB
	model       any
	accessClaim *AccessClaim
	recordID    uuid.UUID
	whereExpr   []conflictSet
	returns     string
}

func (d *DB) NewDelete() *DeleteQuery {
	return &DeleteQuery{db: d}
}

func (q *DeleteQuery) Model(model any) *DeleteQuery {
	q.model = model
	return q
}

func (q *DeleteQuery) With(ac *AccessClaim) *DeleteQuery {
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
	if q.recordID == uuid.Nil {
		if id, ok := whereID(expr, args); ok {
			q.recordID = id
		}
	}
	return q
}

func (q *DeleteQuery) buildBun() *bun.DeleteQuery {
	bq := q.db.DB.NewDelete().Model(q.model)
	if q.returns != "" {
		bq = bq.Returning(q.returns)
	}
	for _, w := range q.whereExpr {
		bq = bq.Where(w.expr, w.args...)
	}
	return bq
}

func (q *DeleteQuery) Exec(ctx context.Context, dest ...any) (sql.Result, error) {
	if writesLocally() {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return nil, err
		}
		return q.buildBun().Exec(ctx, dest...)
	}
	return q.execHTTP(ctx, dest...)
}

func (q *DeleteQuery) Scan(ctx context.Context, dest ...any) error {
	if writesLocally() {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return err
		}
		return q.buildBun().Scan(ctx, dest...)
	}
	_, err := q.execHTTP(ctx, dest...)
	return err
}

func (q *DeleteQuery) execClaimius(ctx context.Context, dest ...any) (bool, error) {
	if q.recordID == uuid.Nil {
		return false, nil
	}
	switch modelTableName(q.db, q.model) {
	case "claimius.claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Claim{})).(*Claim)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteClaim(ctx, q.accessClaim, q.recordID)
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
		result, err := q.db.deleteClaimObject(ctx, q.accessClaim, m.ClaimID, q.recordID)
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
		result, err := q.db.deleteClaimUser(ctx, q.accessClaim, m.ClaimID, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_user":
		return true, fmt.Errorf("samna_user deletion reserved; use DeleteUser")
	case "claimius.organization":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Organization{})).(*Organization)
		if !ok {
			return false, nil
		}
		result, err := q.db.deleteOrganization(ctx, q.accessClaim, q.recordID)
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
		result, err := q.db.deleteLocation(ctx, q.accessClaim, q.recordID)
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
		result, err := q.db.deleteRelation(ctx, q.accessClaim, m.UserID, q.recordID)
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
		result, err := q.db.deleteSecret(ctx, q.accessClaim, q.recordID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	}
	return false, nil
}

func (q *DeleteQuery) execHTTP(ctx context.Context, dest ...any) (sql.Result, error) {
	if q.recordID == uuid.Nil {
		return nil, fmt.Errorf("delete: record id required (use .ID(id) or .Where(\"id = ?\", id))")
	}
	path, err := endpointFor(q.model, q.recordID)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := client.Delete(ctx, path, &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := sauth.Unmarshal(raw, q.model); err != nil {
			return nil, fmt.Errorf("decode delete response: %w", err)
		}
		if err := store.Upsert(ctx, q.db.DB, q.model); err != nil {
			return nil, err
		}
	}
	for _, d := range dest {
		if d == nil {
			continue
		}
		if err := copyInto(q.model, d); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
