package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func Write(operation, auditType, objectType string, objectID, ownerID uuid.UUID, createdBy *uuid.UUID, message string) {
	go func() {
		var lastErr error
		for attempt := range 3 {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			err := func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var row db.Audit
				return db.Instance().NewRaw(
					`SELECT * FROM claimius.write_audit(claimius.get_disciple_app_id(), ?, ?, ?, ?, ?, ?, ?)`,
					operation, auditType, objectType, objectID, ownerID, createdBy, message,
				).Scan(ctx, &row)
			}()
			if err == nil {
				return
			}
			lastErr = err
		}
		if log := db.Instance().Logger; log != nil {
			log.Errorf("audit.Write %s %s %s:%s: %v", auditType, operation, objectType, objectID, lastErr)
		}
	}()
}
