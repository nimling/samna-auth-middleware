package audit

import (
	"context"
	"time"

	"github.com/nimling/samna-auth-middleware/db"
)

func Write(a db.Audit) {
	go func() {
		for attempt := range 3 {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * time.Second)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := db.Instance().
				NewInsert().
				Model(&a).
				ExcludeColumn("app_id").
				Exec(ctx)
			cancel()

			if err == nil {
				return
			}
		}
	}()
}
