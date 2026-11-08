package echo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun/dialect/pgdialect"
)

type BodySearch struct {
	Query              *string      `json:"q" query:"q"`
	ExcludeQuery       *string      `json:"exclude_query" query:"exclude_query"`
	ObjectTypes        []ObjectType `json:"object_types" query:"object_types"`
	ExcludeObjectTypes []ObjectType `json:"exclude_object_types" query:"exclude_object_types"`
	OrgIDs             []uuid.UUID  `json:"org_ids" query:"org_ids"`
	ExcludeOrgIDs      []uuid.UUID  `json:"exclude_org_ids" query:"exclude_org_ids"`
	LocIDs             []uuid.UUID  `json:"loc_ids" query:"loc_ids"`
	ExcludeLocIDs      []uuid.UUID  `json:"exclude_loc_ids" query:"exclude_loc_ids"`
	UsrIDs             []uuid.UUID  `json:"usr_ids" query:"usr_ids"`
	ExcludeUsrIDs      []uuid.UUID  `json:"exclude_usr_ids" query:"exclude_usr_ids"`
	ClaimID            *uuid.UUID   `json:"claim_id" query:"claim_id"`
	OwnerIDs           []uuid.UUID  `json:"owner_ids" query:"owner_ids"`
	ParentID           *uuid.UUID   `json:"parent_id" query:"parent_id"`
	ParentType         *string      `json:"parent_type" query:"parent_type"`
	IDs                []uuid.UUID  `json:"ids" query:"ids"`
	ExcludeIDs         []uuid.UUID  `json:"exclude_ids" query:"exclude_ids"`
	With               []string     `json:"with" query:"with"`
	Format             *string      `json:"format" query:"format"`
	Limit              *int         `json:"limit" query:"limit"`
	Access             *int         `json:"access" query:"access"`
}

type searchWithSet struct {
	organization bool
	location     bool
	app          bool
}

func (s searchWithSet) any() bool {
	return s.organization || s.location || s.app
}

func searchParseWith(values []string) (searchWithSet, error) {
	var s searchWithSet
	for _, v := range values {
		for _, raw := range strings.Split(v, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			switch strings.ToLower(name) {
			case "org", "organization":
				s.organization = true
			case "loc", "location":
				s.location = true
			case "app", "samna_app":
				s.app = true
			default:
				return searchWithSet{}, fmt.Errorf("unsupported with object: %s", name)
			}
		}
	}
	return s, nil
}

func (b *BodySearch) UnmarshalJSON(data []byte) error {
	type alias BodySearch
	aux := &struct {
		QueryAlt *string `json:"query"`
		*alias
	}{
		alias: (*alias)(b),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if b.Query == nil && aux.QueryAlt != nil {
		b.Query = aux.QueryAlt
	}
	return nil
}

type ResponseSearch struct {
	ID            uuid.UUID        `json:"id" bun:"id"`
	ObjectType    ObjectType       `json:"object_type" bun:"object_type"`
	UserClaimID   *uuid.UUID       `json:"user_claim_id" bun:"user_claim_id"`
	Name          *string          `json:"name" bun:"name"`
	Description   *string          `json:"description" bun:"description"`
	Link          *string          `json:"link" bun:"link"`
	Access        int              `json:"access" bun:"sa_access"`
	Scope         *json.RawMessage `json:"scope,omitempty" bun:"scope,type:jsonb"`
	OwnerID       *uuid.UUID       `json:"owner_id" bun:"owner_id"`
	LocationID    *uuid.UUID       `json:"location_id" bun:"location_id"`
	IsDirectGrant bool             `json:"is_direct_grant" bun:"is_direct_grant"`
	Rank          float32          `json:"rank" bun:"rank"`
	TotalCount    int              `json:"-" bun:"total_count"`
}

func PostSearch(ctx echo.Context) error {
	if _, err := utils.ValidateQueryParams(ctx, true, nil); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	var req BodySearch
	if err := ctx.Bind(&req); err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusBadRequest, "Invalid JSON body")
	}
	return searchRun(ctx, &req)
}

