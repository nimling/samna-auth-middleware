package notification

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const (
	channel      = "claimius_notification"
	heartbeat    = 30 * time.Second
	jobBuffer    = 1024
	clientBuffer = 256
	drainTimeout = 10 * time.Second
)

var (
	ErrNotStarted = errors.New("notification not started: call sam.Init")
	ErrStarted    = errors.New("notification already started")
)

type runner struct {
	db     *shared.DB
	logger echo.Logger
	hub    *hub
	jobs   chan job
	stop   <-chan struct{}
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

var (
	runMu sync.Mutex
	run   *runner

	serveCtx, EndStreams = context.WithCancel(context.Background())
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
	notify, err := db.Listen(ctx, channel)
	if err != nil {
		return err
	}
	r := &runner{
		db:     db,
		logger: db.Logger,
		hub:    newHub(),
		jobs:   make(chan job, jobBuffer),
		stop:   ctx.Done(),
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
		r.listenLoop(ctx, notify)
	}()
	go func() {
		wg.Wait()
		r.hub.closeAll()
		r.logger.Info("notification runner stopped")
		close(r.done)
	}()
	run = r
	db.Logger.Info("notification runner started")
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
