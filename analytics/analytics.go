package analytics

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/shared"
)

const (
	jobBuffer = 4096
	// foldInterval is how often the heat grid is refolded and the raw tier
	// expired. Hourly, because a reader looking at today wants the field to
	// be roughly current and nothing here is worth a tighter loop.
	foldInterval = time.Hour
	// rawRetentionDays is how long a positioned signal survives in
	// analytics_event after it has been folded into the grid. It exists so
	// the bucket size can be changed and the grid rebuilt.
	rawRetentionDays = 30
)

type job struct {
	appID      uuid.UUID
	sessionKey string
	events     []shared.AnalyticsEventInput
}

type runner struct {
	db       *shared.DB
	logger   echo.Logger
	jobs     chan job
	stopChan chan struct{}
	mu       sync.Mutex
	running  bool
}

var run *runner

func Init(db *shared.DB) error {
	if run != nil {
		return nil
	}
	run = &runner{
		db:       db,
		logger:   db.Logger,
		jobs:     make(chan job, jobBuffer),
		stopChan: make(chan struct{}),
		running:  true,
	}
	go run.writeLoop(context.Background())
	go run.foldLoop(context.Background())
	db.Logger.Info("analytics runner started")
	return nil
}

func Stop() {
	if run == nil {
		return
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if !run.running {
		return
	}
	run.running = false
	close(run.stopChan)
	run.logger.Info("analytics runner stopped")
}

func Running() bool {
	return run != nil && run.running
}
