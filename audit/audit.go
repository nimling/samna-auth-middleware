package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const writers = 64

var slots = make(chan struct{}, writers)

func Write(d *shared.DB, operation, auditType, objectType string, objectID, ownerID uuid.UUID, createdBy *uuid.UUID, message string) {
	slots <- struct{}{}
	go func() {
		defer func() { <-slots }()
		var lastErr error
		for attempt := range 3 {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			err := func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var row shared.Audit
				return d.NewRaw(
					`SELECT * FROM claimius.insert_audit(?, ?, ?, ?, ?, ?, ?)`,
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
