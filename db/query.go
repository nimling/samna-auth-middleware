package db

import (
	"context"
	"database/sql"
	"encoding/hex"
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
		`(samna_user.user_id = '%[1]s' OR samna_user.user_id IN (SELECT user_id FROM claimius.get_users('%[1]s', claimius.get_disciple_app_id(), %[2]d))) AND samna_user.user_id NOT IN (SELECT user_id FROM claimius.samna_user WHERE type = 'system' AND sa_deleted_at IS NULL)`,
		userID, requiredAccess,
	)
}

func ClaimWhere(userID uuid.UUID, requiredAccess ClaimAccess) string {
	return fmt.Sprintf(
		`claim.id IN (SELECT id FROM claimius.get_claims('%[1]s', claimius.get_disciple_app_id(), %[2]d))`,
		userID, requiredAccess,
	)
}

func ObjectWhere(userID uuid.UUID, objectType string, requiredAccess ClaimAccess) string {
	parts := strings.SplitN(objectType, ".", 2)
	table := parts[len(parts)-1]
	return fmt.Sprintf(
		`%[4]s.id IN (SELECT object_id FROM claimius.get_objects('%[1]s', claimius.get_disciple_app_id(), '%[3]s', %[2]d))`,
		userID, requiredAccess, objectType, table,
	)
}

func AuditWhere(userID uuid.UUID, requiredAccess ClaimAccess) string {
	return fmt.Sprintf(
		`audit.id IN (SELECT id FROM claimius.get_audit('%[1]s', claimius.get_disciple_app_id(), %[2]d))`,
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

func endpointFor(model any) (string, error) {
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
	if baseID := baseIDFromModel(model, otype); baseID != uuid.Nil {
		parts = append(parts, baseID.String())
	}
	return strings.Join(parts, "/"), nil
}

func baseIDFromModel(model any, otype ObjectType) uuid.UUID {
	switch otype {
	case OBJECT_TYPE_CLAIM:
		if m, ok := embeddedPtr(model, reflect.TypeOf(Claim{})).(*Claim); ok && m != nil {
			return m.ID
		}
	case OBJECT_TYPE_CLAIM_OBJECT:
		if m, ok := embeddedPtr(model, reflect.TypeOf(ClaimObject{})).(*ClaimObject); ok && m != nil && m.ObjectID != nil {
			return *m.ObjectID
		}
	case OBJECT_TYPE_USER_CLAIM:
		if m, ok := embeddedPtr(model, reflect.TypeOf(UserClaim{})).(*UserClaim); ok && m != nil {
			return m.UserID
		}
	case OBJECT_TYPE_USER:
		if m, ok := embeddedPtr(model, reflect.TypeOf(SamnaUser{})).(*SamnaUser); ok && m != nil {
			return m.UserID
		}
	case OBJECT_TYPE_ORGANIZATION:
		if m, ok := embeddedPtr(model, reflect.TypeOf(Organization{})).(*Organization); ok && m != nil {
			return m.ID
		}
	case OBJECT_TYPE_LOCATION:
		if m, ok := embeddedPtr(model, reflect.TypeOf(Location{})).(*Location); ok && m != nil {
			return m.ID
		}
	case OBJECT_TYPE_SAMNA_SECRET:
		if m, ok := embeddedPtr(model, reflect.TypeOf(SamnaSecret{})).(*SamnaSecret); ok && m != nil {
			return m.ID
		}
	}
	return uuid.Nil
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
	if GetDebugMode() != DebugModeNone {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return nil, err
		}
		return q.buildBun().Exec(ctx, dest...)
	}
	return q.execHTTP(ctx, dest...)
}

func (q *InsertQuery) Scan(ctx context.Context, dest ...any) error {
	if GetDebugMode() != DebugModeNone {
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
		return true, fmt.Errorf("user_token creation reserved; use UpsertUserToken")
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
	path, err := endpointFor(q.model)
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
	if GetDebugMode() != DebugModeNone {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return nil, err
		}
		return q.buildBun().Exec(ctx, dest...)
	}
	return q.execHTTP(ctx, dest...)
}

func (q *UpdateQuery) Scan(ctx context.Context, dest ...any) error {
	if GetDebugMode() != DebugModeNone {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return err
		}
		return q.buildBun().Scan(ctx, dest...)
	}
	_, err := q.execHTTP(ctx, dest...)
	return err
}

