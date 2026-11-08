package uptime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"

	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const defaultInterval = 60 * time.Second

type Check struct {
	Name string
	Run  func(ctx context.Context) error
}

type Options struct {
	Interval time.Duration
	Version  string
	Checks   []Check
}

type runner struct {
	logger    echo.Logger
	opts      Options
	monitorID uuid.UUID
	last      *Heartbeat
	done      chan struct{}
	mu        sync.Mutex
}

var (
	ErrNotStarted = errors.New("uptime not started: call uptime.Init")
	ErrStarted    = errors.New("uptime already started")
)

var (
	runMu sync.Mutex
	run   *runner
)

func Init(ctx context.Context, logger echo.Logger, opts Options) error {
	runMu.Lock()
	defer runMu.Unlock()
	if run != nil && !run.ended() {
		return ErrStarted
	}
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	r := &runner{logger: logger, opts: opts, done: make(chan struct{})}
	run = r
	if utils.GetDebugMode() != utils.DebugModeNone {
		close(r.done)
		logger.Info("uptime reporter disabled in debug mode")
		return nil
	}
	if err := r.register(ctx); err != nil {
		logger.Errorf("uptime register: %v", err)
	}
	go func() {
		defer close(r.done)
		r.loop(ctx)
		logger.Info("uptime reporter stopped")
	}()
	logger.Info("uptime reporter started")
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

func GetHeartbeat() *Heartbeat {
	r := current()
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		return nil
	}
	copied := *r.last
	copied.Checks = append([]BeatCheck(nil), r.last.Checks...)
	return &copied
}

func monitorTarget(slug string) string {
	return fmt.Sprintf("/api/app/%s/monitor/client", slug)
}

func beatTarget(slug string, id uuid.UUID) string {
	return fmt.Sprintf("/api/app/%s/monitor/%s/beat/client", slug, id)
}

func (r *runner) register(ctx context.Context) error {
	slug := utils.Cfg().AppSlug
	items := make([]Monitor, 0)
	if err := client.Get(ctx, monitorTarget(slug), &items); err != nil {
		return fmt.Errorf("list monitors: %w", err)
	}
	for _, item := range items {
		if item.Name == MonitorName {
			r.setMonitorID(item.ID)
			return nil
		}
	}
	var created Monitor
	seconds := int(r.opts.Interval / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	input := MonitorInput{
		Name:            MonitorName,
		Kind:            MonitorKindHeartbeat,
		IntervalSeconds: seconds,
	}
	if err := client.Post(ctx, monitorTarget(slug), input, &created); err != nil {
		return fmt.Errorf("create monitor: %w", err)
	}
	if created.ID == uuid.Nil {
		return fmt.Errorf("create monitor: prophet returned no id")
	}
	r.setMonitorID(created.ID)
	return nil
}

func (r *runner) setMonitorID(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.monitorID = id
}

func (r *runner) getMonitorID() uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.monitorID
}

func (r *runner) loop(ctx context.Context) {
	ticker := time.NewTicker(r.opts.Interval)
	defer ticker.Stop()
	r.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *runner) tick(ctx context.Context) {
	if r.getMonitorID() == uuid.Nil {
		if err := r.register(ctx); err != nil {
			r.logger.Errorf("uptime register: %v", err)
			return
		}
	}
	beat := r.beat(ctx)
	heartbeat := &Heartbeat{
		MonitorID: r.getMonitorID(),
		BeatAt:    time.Now().UTC(),
		Beat:      beat,
	}
	var check MonitorCheck
	if err := client.Post(ctx, beatTarget(utils.Cfg().AppSlug, heartbeat.MonitorID), beat, &check); err != nil {
		r.logger.Errorf("uptime beat: %v", err)
	} else {
		heartbeat.Reported = true
	}
	r.mu.Lock()
	r.last = heartbeat
	r.mu.Unlock()
}

func (r *runner) beat(ctx context.Context) Beat {
	checkCtx, cancel := context.WithTimeout(ctx, r.opts.Interval)
	defer cancel()
	started := time.Now()
	beat := Beat{OK: true, Version: r.opts.Version, Checks: make([]BeatCheck, 0, len(r.opts.Checks))}
	for _, check := range r.opts.Checks {
		result := BeatCheck{Name: check.Name, OK: true}
		if check.Run != nil {
			if err := check.Run(checkCtx); err != nil {
				result.OK = false
				result.Error = err.Error()
				beat.OK = false
			}
		}
		beat.Checks = append(beat.Checks, result)
	}
	beat.LatencyMs = int(time.Since(started).Milliseconds())
	return beat
}
