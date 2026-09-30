package uptime_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/uptime"
)

type prophetStub struct {
	mu        sync.Mutex
	monitorID uuid.UUID
	existing  []uptime.Monitor
	registers []uptime.MonitorInput
	beats     []uptime.Beat
	beatPaths []string
	headers   []http.Header
	failFirst bool
	srv       *httptest.Server
}

func newProphetStub(t *testing.T, existing []uptime.Monitor, failFirst bool) *prophetStub {
	t.Helper()
	stub := &prophetStub{monitorID: uuid.New(), existing: existing, failFirst: failFirst}
	stub.srv = httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(stub.srv.Close)
	t.Cleanup(uptime.Stop)
	t.Setenv("SAUTH_ENDPOINT", stub.srv.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")
	return stub
}

func (s *prophetStub) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headers = append(s.headers, r.Header.Clone())
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/app/testapp/monitor/client":
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"data": s.existing, "count": len(s.existing)})
	case r.Method == http.MethodPost && r.URL.Path == "/api/app/testapp/monitor/client":
		var input uptime.MonitorInput
		json.NewDecoder(r.Body).Decode(&input)
		s.registers = append(s.registers, input)
		created := uptime.Monitor{ID: s.monitorID, Name: input.Name, Kind: input.Kind, IntervalSeconds: input.IntervalSeconds, Enabled: true}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"data": created, "count": 1})
	case r.Method == http.MethodPost && r.URL.Path == "/api/app/testapp/monitor/"+s.monitorID.String()+"/beat/client":
		var beat uptime.Beat
		json.NewDecoder(r.Body).Decode(&beat)
		s.beats = append(s.beats, beat)
		s.beatPaths = append(s.beatPaths, r.URL.Path)
		if s.failFirst && len(s.beats) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "error": map[string]string{"message": "boom"}})
			return
		}
		check := uptime.MonitorCheck{ID: uuid.New(), MonitorID: s.monitorID, CheckedAt: time.Now(), OK: beat.OK, LatencyMs: beat.LatencyMs}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"data": check, "count": 1})
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "error": map[string]string{"message": "not found"}})
	}
}

func (s *prophetStub) beatCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.beats)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestInitRegistersTheHeartbeatMonitorAndBeats(t *testing.T) {
	stub := newProphetStub(t, nil, false)

	uptime.Init(echo.New().Logger, uptime.Options{
		Interval: 20 * time.Millisecond,
		Version:  "1.2.3",
		Checks: []uptime.Check{
			{Name: "database", Run: func(context.Context) error { return nil }},
			{Name: "cache", Run: func(context.Context) error { return errors.New("dial tcp: refused") }},
		},
	})

	waitFor(t, "first beat", func() bool { return stub.beatCount() >= 1 })

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.registers) != 1 {
		t.Fatalf("registers = %d, want 1", len(stub.registers))
	}
	reg := stub.registers[0]
	if reg.Name != uptime.MonitorName || reg.Kind != uptime.MonitorKindHeartbeat || reg.IntervalSeconds != 1 {
		t.Fatalf("register body = %+v", reg)
	}
	if got := stub.headers[0].Get("SAuth-App-Slug"); got != "testapp" {
		t.Fatalf("SAuth-App-Slug = %q", got)
	}
	if got := stub.headers[0].Get("SAuth-App-Secret"); got != "secret" {
		t.Fatalf("SAuth-App-Secret = %q", got)
	}
	if stub.beatPaths[0] != "/api/app/testapp/monitor/"+stub.monitorID.String()+"/beat/client" {
		t.Fatalf("beat path = %q", stub.beatPaths[0])
	}
	beat := stub.beats[0]
	if beat.OK {
		t.Fatalf("beat ok = true, want false when a check fails")
	}
	if beat.Version != "1.2.3" {
		t.Fatalf("beat version = %q", beat.Version)
	}
	if len(beat.Checks) != 2 || beat.Checks[0].Name != "database" || !beat.Checks[0].OK || beat.Checks[1].Name != "cache" || beat.Checks[1].OK || beat.Checks[1].Error != "dial tcp: refused" {
		t.Fatalf("beat checks = %+v", beat.Checks)
	}
	if beat.LatencyMs < 0 {
		t.Fatalf("beat latency = %d", beat.LatencyMs)
	}
}

func TestInitReusesAnExistingHeartbeatMonitor(t *testing.T) {
	existing := uptime.Monitor{ID: uuid.New(), Name: uptime.MonitorName, Kind: uptime.MonitorKindHeartbeat, Enabled: true}
	stub := newProphetStub(t, []uptime.Monitor{existing}, false)
	stub.monitorID = existing.ID

	uptime.Init(echo.New().Logger, uptime.Options{Interval: 20 * time.Millisecond})

	waitFor(t, "first beat", func() bool { return stub.beatCount() >= 1 })

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.registers) != 0 {
		t.Fatalf("registers = %d, want 0", len(stub.registers))
	}
	if stub.beatPaths[0] != "/api/app/testapp/monitor/"+existing.ID.String()+"/beat/client" {
		t.Fatalf("beat path = %q", stub.beatPaths[0])
	}
	if beat := stub.beats[0]; !beat.OK || len(beat.Checks) != 0 {
		t.Fatalf("beat = %+v, want ok with no checks", beat)
	}
	last := uptime.GetHeartbeat()
	if last == nil || last.MonitorID != existing.ID || !last.Reported {
		t.Fatalf("heartbeat = %+v", last)
	}
}

func TestInitRetriesABeatAfterAServerError(t *testing.T) {
	stub := newProphetStub(t, nil, true)

	uptime.Init(echo.New().Logger, uptime.Options{Interval: 20 * time.Millisecond, Version: "v"})

	waitFor(t, "first beat", func() bool { return stub.beatCount() >= 1 })
	waitFor(t, "unreported heartbeat", func() bool {
		last := uptime.GetHeartbeat()
		return last != nil && !last.Reported
	})
	waitFor(t, "retried beat", func() bool { return stub.beatCount() >= 2 })
	waitFor(t, "reported heartbeat", func() bool {
		last := uptime.GetHeartbeat()
		return last != nil && last.Reported && last.Version == "v"
	})

	if !uptime.Running() {
		t.Fatalf("reporter stopped after a failed beat")
	}
}

func TestInitIsDisabledInDebugMode(t *testing.T) {
	stub := newProphetStub(t, nil, false)
	t.Setenv("SAUTH_DEBUG_USER_ID", uuid.NewString())

	uptime.Init(echo.New().Logger, uptime.Options{Interval: 20 * time.Millisecond})

	time.Sleep(60 * time.Millisecond)
	if uptime.Running() {
		t.Fatalf("reporter running in debug mode")
	}
	if stub.beatCount() != 0 {
		t.Fatalf("beats = %d, want 0", stub.beatCount())
	}
}
