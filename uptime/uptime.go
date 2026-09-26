package uptime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
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
	stopChan  chan struct{}
	mu        sync.Mutex
	running   bool
}

var run *runner

func Init(logger echo.Logger, opts Options) {
	if run != nil && run.running {
		return
	}
	if shared.GetDebugMode() != shared.DebugModeNone {
		logger.Info("uptime reporter disabled in debug mode")
		return
	}
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	run = &runner{
		logger:   logger,
		opts:     opts,
		stopChan: make(chan struct{}),
		running:  true,
	}
	if err := run.register(context.Background()); err != nil {
		logger.Errorf("uptime register: %v", err)
	}
	go run.loop(context.Background())
	logger.Info("uptime reporter started")
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
	run.logger.Info("uptime reporter stopped")
}

func Running() bool {
	return run != nil && run.running
}

func GetHeartbeat() *Heartbeat {
	if run == nil {
		return nil
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.last == nil {
		return nil
	}
	copied := *run.last
	copied.Checks = append([]BeatCheck(nil), run.last.Checks...)
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
		case <-r.stopChan:
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
