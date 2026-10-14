package store

import (
	"context"
	"reflect"
	"strings"

	"github.com/uptrace/bun"
)

func Upsert(ctx context.Context, idb bun.IDB, conn bun.IConn, model any) (bool, error) {
	q := idb.NewInsert().
		Model(model).
		On("CONFLICT (id) DO UPDATE")
	if conn != nil {
		q = q.Conn(conn)
	}
	if fields := idb.Dialect().Tables().Get(reflect.TypeOf(model)).DataFields; len(fields) > 0 {
		current := make([]string, len(fields))
		incoming := make([]string, len(fields))
		for i, f := range fields {
			current[i] = "?TableAlias." + string(f.SQLName)
			incoming[i] = "EXCLUDED." + string(f.SQLName)
		}
		q = q.Where("ROW(" + strings.Join(current, ", ") + ") IS DISTINCT FROM ROW(" + strings.Join(incoming, ", ") + ")")
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
