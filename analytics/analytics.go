// Package analytics is the claimius capture layer. It runs on the prophet,
// where the consent and event tables live. Init starts the batch writer, Post
// hands a batch to it and returns immediately, so the request path never waits
// on a write. Consent is enforced by the schema functions, so a batch from a
// visitor who did not grant the analytics category writes nothing.
package analytics

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/shared"
)

const jobBuffer = 4096

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

// Init starts the async batch writer. Call it once at startup with the database
// the role's Init returned. A second call is a no op.
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
	db.Logger.Info("analytics runner started")
	return nil
}

// Stop ends the write loop. Batches still queued are dropped.
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

// Running reports whether the capture layer is active. A consumer that never
// called Init serves consent reads but drops every batch.
func Running() bool {
	return run != nil && run.running
}
