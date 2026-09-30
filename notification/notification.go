// Package notification is the claimius live notification layer, shared by the
// prophet and the disciple. Write and WriteUser create rows in
// claimius.notification through the claimius writer functions off the request
// path. An insert wakes a dedicated pq listener over the claimius_notification
// channel, which pushes the row to any connected browser through Stream.
package notification

import (
	"context"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const (
	channel      = "claimius_notification"
	listenerPing = 90 * time.Second
	heartbeat    = 30 * time.Second
	jobBuffer    = 1024
	clientBuffer = 256
)

type runner struct {
	db       *shared.DB
	logger   echo.Logger
	listener *pq.Listener
	hub      *hub
	jobs     chan job
	stopChan chan struct{}
	mu       sync.Mutex
	running  bool
}

var run *runner

// Init opens the dedicated notification listener and starts the async write
// worker and the wake loop. Call it once at startup with the database the
// role's Init returned. A second call is a no op.
func Init(db *shared.DB) error {
	if run != nil {
		return nil
	}
	listener := pq.NewListener(db.GetConnectionString(), 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			db.Logger.Errorf("notification listener error: %v", err)
		}
	})
	if err := listener.Listen(channel); err != nil {
		return err
	}
	run = &runner{
		db:       db,
		logger:   db.Logger,
		listener: listener,
		hub:      newHub(),
		jobs:     make(chan job, jobBuffer),
		stopChan: make(chan struct{}),
		running:  true,
	}
	go run.writeLoop(context.Background())
	go run.listenLoop(context.Background())
	db.Logger.Info("notification runner started")
	return nil
}

// Stop closes the listener, ends the loops and releases every connected client.
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
	if run.listener != nil {
		run.listener.Close()
	}
	run.hub.closeAll()
	run.logger.Info("notification runner stopped")
}
