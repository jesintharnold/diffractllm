package governance

import (
	"fmt"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/settings"
)

const SettingsScope = "governance"

type Settings struct {
	VKeySyncSeconds    int64 `json:"vkey_sync_interval_seconds"`
	BudgetSyncSeconds  int64 `json:"budget_sync_interval_seconds"`
	BudgetFlushSeconds int64 `json:"budget_flush_interval_seconds"`
	UsageFlushSeconds  int64 `json:"usage_flush_interval_seconds"`
	UsageBufferSize    int64 `json:"usage_buffer_size"`
}

var settingsSpec = settings.Spec[Settings]{Fields: []settings.Field[Settings]{
	settings.Int("vkey_sync_interval_seconds", func(s *Settings) *int64 { return &s.VKeySyncSeconds }, 1, 3600),
	settings.Int("budget_sync_interval_seconds", func(s *Settings) *int64 { return &s.BudgetSyncSeconds }, 1, 3600),
	settings.Int("budget_flush_interval_seconds", func(s *Settings) *int64 { return &s.BudgetFlushSeconds }, 1, 3600),
	settings.Int("usage_flush_interval_seconds", func(s *Settings) *int64 { return &s.UsageFlushSeconds }, 1, 3600),
	settings.Int("usage_buffer_size", func(s *Settings) *int64 { return &s.UsageBufferSize }, 1000, 1_000_000),
}}

func Register(reg *settings.Registry, defaults Settings) (*settings.Handle[Settings], error) {
	h, err := settings.Register(reg, SettingsScope, settingsSpec, defaults)
	if err != nil {
		return nil, fmt.Errorf("governance section of server.yaml: %w", err)
	}
	return h, nil
}

func (s Settings) Validate() error { return settingsSpec.Validate(s) }

func DefaultSettings() Settings {
	s, _ := FromConfig(config.DefaultGovernance())
	return s
}

func FromConfig(c config.GovernanceConfig) (Settings, error) {
	intervals := []struct {
		name string
		d    time.Duration
		to   func(*Settings) *int64
	}{
		{"vkey_sync_interval", c.VKeySyncInterval, func(s *Settings) *int64 { return &s.VKeySyncSeconds }},
		{"budget_sync_interval", c.BudgetSyncInterval, func(s *Settings) *int64 { return &s.BudgetSyncSeconds }},
		{"budget_flush_interval", c.BudgetFlushInterval, func(s *Settings) *int64 { return &s.BudgetFlushSeconds }},
		{"usage_flush_interval", c.UsageFlushInterval, func(s *Settings) *int64 { return &s.UsageFlushSeconds }},
	}
	s := Settings{UsageBufferSize: int64(c.UsageBufferSize)}
	for _, i := range intervals {
		if i.d%time.Second != 0 {
			return Settings{}, fmt.Errorf("governance.%s must be a whole number of seconds, got %s", i.name, i.d)
		}
		*i.to(&s) = int64(i.d / time.Second)
	}
	return s, nil
}
