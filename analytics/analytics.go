package analytics

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const (
	jobBuffer        = 4096
	CaptureBytesMax  = 8 << 20
	CaptureMaxAge    = "7 days"
	foldInterval     = time.Hour
	foldLead         = time.Minute
	foldLockKey      = "claimius.analytics_fold"
	rawRetentionDays = 30
	drainTimeout     = 10 * time.Second
)

var (
	ErrNotStarted = errors.New("analytics not started: call sam.Init")
	ErrStarted    = errors.New("analytics already started")
)

type job struct {
	appID      uuid.UUID
	sessionKey string
	events     []shared.AnalyticsEventInput
}

type runner struct {
	db     *shared.DB
	logger echo.Logger
	jobs   chan job
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

var (
	runMu sync.Mutex
	run   *runner
)

func Init(ctx context.Context, db *shared.DB) error {
	if db == nil {
		return shared.ErrNotConnected
	}
	runMu.Lock()
	defer runMu.Unlock()
	if run != nil && !run.ended() {
		return ErrStarted
	}
	r := &runner{
		db:     db,
		logger: db.Logger,
		jobs:   make(chan job, jobBuffer),
		done:   make(chan struct{}),
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.writeLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		shared.LeadSync(ctx, db, foldLockKey, foldLead, r.foldLoop)
	}()
	go func() {
		wg.Wait()
		r.logger.Info("analytics runner stopped")
		close(r.done)
	}()
	run = r
	db.Logger.Info("analytics runner started")
	return nil
}

func Done() <-chan struct{} {
	r := current()
	if r == nil {
		log.Error(ErrNotStarted)
		done := make(chan struct{})
		close(done)
		return done
	}
	return r.done
}

func current() *runner {
	runMu.Lock()
	defer runMu.Unlock()
	return run
}

func (r *runner) ended() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}