func GetSearch(ctx echo.Context) error {
	if _, err := utils.ValidateQueryParams(ctx, true, []string{"q|query", "exclude_query", "object_types", "exclude_object_types", "org_id", "exclude_org_id", "loc_id", "exclude_loc_id", "usr_id", "exclude_usr_id", "claim_id", "owner_id", "parent_id", "parent_type", "ids", "exclude_ids", "with", "format", "access"}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	req, err := searchBody(ctx)
	if err != nil {
		ctx.Logger().Warnf("failed to parse search request: %v", err)
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	return searchRun(ctx, req)
}

func searchBody(ctx echo.Context) (*BodySearch, error) {
	params := &utils.QueryParams{Values: ctx.QueryParams()}
	req := BodySearch{}

	if v := params.Get("q"); v != "" {
		req.Query = &v
	} else if v := params.Get("query"); v != "" {
		req.Query = &v
	}
	if v := params.Get("exclude_query"); v != "" {
		req.ExcludeQuery = &v
	}
	for _, s := range params.GetSlice("object_types") {
		ot, err := ResolveObjectType(s)
		if err != nil {
			return nil, err
		}
		req.ObjectTypes = append(req.ObjectTypes, ot)
	}
	for _, s := range params.GetSlice("exclude_object_types") {
		ot, err := ResolveObjectType(s)
		if err != nil {
			return nil, err
		}
		req.ExcludeObjectTypes = append(req.ExcludeObjectTypes, ot)
	}
	for _, s := range params.GetSlice("org_id") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("org_id: %w", err)
		}
		req.OrgIDs = append(req.OrgIDs, id)
	}
	for _, s := range params.GetSlice("exclude_org_id") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("exclude_org_id: %w", err)
		}
		req.ExcludeOrgIDs = append(req.ExcludeOrgIDs, id)
	}
	for _, s := range params.GetSlice("loc_id") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("loc_id: %w", err)
		}
		req.LocIDs = append(req.LocIDs, id)
	}
	for _, s := range params.GetSlice("exclude_loc_id") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("exclude_loc_id: %w", err)
		}
		req.ExcludeLocIDs = append(req.ExcludeLocIDs, id)
	}
	for _, s := range params.GetSlice("usr_id") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("usr_id: %w", err)
		}
		req.UsrIDs = append(req.UsrIDs, id)
	}
	for _, s := range params.GetSlice("exclude_usr_id") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("exclude_usr_id: %w", err)
		}
		req.ExcludeUsrIDs = append(req.ExcludeUsrIDs, id)
	}
	if v := params.Get("claim_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("claim_id: %w", err)
		}
		req.ClaimID = &id
	}
	for _, s := range params.GetSlice("ids") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("ids: %w", err)
		}
		req.IDs = append(req.IDs, id)
	}
	for _, s := range params.GetSlice("exclude_ids") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("exclude_ids: %w", err)
		}
		req.ExcludeIDs = append(req.ExcludeIDs, id)
	}
	if v := params.Get("parent_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("parent_id: %w", err)
		}
		req.ParentID = &id
	}
	if v := params.Get("parent_type"); v != "" {
		ot, err := ResolveObjectType(v)
		if err != nil {
			return nil, err
		}
		t := string(ot)
		req.ParentType = &t
	}
	for _, s := range params.GetSlice("owner_id") {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("owner_id: %w", err)
		}
		req.OwnerIDs = append(req.OwnerIDs, id)
	}
	for _, s := range params.GetSlice("with") {
		req.With = append(req.With, s)
	}
	if v := params.Get("format"); v != "" {
		req.Format = &v
	}
	if v := params.Get("limit"); v != "" {
		n, err := utils.ParseInt(v, 0)
		if err != nil {
			return nil, fmt.Errorf("limit: %w", err)
		}
		req.Limit = &n
	}
	if v := params.Get("access"); v != "" {
		n, err := utils.ParseInt(v, 0)
		if err != nil {
			return nil, fmt.Errorf("access: %w", err)
		}
		req.Access = &n
	}
	return &req, nil
}

