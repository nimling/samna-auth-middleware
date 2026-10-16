package store

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/uptrace/bun"
)

func Upsert(ctx context.Context, idb bun.IDB, conn bun.IConn, model any, columns []string) (bool, error) {
	q := idb.NewInsert().
		Model(model).
		On("CONFLICT (id) DO UPDATE")
	if conn != nil {
		q = q.Conn(conn)
	}
	fields := idb.Dialect().Tables().Get(reflect.TypeOf(model)).DataFields
	current := make([]string, 0, len(fields))
	incoming := make([]string, 0, len(fields))
	for _, f := range fields {
		if columns != nil && !slices.Contains(columns, f.Name) {
			continue
		}
		q = q.Set("? = EXCLUDED.?", f.SQLName, f.SQLName)
		current = append(current, "?TableAlias."+string(f.SQLName))
		incoming = append(incoming, "EXCLUDED."+string(f.SQLName))
	}
	if len(current) > 0 {
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
