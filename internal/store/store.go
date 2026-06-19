package store

import (
	"context"

	"github.com/uptrace/bun"
)

func Upsert(ctx context.Context, idb bun.IDB, model any) error {
	_, err := idb.NewInsert().
		Model(model).
		On("CONFLICT (id) DO UPDATE").
		Returning("*").
		Exec(ctx)
	return err
}
