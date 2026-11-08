package shared

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func WriteAudit(ctx context.Context, idb bun.IDB, appID *uuid.UUID, operation, auditType, objectType string, objectID, ownerID uuid.UUID, createdBy *uuid.UUID, message string) error {
	if appID == nil {
		_, err := idb.NewRaw(
			`SELECT FROM claimius.insert_audit(?, ?, ?, ?, ?, ?, ?)`,
			operation, auditType, objectType, objectID, ownerID, createdBy, message,
		).Exec(ctx)
		return err
	}
	_, err := idb.NewRaw(
		`SELECT FROM claimius.insert_audit(?, ?, ?, ?, ?, ?, ?, ?)`,
		*appID, operation, auditType, objectType, objectID, ownerID, createdBy, message,
	).Exec(ctx)
	return err
}
