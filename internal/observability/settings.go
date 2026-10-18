// Package observability holds the console-editable observability settings: what the request log
// keeps, how long, and how verbose the gateway's own log is. server.yaml has the defaults
// (metrics_engine and observability sections); what the console saves overrides them from the next start.
package observability

import (
	"errors"
	"fmt"
	"strings"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/observability/logging"
	"diffractllm/internal/settings"
)

// SettingsScope is the setting_overrides row these overrides live in.
const SettingsScope = "observability"

type Settings struct {
	RequestLog     bool   `json:"request_log_enabled"`
	EventsDays     int64  `json:"events_retention_days"`
	PayloadsDays   int64  `json:"payload_retention_days"`
	PayloadMode    string `json:"payload_mode"`
	PayloadMaxKB   int64  `json:"payload_max_kb"`
	FlushSeconds   int64  `json:"flush_interval_seconds"`
	BufferCapacity int64  `json:"buffer_capacity"`
	LogLevel       string `json:"log_level"`
}

var spec = settings.Spec[Settings]{
	Fields: []settings.Field[Settings]{
		settings.Bool("request_log_enabled", func(s *Settings) *bool { return &s.RequestLog }),
		settings.Int("events_retention_days", func(s *Settings) *int64 { return &s.EventsDays }, 1, 3650),
		settings.Int("payload_retention_days", func(s *Settings) *int64 { return &s.PayloadsDays }, 1, 3650),
		settings.Choice("payload_mode", func(s *Settings) *string { return &s.PayloadMode },
			config.PayloadModeNone, config.PayloadModeErrorsOnly, config.PayloadModeFull),
		settings.Int("payload_max_kb", func(s *Settings) *int64 { return &s.PayloadMaxKB }, 1, 10240),
		settings.Int("flush_interval_seconds", func(s *Settings) *int64 { return &s.FlushSeconds }, 1, 3600),
		settings.Int("buffer_capacity", func(s *Settings) *int64 { return &s.BufferCapacity }, 100, 1_000_000),
		settings.Live(
			settings.Choice("log_level", func(s *Settings) *string { return &s.LogLevel }, "debug", "info", "warn", "error"),
			func(s Settings) error {
				logging.SetLevel(s.LogLevel)
				return nil
			},
		),
	},
	// A payload is stored with its event, so it cannot outlive it.
	Cross: func(s Settings) map[string]string {
		if s.PayloadsDays > s.EventsDays {
			return map[string]string{"payload_retention_days": "cannot exceed the events retention"}
		}
		return nil
	},
}

func (s Settings) Validate() error { return spec.Validate(s) }

func Register(reg *settings.Registry, cfg *config.GatewayConfig) (*settings.Handle[Settings], error) {
	defaults, err := FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	h, err := settings.Register(reg, SettingsScope, spec, defaults)
	if err != nil {
		return nil, fmt.Errorf("metrics_engine / observability sections of server.yaml: %w", err)
	}
	running := h.Running()
	running.Apply(cfg)
	logging.SetLevel(running.LogLevel)
	return h, nil
}


func FromConfig(cfg *config.GatewayConfig) (Settings, error) {
	m := cfg.MetricsEngine
	if m == nil {
		return Settings{}, errors.New("the metrics_engine section of server.yaml is missing")
	}
	if m.FlushInterval%time.Second != 0 {
		return Settings{}, fmt.Errorf("metrics_engine.flush_interval must be a whole number of seconds, got %s", m.FlushInterval)
	}
	mode := m.PayloadMode
	if mode == "" {
		mode = config.PayloadModeNone
	}
	level := "info"
	if cfg.Observability != nil && cfg.Observability.LogLevel != "" {
		level = strings.ToLower(cfg.Observability.LogLevel)
	}
	return Settings{
		RequestLog:     m.Enabled,
		EventsDays:     int64(m.EventsDays),
		PayloadsDays:   int64(m.PayloadsDays),
		PayloadMode:    mode,
		PayloadMaxKB:   int64(m.PayloadMaxKB),
		FlushSeconds:   int64(m.FlushInterval / time.Second),
		BufferCapacity: int64(m.BufferCapacity),
		LogLevel:       level,
	}, nil
}

func (s Settings) Apply(cfg *config.GatewayConfig) {
	if cfg.MetricsEngine == nil {
		cfg.MetricsEngine = &config.MetricsEngineConfig{}
	}
	m := cfg.MetricsEngine
	m.Enabled = s.RequestLog
	m.EventsDays = int(s.EventsDays)
	m.PayloadsDays = int(s.PayloadsDays)
	m.PayloadMode = s.PayloadMode
	m.PayloadMaxKB = int(s.PayloadMaxKB)
	m.FlushInterval = time.Duration(s.FlushSeconds) * time.Second
	m.BufferCapacity = int(s.BufferCapacity)
	if cfg.Observability == nil {
		cfg.Observability = &config.Observability{}
	}
	cfg.Observability.LogLevel = s.LogLevel
}
