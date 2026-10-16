package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/nimling/samna-auth-middleware/v2/internal/store"
	"github.com/nimling/samna-auth-middleware/v2/sauth"
	"github.com/uptrace/bun"
)

type idEnvelope struct {
	ID json.RawMessage `json:"id"`
}

func ApplyEvent(ctx context.Context, idb bun.IDB, conn bun.IConn, operation, eventType string, raw json.RawMessage) error {
	if eventType == "claimius.samna_app" {
		return nil
	}
	raw = stripAppID(raw)
	switch operation {
	case "upsert":
		changed, err := applyUpsert(ctx, idb, conn, eventType, raw)
		if err != nil || !changed {
			return err
		}
	case "delete":
		if err := applyDelete(ctx, idb, conn, eventType, raw); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown sync operation: %s", operation)
	}
	exec := conn
	if exec == nil {
		exec = idb
	}
	return RecomputeExternal(ctx, exec, eventType, raw)
}

func stripAppID(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if _, ok := m["app_id"]; !ok {
		return raw
	}
	delete(m, "app_id")
	cleaned, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return cleaned
}

func presentColumns(idb bun.IDB, model any, raw json.RawMessage) []string {
	keys := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil
	}
	columns := make([]string, 0, len(keys))
	for _, f := range idb.Dialect().Tables().Get(reflect.TypeOf(model)).Fields {
		jsonName, _, _ := strings.Cut(f.StructField.Tag.Get("json"), ",")
		sauthName := f.StructField.Tag.Get("sauth")
		if _, ok := keys[f.Name]; ok {
			columns = append(columns, f.Name)
		} else if _, ok := keys[jsonName]; ok && jsonName != "" && jsonName != "-" {
			columns = append(columns, f.Name)
		} else if _, ok := keys[sauthName]; ok && sauthName != "" {
			columns = append(columns, f.Name)
		}
	}
	return columns
}

func applyUpsert(ctx context.Context, idb bun.IDB, conn bun.IConn, eventType string, raw json.RawMessage) (bool, error) {
	switch eventType {
	case "claimius.samna_user":
		var incoming SamnaUser
		if err := sauth.Unmarshal(raw, &incoming); err != nil {
			return false, fmt.Errorf("unmarshal: %w", err)
		}
		retire := idb.NewUpdate().
			Model((*SamnaUser)(nil)).
			Set("sa_deleted_at = now()").
			Set("sa_updated_at = now()").
			Where("user_id = ?", incoming.UserID).
			Where("id <> ?", incoming.ID).
			Where("sa_deleted_at IS NULL")
		if conn != nil {
			retire = retire.Conn(conn)
		}
		if _, err := retire.Exec(ctx); err != nil {
			return false, fmt.Errorf("retire superseded samna_user %s: %w", incoming.UserID, err)
		}
		return store.Upsert(ctx, idb, conn, &incoming, presentColumns(idb, &incoming, raw))
	case "claimius.samna_client":
		return upsertEntity[SamnaClient](ctx, idb, conn, raw)
	case "claimius.samna_secret":
		return upsertEntity[SamnaSecret](ctx, idb, conn, raw)
	case "claimius.organization":
		return upsertEntity[Organization](ctx, idb, conn, raw)
	case "claimius.location":
		return upsertEntity[Location](ctx, idb, conn, raw)
	case "claimius.claim":
		return upsertEntity[Claim](ctx, idb, conn, raw)
	case "claimius.user_claim":
		return upsertEntity[UserClaim](ctx, idb, conn, raw)
	case "claimius.claim_object":
		return upsertEntity[ClaimObject](ctx, idb, conn, raw)
	case "claimius.object_field":
		return upsertEntity[ObjectField](ctx, idb, conn, raw)
	case "claimius.user_field":
		return upsertEntity[UserField](ctx, idb, conn, raw)
	case "claimius.user_relation":
		return upsertEntity[UserRelation](ctx, idb, conn, raw)
	case "claimius.object_asset":
		return upsertEntity[ObjectAsset](ctx, idb, conn, raw)
	case "claimius.user_token":
		return upsertEntity[UserToken](ctx, idb, conn, raw)
	case "claimius.audit":
		var row Audit
		if err := sauth.Unmarshal(raw, &row); err != nil {
			return false, fmt.Errorf("unmarshal: %w", err)
		}
		q := idb.NewInsert().Model(&row).On("CONFLICT DO NOTHING")
		if conn != nil {
			q = q.Conn(conn)
		}
		res, err := q.Exec(ctx)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		return n > 0, err
	default:
		return false, fmt.Errorf("unknown event_type for upsert: %s", eventType)
	}
}

func upsertEntity[T any](ctx context.Context, idb bun.IDB, conn bun.IConn, raw json.RawMessage) (bool, error) {
	var entity T
	if err := sauth.Unmarshal(raw, &entity); err != nil {
		return false, fmt.Errorf("unmarshal: %w", err)
	}
	return store.Upsert(ctx, idb, conn, &entity, presentColumns(idb, &entity, raw))
}

func applyDelete(ctx context.Context, idb bun.IDB, conn bun.IConn, eventType string, raw json.RawMessage) error {
	var env idEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("unmarshal id envelope: %w", err)
	}
	switch eventType {
	case "claimius.samna_user":
		return deleteEntity[SamnaUser](ctx, idb, conn, env.ID)
	case "claimius.samna_client":
		return deleteEntity[SamnaClient](ctx, idb, conn, env.ID)
	case "claimius.samna_secret":
		return deleteEntity[SamnaSecret](ctx, idb, conn, env.ID)
	case "claimius.organization":
		return deleteEntity[Organization](ctx, idb, conn, env.ID)
	case "claimius.location":
		return deleteEntity[Location](ctx, idb, conn, env.ID)
	case "claimius.claim":
		return deleteEntity[Claim](ctx, idb, conn, env.ID)
	case "claimius.user_claim":
		return deleteEntity[UserClaim](ctx, idb, conn, env.ID)
	case "claimius.claim_object":
		return deleteEntity[ClaimObject](ctx, idb, conn, env.ID)
	case "claimius.object_field":
		return deleteEntity[ObjectField](ctx, idb, conn, env.ID)
	case "claimius.user_field":
		return deleteEntity[UserField](ctx, idb, conn, env.ID)
	case "claimius.user_relation":
		return deleteEntity[UserRelation](ctx, idb, conn, env.ID)
	case "claimius.object_asset":
		return deleteEntity[ObjectAsset](ctx, idb, conn, env.ID)
	case "claimius.user_token":
		return deleteEntity[UserToken](ctx, idb, conn, env.ID)
	default:
		return fmt.Errorf("delete not supported for event_type: %s", eventType)
	}
}

func deleteEntity[T any](ctx context.Context, idb bun.IDB, conn bun.IConn, id json.RawMessage) error {
	var key string
	if err := json.Unmarshal(id, &key); err != nil {
		return fmt.Errorf("unmarshal id: %w", err)
	}
	entity := new(T)
	q := idb.NewDelete().Model(entity).Where("id = ?", key)
	if conn != nil {
		q = q.Conn(conn)
	}
	result, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete %T: %w", entity, err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("delete %T rows: %w", entity, err)
	}
	return nil
}
