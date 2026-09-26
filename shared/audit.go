package shared

import (
	"context"
	"time"

	"github.com/google/uuid"
)

func WriteAudit(d *DB, operation, auditType, objectType string, objectID, ownerID uuid.UUID, createdBy *uuid.UUID, message string) {
	go func() {
		var lastErr error
		for attempt := range 3 {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			err := func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var row Audit
				return d.NewRaw(
					`SELECT * FROM claimius.write_audit(?, ?, ?, ?, ?, ?, ?)`,
					operation, auditType, objectType, objectID, ownerID, createdBy, message,
				).Scan(ctx, &row)
			}()
			if err == nil {
				return
			}
			lastErr = err
		}
		if log := d.Logger; log != nil {
			log.Errorf("audit.Write %s %s %s:%s: %v", auditType, operation, objectType, objectID, lastErr)
		}
	}()
}
