package shared

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/nimling/nimtel"
	"github.com/uptrace/bun"
)

func (d *DB) GetAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	if userID == AnonymousUserID {
		return d.getGuestAccess(ctx, objectID, objectType, requiredAccess)
	}
	if objectType == OBJECT_TYPE_USER && userID == objectID {
		return selfAccessClaim(userID, requiredAccess), nil
	}
	claims := make([]AccessClaim, 0)
	err := d.prepared(ctx, d.DB, &claims, "SELECT * FROM claimius.get_access($1::uuid, $2::uuid, $3::text, $4::int)", userID, objectID, objectType, requiredAccess)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoAccess
		}
		return nil, ErrInternalServer
	}
	if len(claims) == 0 {
		return nil, ErrNoAccess
	}
	d.Usage.Add(nil, userID, objectType, objectID)
	nimtel.Organization(ctx, claims[0].RootID, claims[0].OwnerID)
	claims[0].UserID = userID
	return &claims[0], nil
}

func (d *DB) GetDirectAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string) (*AccessClaim, error) {
	if objectType == OBJECT_TYPE_USER && userID == objectID {
		return selfAccessClaim(userID, ACCESS_ALL), nil
	}
	claims := make([]AccessClaim, 0)
	err := d.prepared(ctx, d.DB, &claims, "SELECT * FROM claimius.get_direct_access($1::uuid, $2::uuid, $3::text)", userID, objectID, objectType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoAccess
		}
		return nil, ErrInternalServer
	}
	if len(claims) == 0 {
		return nil, ErrNoAccess
	}
	nimtel.Organization(ctx, claims[0].RootID, claims[0].OwnerID)
	claims[0].UserID = userID
	return &claims[0], nil
}