func searchRun(ctx echo.Context, req *BodySearch) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)

	if err := respond.InitPagination(ctx, req.Limit, nil); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	pagination := respond.GetPagination(ctx)
	offset := (pagination.Page - 1) * pagination.Limit
	cap := pagination.Limit + offset

	query := ""
	if req.Query != nil {
		query = strings.TrimSpace(*req.Query)
	}
	if query == "" {
		return respond.Error(ctx, http.StatusBadRequest, "q is required")
	}

	access := int(ACCESS_NONE)
	if req.Access != nil {
		access = *req.Access
	}

	withSet, err := searchParseWith(req.With)
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	formatDistinct := req.Format != nil && *req.Format == "distinct"

	var claimObjectKeys map[string]struct{}
	if req.ClaimID != nil {
		keys, fetchErr := searchClaimObjectKeys(ctx, *req.ClaimID)
		if fetchErr != nil {
			ctx.Logger().Errorf("fetch claim objects: %v", fetchErr)
			return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
		}
		claimObjectKeys = keys
	}

	rs, total, err := searchClaimius(ctx, uuid.Nil, appID, query, req, access, cap)
	if err != nil {
		ctx.Logger().Errorf("claimius search failed: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	rs = searchApplyScope(rs, req, claimObjectKeys)
	rs = searchApplyFilters(ctx, appID, rs, req)
	rs = searchDedup(rs)

	sort.Slice(rs, func(i, j int) bool { return rs[i].Rank > rs[j].Rank })

	if offset >= len(rs) {
		if formatDistinct {
			return respond.JSON(ctx, http.StatusOK, map[string]map[uuid.UUID]json.RawMessage{}, total)
		}
		return respond.JSON(ctx, http.StatusOK, []ResponseSearch{}, total)
	}
	end := offset + pagination.Limit
	if end > len(rs) {
		end = len(rs)
	}
	page := rs[offset:end]

	if !withSet.any() && !formatDistinct {
		return respond.JSON(ctx, http.StatusOK, page, total)
	}

	hydrated, err := searchHydrate(ctx, page, withSet)
	if err != nil {
		ctx.Logger().Errorf("hydrate search: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if formatDistinct {
		return respond.JSON(ctx, http.StatusOK, hydrated, total)
	}
	return respond.JSON(ctx, http.StatusOK, page, total)
}

func searchClaimius(ctx echo.Context, userID, appID uuid.UUID, query string, req *BodySearch, access, cap int) ([]ResponseSearch, int, error) {
	rs := make([]ResponseSearch, 0)
	err := sdb.Instance().NewRaw(
		`SELECT object_id       AS id,
		        object_type     AS object_type,
		        user_claim_id   AS user_claim_id,
		        name            AS name,
		        description     AS description,
		        link            AS link,
		        sa_access       AS sa_access,
		        scope           AS scope,
		        owner_id        AS owner_id,
		        location_id     AS location_id,
		        is_direct_grant AS is_direct_grant,
		        rank            AS rank,
		        total_count     AS total_count
		   FROM claimius.search(?, ?, ?, ?::text[], ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, appID, query, searchTypeArray(req.ObjectTypes), access,
		searchUUIDArray(req.IDs), searchUUIDArray(req.ExcludeIDs),
		searchUUIDArray(req.OwnerIDs), req.ParentID, req.ParentType,
		cap, 0,
	).Scan(ctx.Request().Context(), &rs)
	if err != nil {
		return nil, 0, err
	}
	total := 0
	if len(rs) > 0 {
		total = rs[0].TotalCount
	}
	return rs, total, nil
}

func searchClaimObjectKeys(ctx echo.Context, claimID uuid.UUID) (map[string]struct{}, error) {
	var rows []struct {
		ObjectID   uuid.UUID `bun:"object_id"`
		ObjectType string    `bun:"object_type"`
	}
	err := sdb.Instance().NewRaw(
		`SELECT object_id, object_type
		   FROM claimius.claim_object
		  WHERE claim_id = ?
		    AND object_id IS NOT NULL
		    AND sa_deleted_at IS NULL`,
		claimID,
	).Scan(ctx.Request().Context(), &rows)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		keys[r.ObjectID.String()+":"+r.ObjectType] = struct{}{}
	}
	return keys, nil
}

func searchApplyScope(results []ResponseSearch, req *BodySearch, claimObjectKeys map[string]struct{}) []ResponseSearch {
	if len(req.OrgIDs) == 0 && len(req.LocIDs) == 0 && claimObjectKeys == nil {
		return results
	}
	out := make([]ResponseSearch, 0, len(results))
	for _, r := range results {
		if len(req.OrgIDs) > 0 {
			if r.OwnerID == nil || !searchContainsUUID(req.OrgIDs, *r.OwnerID) {
				continue
			}
		}
		if len(req.LocIDs) > 0 {
			if r.LocationID == nil || !searchContainsUUID(req.LocIDs, *r.LocationID) {
				continue
			}
		}
		if claimObjectKeys != nil {
			if _, ok := claimObjectKeys[r.ID.String()+":"+string(r.ObjectType)]; !ok {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func searchApplyFilters(ctx echo.Context, appID uuid.UUID, results []ResponseSearch, req *BodySearch) []ResponseSearch {
	excludeTypes := searchIndexStrings(req.ExcludeObjectTypes)
	excludeOwners := searchIndexUUIDs(req.ExcludeOrgIDs)
	excludeLocations := searchIndexUUIDs(req.ExcludeLocIDs)
	excludeUsrs := searchIndexUUIDs(req.ExcludeUsrIDs)
	excludeNeedle := ""
	if req.ExcludeQuery != nil && *req.ExcludeQuery != "" {
		excludeNeedle = strings.ToLower(*req.ExcludeQuery)
	}

	out := make([]ResponseSearch, 0, len(results))
	for _, r := range results {
		if _, skip := excludeTypes[string(r.ObjectType)]; skip {
			continue
		}
		if r.OwnerID != nil {
			if _, skip := excludeOwners[*r.OwnerID]; skip {
				continue
			}
		}
		if r.LocationID != nil {
			if _, skip := excludeLocations[*r.LocationID]; skip {
				continue
			}
		}
		if excludeNeedle != "" {
			match := false
			if r.Name != nil && strings.Contains(strings.ToLower(*r.Name), excludeNeedle) {
				match = true
			}
			if !match && r.Description != nil && strings.Contains(strings.ToLower(*r.Description), excludeNeedle) {
				match = true
			}
			if match {
				continue
			}
		}
		if len(req.UsrIDs) > 0 {
			if !searchAnyUserHasAccess(ctx, appID, req.UsrIDs, r.ID, string(r.ObjectType)) {
				continue
			}
		}
		if len(excludeUsrs) > 0 {
			excluded := false
			for uid := range excludeUsrs {
				if searchAnyUserHasAccess(ctx, appID, []uuid.UUID{uid}, r.ID, string(r.ObjectType)) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func searchAnyUserHasAccess(ctx echo.Context, appID uuid.UUID, userIDs []uuid.UUID, objectID uuid.UUID, objectType string) bool {
	for _, uid := range userIDs {
		exists := false
		err := sdb.Instance().NewRaw(
			`SELECT EXISTS (SELECT 1 FROM claimius.get_access(?, ?, ?, ?, ?))`,
			uid, appID, objectID, objectType, int(ACCESS_READ),
		).Scan(ctx.Request().Context(), &exists)
		if err == nil && exists {
			return true
		}
	}
	return false
}

func searchIndexStrings(in []ObjectType) map[string]struct{} {
	m := make(map[string]struct{}, len(in))
	for _, v := range in {
		m[string(v)] = struct{}{}
	}
	return m
}

func searchIndexUUIDs(in []uuid.UUID) map[uuid.UUID]struct{} {
	m := make(map[uuid.UUID]struct{}, len(in))
	for _, v := range in {
		m[v] = struct{}{}
	}
	return m
}

func searchContainsUUID(s []uuid.UUID, target uuid.UUID) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

func searchDedup(in []ResponseSearch) []ResponseSearch {
	type key struct {
		id  uuid.UUID
		typ ObjectType
	}
	best := make(map[key]int, len(in))
	for i, r := range in {
		k := key{id: r.ID, typ: r.ObjectType}
		if j, ok := best[k]; !ok || in[j].Rank < r.Rank {
			best[k] = i
		}
	}
	out := make([]ResponseSearch, 0, len(best))
	for _, i := range best {
		out = append(out, in[i])
	}
	return out
}

func searchTypeArray(types []ObjectType) interface{} {
	if len(types) == 0 {
		return pgdialect.Array([]string(nil))
	}
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = string(t)
	}
	return pgdialect.Array(out)
}

func searchUUIDArray(s []uuid.UUID) interface{} {
	if len(s) == 0 {
		return pgdialect.Array([]uuid.UUID(nil))
	}
	return pgdialect.Array(s)
}

func searchStringArray(s []string) interface{} {
	if len(s) == 0 {
		return pgdialect.Array([]string(nil))
	}
	return pgdialect.Array(s)
}

func searchHydrate(ctx echo.Context, page []ResponseSearch, with searchWithSet) (map[string]map[uuid.UUID]json.RawMessage, error) {
	dict := make(map[string]map[uuid.UUID]json.RawMessage)
	addKey := func(ot string, id uuid.UUID) {
		strip := StripObjectSchema(ot)
		bucket, ok := dict[strip]
		if !ok {
			bucket = make(map[uuid.UUID]json.RawMessage)
			dict[strip] = bucket
		}
		if _, exists := bucket[id]; !exists {
			bucket[id] = nil
		}
	}
	for _, r := range page {
		addKey(string(r.ObjectType), r.ID)
	}

	trees := make([]string, 0, 2)
	if with.organization {
		trees = append(trees, "ownership")
	}
	if with.location {
		trees = append(trees, "location")
	}
	if len(trees) > 0 {
		srcTypes := make([]string, 0, len(page))
		srcIDs := make([]uuid.UUID, 0, len(page))
		for _, r := range page {
			srcTypes = append(srcTypes, string(r.ObjectType))
			srcIDs = append(srcIDs, r.ID)
		}
		var rows []struct {
			ObjectType string    `bun:"object_type"`
			ObjectID   uuid.UUID `bun:"object_id"`
			TreeType   string    `bun:"tree_type"`
		}
		err := sdb.Instance().NewRaw(
			`SELECT a.object_type, a.object_id, a.tree_type
			   FROM unnest(?::text[], ?::uuid[]) AS src(object_type, object_id)
			   CROSS JOIN LATERAL claimius.get_ancestors(src.object_type, src.object_id) a
			   WHERE a.tree_type = ANY(?::text[])`,
			searchStringArray(srcTypes), searchUUIDArray(srcIDs), searchStringArray(trees),
		).Scan(ctx.Request().Context(), &rows)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			addKey(r.ObjectType, r.ObjectID)
		}
	}

	if with.app {
		seen := make(map[uuid.UUID]struct{})
		for _, r := range page {
			if string(r.ObjectType) == string(OBJECT_TYPE_APP) {
				if _, ok := seen[r.ID]; !ok {
					seen[r.ID] = struct{}{}
					addKey(string(OBJECT_TYPE_APP), r.ID)
				}
			}
		}
	}

	idsByType := make(map[string][]uuid.UUID, len(dict))
	for stripped, bucket := range dict {
		if len(bucket) == 0 {
			continue
		}
		if _, _, ok := searchSchemaTable(stripped); !ok {
			continue
		}
		ids := make([]uuid.UUID, 0, len(bucket))
		for id := range bucket {
			ids = append(ids, id)
		}
		idsByType[stripped] = ids
	}
	rowsByType, err := searchLoadObjectsUnion(ctx, idsByType)
	if err != nil {
		return nil, err
	}
	for stripped, bucket := range dict {
		fetched := rowsByType[stripped]
		for id, raw := range fetched {
			bucket[id] = raw
		}
	}

	for stripped, bucket := range dict {
		for id, raw := range bucket {
			if raw == nil {
				delete(bucket, id)
			}
		}
		if len(bucket) == 0 {
			delete(dict, stripped)
		}
	}
	return dict, nil
}

func searchSchemaTable(stripped string) (string, string, bool) {
	switch stripped {
	case "samna_user":
		return "claimius", "samna_user", true
	case "organization":
		return "claimius", "organization", true
	case "location":
		return "claimius", "location", true
	case "samna_client":
		return "claimius", "samna_client", true
	case "samna_app":
		return "claimius", "samna_app", true
	case "claim":
		return "claimius", "claim", true
	case "samna_secret":
		return "claimius", "samna_secret", true
	case "code":
		return "public", "code", true
	case "asset":
		return "public", "asset", true
	case "provider":
		return "public", "provider", true
	case "module":
		return "public", "module", true
	}
	return "", "", false
}

func searchLoadObjectsUnion(ctx echo.Context, idsByType map[string][]uuid.UUID) (map[string]map[uuid.UUID]json.RawMessage, error) {
	out := make(map[string]map[uuid.UUID]json.RawMessage, len(idsByType))
	if len(idsByType) == 0 {
		return out, nil
	}
	branches := make([]string, 0, len(idsByType))
	args := make([]interface{}, 0, len(idsByType))
	for stripped, ids := range idsByType {
		schema, table, ok := searchSchemaTable(stripped)
		if !ok || len(ids) == 0 {
			continue
		}
		branches = append(branches, fmt.Sprintf(
			`SELECT '%s'::text AS object_type, t.id::uuid AS id, to_jsonb(t) AS data FROM %s.%s t WHERE t.id = ANY(?)`,
			stripped, schema, table,
		))
		args = append(args, searchUUIDArray(ids))
	}
	if len(branches) == 0 {
		return out, nil
	}
	sqlStr := strings.Join(branches, "\nUNION ALL\n")
	var rows []struct {
		ObjectType string          `bun:"object_type"`
		ID         uuid.UUID       `bun:"id"`
		Data       json.RawMessage `bun:"data"`
	}
	if err := sdb.Instance().NewRaw(sqlStr, args...).Scan(ctx.Request().Context(), &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		bucket, ok := out[r.ObjectType]
		if !ok {
			bucket = make(map[uuid.UUID]json.RawMessage)
			out[r.ObjectType] = bucket
		}
		bucket[r.ID] = r.Data
	}
	return out, nil
}