func (q *UpdateQuery) execClaimius(ctx context.Context, dest ...any) (bool, error) {
	switch modelTableName(q.db, q.model) {
	case "claimius.claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Claim{})).(*Claim)
		if !ok {
			return false, nil
		}
		if m.ID == uuid.Nil {
			return true, fmt.Errorf("update claim: id required on model")
		}
		result, err := q.db.updateClaim(ctx, q.accessClaim, m.ID, m)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_secret":
		if err := q.encryptSecretValue(ctx); err != nil {
			return true, err
		}
		if err := q.runBunUpdate(ctx); err != nil {
			return true, err
		}
		return true, dispatchScan(q.model, dest...)
	case "claimius.samna_user", "claimius.organization", "claimius.location", "claimius.user_relation":
		if err := q.runBunUpdate(ctx); err != nil {
			return true, err
		}
		return true, dispatchScan(q.model, dest...)
	}
	return false, nil
}

func (q *UpdateQuery) runBunUpdate(ctx context.Context) error {
	if q.accessClaim == nil {
		return ErrNoAccess
	}
	bq := q.buildBun()
	if nuller, ok := q.model.(interface{ PatchNulls() []string }); ok {
		for _, column := range nuller.PatchNulls() {
			bq = bq.Set("? = NULL", bun.Ident(column))
		}
	}
	_, err := bq.Exec(ctx, q.model)
	return err
}

func (q *UpdateQuery) encryptSecretValue(ctx context.Context) error {
	m, ok := embeddedPtr(q.model, reflect.TypeOf(SamnaSecret{})).(*SamnaSecret)
	if !ok || m.Value == "" {
		return nil
	}
	app, err := q.db.GetApp(ctx)
	if err != nil {
		return err
	}
	seed, err := hex.DecodeString(app.PrivateSeed)
	if err != nil {
		return fmt.Errorf("decode app seed: %w", err)
	}
	encrypted, err := aesEncrypt(m.Value, seed)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	m.Value = hex.EncodeToString(encrypted)
	return nil
}

func (q *UpdateQuery) execHTTP(ctx context.Context, dest ...any) (sql.Result, error) {
	path, err := endpointFor(q.model)
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

func (q *DeleteQuery) Returning(s string) *DeleteQuery {
	q.returns = s
	return q
}

func (q *DeleteQuery) Where(expr string, args ...any) *DeleteQuery {
	q.whereExpr = append(q.whereExpr, conflictSet{expr: expr, args: args})
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
	if GetDebugMode() != DebugModeNone {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return nil, err
		}
		return q.buildBun().Exec(ctx, dest...)
	}
	return q.execHTTP(ctx, dest...)
}

func (q *DeleteQuery) Scan(ctx context.Context, dest ...any) error {
	if GetDebugMode() != DebugModeNone {
		if handled, err := q.execClaimius(ctx, dest...); handled {
			return err
		}
		return q.buildBun().Scan(ctx, dest...)
	}
	_, err := q.execHTTP(ctx, dest...)
	return err
}

func (q *DeleteQuery) execClaimius(ctx context.Context, dest ...any) (bool, error) {
	switch modelTableName(q.db, q.model) {
	case "claimius.claim":
		m, ok := embeddedPtr(q.model, reflect.TypeOf(Claim{})).(*Claim)
		if !ok {
			return false, nil
		}
		if m.ID == uuid.Nil {
			return true, fmt.Errorf("delete claim: id required on model")
		}
		result, err := q.db.deleteClaim(ctx, q.accessClaim, m.ID)
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
		if m.ClaimID == uuid.Nil || m.ObjectID == nil || *m.ObjectID == uuid.Nil {
			return true, fmt.Errorf("delete claim_object: claim_id and object_id required on model")
		}
		result, err := q.db.deleteClaimObject(ctx, q.accessClaim, m.ClaimID, *m.ObjectID)
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
		if m.ClaimID == uuid.Nil || m.UserID == uuid.Nil {
			return true, fmt.Errorf("delete user_claim: claim_id and user_id required on model")
		}
		result, err := q.db.deleteClaimUser(ctx, q.accessClaim, m.ClaimID, m.UserID)
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
		if m.ID == uuid.Nil {
			return true, fmt.Errorf("delete organization: id required on model")
		}
		result, err := q.db.deleteOrganization(ctx, q.accessClaim, m.ID)
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
		if m.ID == uuid.Nil {
			return true, fmt.Errorf("delete location: id required on model")
		}
		result, err := q.db.deleteLocation(ctx, q.accessClaim, m.ID)
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
		if m.ID == uuid.Nil {
			return true, fmt.Errorf("delete secret: id required on model")
		}
		result, err := q.db.deleteSecret(ctx, q.accessClaim, m.ID)
		if err != nil {
			return true, err
		}
		*m = *result
		return true, dispatchScan(q.model, dest...)
	}
	return false, nil
}

func (q *DeleteQuery) execHTTP(ctx context.Context, dest ...any) (sql.Result, error) {
	path, err := endpointFor(q.model)
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
