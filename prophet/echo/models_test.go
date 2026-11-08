package echo_test

import (
	"database/sql"
	"reflect"
	"testing"

	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

func TestAssetMapsEveryAssetColumn(t *testing.T) {
	table := bun.NewDB(&sql.DB{}, pgdialect.New()).Table(reflect.TypeOf(pecho.Asset{}))
	columns := []string{"id", "app_id", "name", "description", "mime_type", "sa_owner_id", "sa_created_by", "status", "type", "banner_id", "icon", "index", "blob_url", "sa_deleted_at", "sa_created_at", "sa_updated_at"}
	for _, column := range columns {
		if !table.HasField(column) {
			t.Errorf("Asset has no field for column %s, so an insert returning * fails to scan public.asset", column)
		}
	}
}

func TestSamnaUserMapsEverySamnaUserColumn(t *testing.T) {
	table := bun.NewDB(&sql.DB{}, pgdialect.New()).Table(reflect.TypeOf(pecho.SamnaUser{}))
	columns := []string{"id", "user_id", "app_id", "first_name", "last_name", "user_name", "user_image", "phone", "email", "external_id", "status", "type", "config", "sa_deleted_at", "sa_created_at", "sa_updated_at"}
	for _, column := range columns {
		if !table.HasField(column) {
			t.Errorf("SamnaUser has no field for column %s, so an update or delete returning * fails to scan claimius.samna_user", column)
		}
	}
}
