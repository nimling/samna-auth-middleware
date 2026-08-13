package sdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/internal/store"
	"github.com/nimling/samna-auth-middleware/shared"
)

type writeMarshaler interface{ MarshalWrite() ([]byte, error) }
type writeUnmarshaler interface{ UnmarshalWrite([]byte) error }
type patchBodyer interface{ PatchBody() map[string]any }

func marshalModel(model any) ([]byte, error) {
	m, ok := model.(writeMarshaler)
	if !ok {
		return nil, fmt.Errorf("model %T does not implement MarshalWrite", model)
	}
	return m.MarshalWrite()
}

func unmarshalModel(model any, raw []byte) error {
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

type InsertQuery struct{ s *shared.InsertQuery }

func (d *DB) NewInsert() *InsertQuery { return &InsertQuery{s: d.DB.NewInsert()} }

func (q *InsertQuery) Model(m any) *InsertQuery                   { q.s.Model(m); return q }
func (q *InsertQuery) Access(ac *shared.AccessClaim) *InsertQuery { q.s.Access(ac); return q }
func (q *InsertQuery) Returning(v string) *InsertQuery            { q.s.Returning(v); return q }
func (q *InsertQuery) ExcludeColumn(c ...string) *InsertQuery     { q.s.ExcludeColumn(c...); return q }
func (q *InsertQuery) On(e string) *InsertQuery                   { q.s.On(e); return q }
func (q *InsertQuery) Set(e string, a ...any) *InsertQuery        { q.s.Set(e, a...); return q }

func (q *InsertQuery) Exec(ctx context.Context, dest ...any) (sql.Result, error) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return q.s.Exec(ctx, dest...)
	}
	model, ac := q.s.Built()
	path, err := shared.EndpointFor(model, uuid.Nil)
	if err != nil {
		return nil, err
	}
	shared.StampAccessClaim(model, ac)
	if err := shared.StampRoot(ctx, instance.DB, model, ac); err != nil {
		return nil, err
	}
	body, err := marshalModel(model)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := client.Post(ctx, path, json.RawMessage(body), &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := unmarshalModel(model, raw); err != nil {
			return nil, err
		}
		if err := store.Upsert(ctx, instance.DB.DB, model); err != nil {
			return nil, err
		}
	}
	for _, d := range dest {
		if d == nil {
			continue
		}
		if err := shared.CopyInto(model, d); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (q *InsertQuery) Scan(ctx context.Context, dest ...any) error {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return q.s.Scan(ctx, dest...)
	}
	_, err := q.Exec(ctx, dest...)
	return err
}

type UpdateQuery struct{ s *shared.UpdateQuery }

func (d *DB) NewUpdate() *UpdateQuery { return &UpdateQuery{s: d.DB.NewUpdate()} }

func (q *UpdateQuery) Model(m any) *UpdateQuery                   { q.s.Model(m); return q }
func (q *UpdateQuery) Access(ac *shared.AccessClaim) *UpdateQuery { q.s.Access(ac); return q }
func (q *UpdateQuery) ID(id uuid.UUID) *UpdateQuery               { q.s.ID(id); return q }
func (q *UpdateQuery) Returning(v string) *UpdateQuery            { q.s.Returning(v); return q }
func (q *UpdateQuery) ExcludeColumn(c ...string) *UpdateQuery     { q.s.ExcludeColumn(c...); return q }
func (q *UpdateQuery) Column(c ...string) *UpdateQuery            { q.s.Column(c...); return q }
func (q *UpdateQuery) OmitZero() *UpdateQuery                     { q.s.OmitZero(); return q }
func (q *UpdateQuery) Set(e string, a ...any) *UpdateQuery        { q.s.Set(e, a...); return q }
func (q *UpdateQuery) Where(e string, a ...any) *UpdateQuery      { q.s.Where(e, a...); return q }

func (q *UpdateQuery) Exec(ctx context.Context, dest ...any) (sql.Result, error) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return q.s.Exec(ctx, dest...)
	}
	model, _, recordID := q.s.Built()
	path, err := shared.EndpointFor(model, recordID)
	if err != nil {
		return nil, err
	}
	var payload any
	if body := patchBody(model); body != nil {
		payload = body
	} else {
		body, err := marshalModel(model)
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
		if err := unmarshalModel(model, raw); err != nil {
			return nil, err
		}
		if err := store.Upsert(ctx, instance.DB.DB, model); err != nil {
			return nil, err
		}
	}
	for _, d := range dest {
		if d == nil {
			continue
		}
		if err := shared.CopyInto(model, d); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (q *UpdateQuery) Scan(ctx context.Context, dest ...any) error {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return q.s.Scan(ctx, dest...)
	}
	_, err := q.Exec(ctx, dest...)
	return err
}

type DeleteQuery struct{ s *shared.DeleteQuery }

func (d *DB) NewDelete() *DeleteQuery { return &DeleteQuery{s: d.DB.NewDelete()} }

func (q *DeleteQuery) Model(m any) *DeleteQuery                   { q.s.Model(m); return q }
func (q *DeleteQuery) Access(ac *shared.AccessClaim) *DeleteQuery { q.s.Access(ac); return q }
func (q *DeleteQuery) ID(id uuid.UUID) *DeleteQuery               { q.s.ID(id); return q }
func (q *DeleteQuery) Returning(v string) *DeleteQuery            { q.s.Returning(v); return q }
func (q *DeleteQuery) Where(e string, a ...any) *DeleteQuery      { q.s.Where(e, a...); return q }

func (q *DeleteQuery) Exec(ctx context.Context, dest ...any) (sql.Result, error) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return q.s.Exec(ctx, dest...)
	}
	model, _, recordID := q.s.Built()
	path, err := shared.EndpointFor(model, recordID)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := client.Delete(ctx, path, nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := unmarshalModel(model, raw); err != nil {
			return nil, err
		}
		if err := store.Upsert(ctx, instance.DB.DB, model); err != nil {
			return nil, err
		}
	}
	for _, d := range dest {
		if d == nil {
			continue
		}
		if err := shared.CopyInto(model, d); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (q *DeleteQuery) Scan(ctx context.Context, dest ...any) error {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return q.s.Scan(ctx, dest...)
	}
	_, err := q.Exec(ctx, dest...)
	return err
}