func (d *DB) GetClaim(ctx context.Context, userID, claimID uuid.UUID, requiredAccess ClaimAccess) (*Claim, error) {
	var claim Claim
	err := d.GetClaimQuery(&claim, userID, claimID, requiredAccess).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

func (d *DB) GetClaimQuery(claim *Claim, userID, claimID uuid.UUID, requiredAccess ClaimAccess) *bun.SelectQuery {
	return d.NewSelect().
		Model(claim).
		Where("claim.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		Where("claim.id = ?", claimID).
		Where("EXISTS (SELECT 1 FROM claimius.get_access(?,claim.id, ?, ?))", userID, OBJECT_TYPE_CLAIM, requiredAccess).
		Limit(1)
}

func (d *DB) GetClaims(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]Claim, error) {
	claims := make([]Claim, 0)
	err := d.GetClaimsQuery(&claims, userID, requiredAccess).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (d *DB) GetClaimsQuery(claims *[]Claim, userID uuid.UUID, requiredAccess ClaimAccess) *bun.SelectQuery {
	return d.NewSelect().
		Model(claims).
		Where("claim.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		Where(ClaimWhere(userID, requiredAccess))
}

func (d *DB) GetClaimAudit(ctx context.Context, actorID, claimID uuid.UUID, requiredAccess ClaimAccess) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_audit(?,?) AS audit", actorID, requiredAccess).
		Column("audit.*").
		Where("audit.object_type = ?", OBJECT_TYPE_CLAIM).
		Where("audit.object_id = ?", claimID).
		OrderExpr("audit.sa_created_at DESC").
		Scan(ctx, &audits)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

func (d *DB) Search(ctx context.Context, userID uuid.UUID, query string, objectTypes []string, requiredAccess ClaimAccess, limit int, offset int) ([]SearchResult, error) {
	results := make([]SearchResult, 0)
	err := d.NewRaw(
		`SELECT * FROM claimius.search(?,?, ?::text[], ?, ?, ?)`,
		userID, query, pq.Array(objectTypes), requiredAccess, limit, offset,
	).Scan(ctx, &results)
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (d *DB) GetAudit(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess, ids []uuid.UUID) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_audit(?, ?, p_ids => ?::uuid[]) AS audit", userID, requiredAccess, pq.Array(ids)).
		Column("audit.*").
		Scan(ctx, &audits)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

func (d *DB) HasAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) bool {
	claim, err := d.GetAccess(ctx, userID, objectID, objectType, requiredAccess)
	return err == nil && claim != nil
}

func (d *DB) GetObjectClaims(ctx context.Context, userID, objectID uuid.UUID, objectType string) ([]Claim, error) {
	claims := make([]Claim, 0)
	err := d.NewSelect().
		Model(&claims).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		Where("claim.id IN (SELECT id FROM claimius.get_claims(?,?, ?))", userID, objectID, objectType).
		Scan(ctx)
	return claims, err
}

type GraphWindow struct {
	Width  int
	Sort   string
	NextID *uuid.UUID
}

const (
	GRAPH_SORT_NAME  = "name"
	GRAPH_SORT_USAGE = "usage"
)

func (w GraphWindow) sort() string {
	if w.Sort == "" {
		return GRAPH_SORT_NAME
	}
	return w.Sort
}

func (d *DB) GetOwnerGraph(ctx context.Context, userID uuid.UUID, startID *uuid.UUID, depth int, requiredAccess ClaimAccess, window GraphWindow) ([]GraphNode, error) {
	nodes := make([]GraphNode, 0)
	err := d.GetOwnerGraphQuery(userID, startID, depth, requiredAccess, window).Scan(ctx, &nodes)
	return nodes, err
}

func (d *DB) GetOwnerGraphQuery(userID uuid.UUID, startID *uuid.UUID, depth int, requiredAccess ClaimAccess, window GraphWindow) *bun.SelectQuery {
	return d.NewSelect().
		TableExpr("claimius.get_owner_graph(p_user_id := ?, p_start_id := ?, p_depth := ?, p_required_access := ?, p_width := ?, p_sort := ?, p_next_id := ?) AS graph",
			userID, startID, depth, requiredAccess, window.Width, window.sort(), window.NextID).
		Column("graph.*")
}

func (d *DB) GetClaimGraph(ctx context.Context, userID uuid.UUID, startID *uuid.UUID, depth int, requiredAccess ClaimAccess, window GraphWindow) ([]GraphNode, error) {
	nodes := make([]GraphNode, 0)
	err := d.GetClaimGraphQuery(userID, startID, depth, requiredAccess, window).Scan(ctx, &nodes)
	return nodes, err
}

func (d *DB) GetClaimGraphQuery(userID uuid.UUID, startID *uuid.UUID, depth int, requiredAccess ClaimAccess, window GraphWindow) *bun.SelectQuery {
	return d.NewSelect().
		TableExpr("claimius.get_claim_graph(p_user_id := ?, p_start_id := ?, p_depth := ?, p_required_access := ?, p_width := ?, p_sort := ?, p_next_id := ?) AS graph",
			userID, startID, depth, requiredAccess, window.Width, window.sort(), window.NextID).
		Column("graph.*")
}

func (d *DB) insertClaim(ctx context.Context, conn bun.IConn, ac *AccessClaim, claim *Claim) (*Claim, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	claim.CreatedBy = ac.UserClaimID
	claim.RootID = ac.RootID
	var payload []byte
	err := d.NewRaw(`
		SELECT (claimius.insert_claim(
			?, ?, ?, ?, ?, ?, ?, ?::claimius.claim_type, ?
		) -> 'claim')::jsonb
	`,
		claim.Name,
		claim.Description,
		claim.Access,
		claim.OwnerID,
		claim.RootID,
		claim.CreatedBy,
		claim.Inherits,
		string(claim.Type),
		claim.Level,
	).Conn(conn).Scan(ctx, &payload)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, claim); err != nil {
		return nil, err
	}
	return claim, nil
}

func (d *DB) updateClaim(ctx context.Context, conn bun.IConn, ac *AccessClaim, claimID uuid.UUID, claim *Claim) (*Claim, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var (
		name        interface{}
		description interface{}
		access      interface{}
		inherits    interface{}
		level       interface{}
		claimType   interface{}
	)
	if claim.HasKey("name") {
		name = claim.Name
	}
	if claim.HasKey("description") {
		description = claim.Description
	}
	if claim.HasKey("access") {
		access = claim.Access
	}
	if claim.HasKey("inherits") {
		inherits = claim.Inherits
	}
	if claim.HasKey("level") {
		level = claim.Level
	}
	if claim.HasKey("type") {
		claimType = string(claim.Type)
	}
	var payload []byte
	err := d.NewRaw(`
		SELECT (claimius.update_claim(?, ?, ?, ?, ?, ?, ?::claimius.claim_type, ?::uuid) -> 'claim')::jsonb
	`,
		claimID,
		name,
		description,
		access,
		inherits,
		level,
		claimType,
		ac.UserClaimID,
	).Conn(conn).Scan(ctx, &payload)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, claim); err != nil {
		return nil, err
	}
	return claim, nil
}

func (d *DB) deleteClaim(ctx context.Context, conn bun.IConn, ac *AccessClaim, claimID uuid.UUID) (*Claim, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	var claim Claim
	err := d.NewSelect().Conn(conn).
		Model(&claim).
		ModelTableExpr("claimius.delete_claim(?, ?) AS claim", claimID, *ac.UserClaimID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

func (d *DB) insertClaimObject(ctx context.Context, conn bun.IConn, ac *AccessClaim, claimID uuid.UUID, co *ClaimObject) (*ClaimObject, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	if co.OwnerID == uuid.Nil {
		co.OwnerID = ac.OwnerID
	}
	if co.RootID == uuid.Nil {
		co.RootID = ac.RootID
	}
	co.CreatedBy = ac.UserClaimID
	var payload []byte
	err := d.NewRaw(`
		SELECT (claimius.insert_claim_object(
			p_claim_id      := ?,
			p_sa_owner_id   := ?,
			p_sa_root_id    := ?,
			p_sa_created_by := ?,
			p_object_id     := ?,
			p_object_type   := ?,
			p_sa_access     := ?,
			p_inherits      := ?,
			p_reason        := ?,
			p_ref_id        := ?,
			p_scope         := ?
		) -> 'claim_object')::jsonb
	`,
		claimID,
		co.OwnerID,
		co.RootID,
		co.CreatedBy,
		co.ObjectID,
		co.ObjectType,
		co.Access,
		co.Inherits,
		co.Reason,
		co.RefID,
		co.Scope,
	).Conn(conn).Scan(ctx, &payload)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, co); err != nil {
		return nil, err
	}
	return co, nil
}

func (d *DB) deleteClaimObject(ctx context.Context, conn bun.IConn, ac *AccessClaim, claimID, bindingID uuid.UUID) (*ClaimObject, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	var rowID uuid.UUID
	err := d.NewSelect().Conn(conn).
		ColumnExpr("id").
		Table("claimius.claim_object").
		Where("claim_id = ?", claimID).
		Where("id = ?", bindingID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx, &rowID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var co ClaimObject
	err = d.NewSelect().Conn(conn).
		Model(&co).
		ModelTableExpr("claimius.delete_claim_object(?, ?) AS claim_object", rowID, *ac.UserClaimID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &co, nil
}

func (d *DB) insertClaimUser(ctx context.Context, conn bun.IConn, ac *AccessClaim, claimID uuid.UUID, uc *UserClaim) (*UserClaim, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	uc.OwnerID = ac.OwnerID
	uc.CreatedBy = *ac.UserClaimID
	var payload []byte
	err := d.NewRaw(`
		SELECT (claimius.insert_user_claim(
			?, ?, ?, ?, ?, ?, ?
		) -> 'user_claim')::jsonb
	`,
		claimID,
		uc.UserID,
		uc.OwnerID,
		uc.CreatedBy,
		uc.Reason,
		uc.StartsAt,
		uc.EndsAt,
	).Conn(conn).Scan(ctx, &payload)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, uc); err != nil {
		return nil, err
	}
	return uc, nil
}

func (d *DB) deleteClaimUser(ctx context.Context, conn bun.IConn, ac *AccessClaim, claimID, grantID uuid.UUID) (*UserClaim, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	var rowID uuid.UUID
	err := d.NewSelect().Conn(conn).
		ColumnExpr("id").
		Table("claimius.user_claim").
		Where("claim_id = ?", claimID).
		Where("id = ?", grantID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx, &rowID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var uc UserClaim
	err = d.NewSelect().Conn(conn).
		Model(&uc).
		ModelTableExpr("claimius.delete_user_claim(?, ?) AS user_claim", rowID, *ac.UserClaimID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &uc, nil
}

type CreateVirtualClaimParams struct {
	Name         string
	Description  *string
	Access       ClaimAccess
	OwnerID      uuid.UUID
	ObjectID     uuid.UUID
	ObjectType   string
	TargetUserID uuid.UUID
}

func (d *DB) CreateVirtualClaim(ctx context.Context, conn bun.IConn, ac *AccessClaim, p CreateVirtualClaimParams) (*AccessClaim, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	owner := *ac
	owner.OwnerID = p.OwnerID
	claim, err := d.CreateClaim(ctx, conn, &owner, CreateClaimParams{
		Name:        p.Name,
		Description: p.Description,
		Access:      p.Access,
		Type:        CLAIM_TYPE_VIRTUAL,
	})
	if err != nil {
		return nil, err
	}
	objectType := ObjectType(p.ObjectType)
	if _, err := claim.AssignObject(&ClaimObject{
		ObjectID:   &p.ObjectID,
		ObjectType: &objectType,
		Inherits:   true,
		OwnerID:    claim.OwnerID,
		RootID:     claim.RootID,
	}); err != nil {
		return nil, err
	}
	granted, err := claim.AssignUser(p.TargetUserID)
	if err != nil {
		return nil, err
	}
	if len(granted) == 0 {
		return nil, ErrNoAccess
	}
	return &AccessClaim{
		UserClaimID: &granted[0].ID,
		ClaimID:     &claim.ID,
		ClaimName:   &claim.Name,
		Access:      claim.Access,
		OwnerID:     claim.OwnerID,
		RootID:      claim.RootID,
	}, nil
}

type GrantFilters struct {
	IDs       []uuid.UUID
	UserIDs   []uuid.UUID
	ClaimIDs  []uuid.UUID
	OwnerIDs  []uuid.UUID
	NoClients bool
}

type BindingFilters struct {
	ObjectType ObjectType
	ObjectIDs  []uuid.UUID
	ClaimIDs   []uuid.UUID
}

func (d *DB) GetClaimsMany(ctx context.Context, ids, ownerIDs []uuid.UUID) ([]Claim, error) {
	claims := make([]Claim, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_claims(?::uuid[], ?::uuid[])", pq.Array(ids), pq.Array(ownerIDs)).Scan(ctx, &claims)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (d *DB) GetGrants(ctx context.Context, f GrantFilters) ([]UserClaim, error) {
	grants := make([]UserClaim, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_grants(?::uuid[], ?::uuid[], ?::uuid[], ?::uuid[], ?)",
		pq.Array(f.IDs), pq.Array(f.UserIDs), pq.Array(f.ClaimIDs), pq.Array(f.OwnerIDs), !f.NoClients).Scan(ctx, &grants)
	if err != nil {
		return nil, err
	}
	return grants, nil
}

func (d *DB) GetBindings(ctx context.Context, f BindingFilters) ([]ClaimObject, error) {
	var objectType *string
	if f.ObjectType != "" {
		t := string(f.ObjectType)
		objectType = &t
	}
	bindings := make([]ClaimObject, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_bindings(?::text, ?::uuid[], ?::uuid[])",
		objectType, pq.Array(f.ObjectIDs), pq.Array(f.ClaimIDs)).Scan(ctx, &bindings)
	if err != nil {
		return nil, err
	}
	return bindings, nil
}

func (d *DB) GetObjectTypes(ctx context.Context) ([]ObjectType, error) {
	types := make([]ObjectType, 0)
	err := d.NewRaw("SELECT object_type FROM claimius.get_object_types()").Scan(ctx, &types)
	if err != nil {
		return nil, err
	}
	return types, nil
}

type Descendant struct {
	ObjectType ObjectType `bun:"object_type" json:"object_type"`
	ObjectID   uuid.UUID  `bun:"object_id" json:"object_id"`
	Hop        int        `bun:"hop" json:"hop"`
	TreeType   string     `bun:"tree_type" json:"tree_type"`
}

func (d *DB) GetDescendants(ctx context.Context, objectID uuid.UUID) ([]Descendant, error) {
	rows := make([]Descendant, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_descendants(?::uuid)", objectID).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
