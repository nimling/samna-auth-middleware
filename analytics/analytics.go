package analytics

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const (
	jobBuffer        = 4096
	CaptureBytesMax  = 8 << 20
	CaptureMaxAge    = "7 days"
	foldInterval     = time.Hour
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
