package governance

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	config "diffractllm/configs"

	"go.uber.org/zap"
)

const SettingsScope = "governance"
type Settings struct {
	VKeySyncSeconds    int64 `json:"vkey_sync_interval_seconds"`
	BudgetSyncSeconds  int64 `json:"budget_sync_interval_seconds"`
	BudgetFlushSeconds int64 `json:"budget_flush_interval_seconds"`
	UsageFlushSeconds  int64 `json:"usage_flush_interval_seconds"`
	UsageBufferSize    int64 `json:"usage_buffer_size"`
}

var settingFields = []struct {
	key      string
	value    func(*Settings) *int64
	min, max int64
}{
	{"vkey_sync_interval_seconds", func(s *Settings) *int64 { return &s.VKeySyncSeconds }, 1, 3600},
	{"budget_sync_interval_seconds", func(s *Settings) *int64 { return &s.BudgetSyncSeconds }, 1, 3600},
	{"budget_flush_interval_seconds", func(s *Settings) *int64 { return &s.BudgetFlushSeconds }, 1, 3600},
	{"usage_flush_interval_seconds", func(s *Settings) *int64 { return &s.UsageFlushSeconds }, 1, 3600},
	{"usage_buffer_size", func(s *Settings) *int64 { return &s.UsageBufferSize }, 1000, 1_000_000},
}

type SettingsError struct{ Fields map[string]string }

func (e *SettingsError) Error() string {
	keys := slices.Sorted(maps.Keys(e.Fields))
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + e.Fields[k]
	}
	return "invalid governance settings: " + strings.Join(parts, "; ")
}

func rangeMessage(min, max int64) string {
	return fmt.Sprintf("must be between %d and %d", min, max)
}

func (s Settings) Validate() error {
	problems := map[string]string{}
	for _, f := range settingFields {
		if v := *f.value(&s); v < f.min || v > f.max {
			problems[f.key] = rangeMessage(f.min, f.max)
		}
	}
	if len(problems) > 0 {
		return &SettingsError{Fields: problems}
	}
	return nil
}

// DefaultSettings are the values server.yaml ships with.
func DefaultSettings() Settings {
	s, _ := FromConfig(config.DefaultGovernance())
	return s
}

// FromConfig reads the yaml section. Intervals must be whole seconds; the bounds are checked by Validate.
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

func Resolve(defaults Settings, saved []byte, logger *zap.Logger) Settings {
	if len(saved) == 0 {
		return defaults
	}
	var overrides map[string]int64
	if err := json.Unmarshal(saved, &overrides); err != nil {
		logger.Warn("governance overrides are unreadable, using server.yaml", zap.Error(err))
		return defaults
	}
	out := defaults
	known := map[string]bool{}
	for _, f := range settingFields {
		known[f.key] = true
		v, ok := overrides[f.key]
		if !ok {
			continue
		}
		if v < f.min || v > f.max {
			logger.Warn("governance override is out of range, using server.yaml",
				zap.String("setting", f.key), zap.Int64("override", v), zap.String("allowed", rangeMessage(f.min, f.max)))
			continue
		}
		logger.Info("governance setting overrides server.yaml",
			zap.String("setting", f.key), zap.Int64("yaml", *f.value(&defaults)), zap.Int64("override", v))
		*f.value(&out) = v
	}
	for key := range overrides {
		if !known[key] {
			logger.Warn("governance override is not a known setting, ignored", zap.String("setting", key))
		}
	}
	return out
}


func diff(defaults, s Settings) map[string]int64 {
	out := map[string]int64{}
	for _, f := range settingFields {
		if v := *f.value(&s); v != *f.value(&defaults) {
			out[f.key] = v
		}
	}
	return out
}

type SettingsView struct {
	Settings Settings `json:"settings"`
	Defaults Settings `json:"defaults"`
}

func (g *Governance) view(raw []byte) SettingsView {
	return SettingsView{Settings: Resolve(g.defaults, raw, g.logger), Defaults: g.defaults}
}

func (g *Governance) SavedSettings() (SettingsView, error) {
	raw, err := g.Store.GetSettingOverrides(SettingsScope)
	if err != nil {
		return SettingsView{}, err
	}
	return g.view(raw), nil
}


func (g *Governance) SaveSettings(next Settings) (SettingsView, error) {
	if err := next.Validate(); err != nil {
		return SettingsView{}, err
	}
	g.settingsMu.Lock()
	defer g.settingsMu.Unlock()

	changed := diff(g.defaults, next)
	if len(changed) == 0 {
		return g.resetLocked()
	}
	raw, err := json.Marshal(changed)
	if err != nil {
		return SettingsView{}, err
	}
	if err := g.Store.SaveSettingOverrides(SettingsScope, raw); err != nil {
		return SettingsView{}, err
	}
	return g.view(raw), nil
}

func (g *Governance) ResetSettings() (SettingsView, error) {
	g.settingsMu.Lock()
	defer g.settingsMu.Unlock()
	return g.resetLocked()
}

func (g *Governance) resetLocked() (SettingsView, error) {
	if err := g.Store.DeleteSettingOverrides(SettingsScope); err != nil {
		return SettingsView{}, err
	}
	return g.view(nil), nil
}
