package uptime

import (
	"time"

	"github.com/google/uuid"
)

const (
	MonitorKindHTTP      = "http"
	MonitorKindTCP       = "tcp"
	MonitorKindDisciple  = "disciple"
	MonitorKindHeartbeat = "heartbeat"
	MonitorName          = "heartbeat"
)

type Monitor struct {
	ID              uuid.UUID         `json:"id"`
	AppID           uuid.UUID         `json:"app_id"`
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`
	Target          string            `json:"target,omitempty"`
	IntervalSeconds int               `json:"interval_seconds"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
	ExpectedStatus  int               `json:"expected_status"`
	Enabled         bool              `json:"enabled"`
	OwnerID         uuid.UUID         `json:"owner_id"`
	CreatedBy       uuid.UUID         `json:"created_by"`
	DeletedAt       *time.Time        `json:"deleted_at,omitzero"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	LastCheckedAt   *time.Time        `json:"last_checked_at,omitempty"`
	LastOK          *bool             `json:"last_ok,omitempty"`
	AppSlug         string            `json:"app_slug,omitempty"`
	AppName         string            `json:"app_name,omitempty"`
	Days            []MonitorCheckDay `json:"days,omitempty"`
}

type MonitorInput struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	IntervalSeconds int    `json:"interval_seconds"`
}

type MonitorCheck struct {
	ID         uuid.UUID `json:"id"`
	AppID      uuid.UUID `json:"app_id"`
	MonitorID  uuid.UUID `json:"monitor_id"`
	CheckedAt  time.Time `json:"checked_at"`
	OK         bool      `json:"ok"`
	StatusCode *int      `json:"status_code,omitempty"`
	LatencyMs  int       `json:"latency_ms"`
	Error      *string   `json:"error,omitempty"`
	OwnerID    uuid.UUID `json:"owner_id"`
}

type MonitorCheckDay struct {
	MonitorID   uuid.UUID `json:"monitor_id"`
	AppID       uuid.UUID `json:"app_id"`
	Day         time.Time `json:"day"`
	OKCount     int64     `json:"ok_count"`
	FailedCount int64     `json:"failed_count"`
}

type MonitorIncident struct {
	ID          uuid.UUID  `json:"id"`
	AppID       uuid.UUID  `json:"app_id"`
	MonitorID   uuid.UUID  `json:"monitor_id"`
	OpenedAt    time.Time  `json:"opened_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	Title       string     `json:"title"`
	Description *string    `json:"description,omitempty"`
	OwnerID     uuid.UUID  `json:"owner_id"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	DeletedAt   *time.Time `json:"deleted_at,omitzero"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	AppSlug     string     `json:"app_slug,omitempty"`
	AppName     string     `json:"app_name,omitempty"`
	MonitorName string     `json:"monitor_name,omitempty"`
}

type MonitorStatus struct {
	AppSlug   string            `json:"app_slug"`
	AppName   string            `json:"app_name"`
	Monitors  []Monitor         `json:"monitors"`
	Incidents []MonitorIncident `json:"incidents"`
}

type BeatCheck struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type Beat struct {
	OK        bool        `json:"ok"`
	LatencyMs int         `json:"latency_ms"`
	Version   string      `json:"version"`
	Checks    []BeatCheck `json:"checks"`
}

type Heartbeat struct {
	MonitorID uuid.UUID `json:"monitor_id"`
	BeatAt    time.Time `json:"beat_at"`
	Reported  bool      `json:"reported"`
	Beat
}
