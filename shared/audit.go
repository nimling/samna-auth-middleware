package shared

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// WriteAudit writes one audit row through claimius.write_audit on idb, the
// transaction of the change it records, so the row commits with the change
// or not at all. appID is the app the row belongs to on a prophet; a disciple
// mirrors one app and passes nil.
func WriteAudit(ctx context.Context, idb bun.IDB, appID *uuid.UUID, operation, auditType, objectType string, objectID, ownerID uuid.UUID, createdBy *uuid.UUID, message string) error {
	if appID == nil {
		_, err := idb.NewRaw(
			`SELECT FROM claimius.write_audit(?, ?, ?, ?, ?, ?, ?)`,
			operation, auditType, objectType, objectID, ownerID, createdBy, message,
		).Exec(ctx)
		return err
	}
	_, err := idb.NewRaw(
		`SELECT FROM claimius.write_audit(?, ?, ?, ?, ?, ?, ?, ?)`,
		*appID, operation, auditType, objectType, objectID, ownerID, createdBy, message,
	).Exec(ctx)
	return err
}
