package observability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	config "diffractllm/configs"
	"diffractllm/internal/observability/logging"
	"diffractllm/internal/settings"
)

func yamlConfig() *config.GatewayConfig {
	return &config.GatewayConfig{
		Observability: &config.Observability{LogLevel: "DEBUG"},
		MetricsEngine: &config.MetricsEngineConfig{
			Enabled: true, EventsDays: 90, PayloadsDays: 7, PayloadMode: "", PayloadMaxKB: 64,
			FlushInterval: 5 * time.Second, BufferCapacity: 5000,
		},
	}
}

func TestFromConfigReadsTheYamlSections(t *testing.T) {
	got, err := FromConfig(yamlConfig())

	require.NoError(t, err)
	assert.Equal(t, Settings{
		RequestLog: true, EventsDays: 90, PayloadsDays: 7, PayloadMode: "none", PayloadMaxKB: 64,
		FlushSeconds: 5, BufferCapacity: 5000, LogLevel: "debug",
	}, got, "an empty payload_mode means none, and the level is lower-cased")
	require.NoError(t, got.Validate())
}

func TestFromConfigRefusesWhatCannotBeShownInWholeSeconds(t *testing.T) {
	cfg := yamlConfig()
	cfg.MetricsEngine.FlushInterval = 1500 * time.Millisecond
	_, err := FromConfig(cfg)
	require.ErrorContains(t, err, "flush_interval")

	cfg.MetricsEngine = nil
	_, err = FromConfig(cfg)
	require.ErrorContains(t, err, "metrics_engine")
}

func TestValidateNamesEachBadField(t *testing.T) {
	s, err := FromConfig(yamlConfig())
	require.NoError(t, err)
	s.PayloadMode = "everything"
	s.BufferCapacity = 5
	s.LogLevel = "loud"

	var bad *settings.Error
	require.ErrorAs(t, s.Validate(), &bad)

	assert.Len(t, bad.Fields, 3)
	assert.Contains(t, bad.Fields["payload_mode"], "none, errors_only, full")
	assert.Equal(t, "must be between 100 and 1000000", bad.Fields["buffer_capacity"])
	assert.Contains(t, bad.Fields["log_level"], "debug, info, warn, error")
}

// A body is stored with its event, so it cannot be kept longer than the event.
func TestPayloadsCannotOutliveTheirEvents(t *testing.T) {
	s, err := FromConfig(yamlConfig())
	require.NoError(t, err)
	s.EventsDays, s.PayloadsDays = 30, 60

	var bad *settings.Error
	require.ErrorAs(t, s.Validate(), &bad)

	assert.Equal(t, map[string]string{"payload_retention_days": "cannot exceed the events retention"}, bad.Fields)
}

func TestApplyWritesTheSettingsOverTheConfig(t *testing.T) {
	cfg := yamlConfig()
	cfg.MetricsEngine.DSN = "./events.duckdb"
	s := Settings{
		RequestLog: false, EventsDays: 30, PayloadsDays: 3, PayloadMode: "full", PayloadMaxKB: 128,
		FlushSeconds: 20, BufferCapacity: 9000, LogLevel: "warn",
	}

	s.Apply(cfg)

	m := cfg.MetricsEngine
	assert.False(t, m.Enabled)
	assert.Equal(t, 30, m.EventsDays)
	assert.Equal(t, 3, m.PayloadsDays)
	assert.Equal(t, "full", m.PayloadMode)
	assert.Equal(t, 128, m.PayloadMaxKB)
	assert.Equal(t, 20*time.Second, m.FlushInterval)
	assert.Equal(t, 9000, m.BufferCapacity)
	assert.Equal(t, "warn", cfg.Observability.LogLevel)
	assert.Equal(t, "./events.duckdb", m.DSN, "what is not a setting stays as loaded")
}

type memory struct{ rows map[string][]byte }

func (m *memory) GetSettingOverrides(scope string) ([]byte, error) { return m.rows[scope], nil }
func (m *memory) SaveSettingOverrides(scope string, data []byte) error {
	m.rows[scope] = data
	return nil
}
func (m *memory) DeleteSettingOverrides(scope string) error { delete(m.rows, scope); return nil }

// Saved settings reach the config before anything is built from it; what is not saved stays yaml.
// Everything but the log level waits for the restart.
func TestSavedOverridesApplyAtStart(t *testing.T) {
	store := &memory{rows: map[string][]byte{}}
	logging.Init("info", logging.FormatConsole)
	h, err := Register(settings.NewRegistry(store), yamlConfig())
	require.NoError(t, err)
	next := h.Defaults()
	next.PayloadMode = "full"
	next.LogLevel = "error"

	view, err := h.Save(next)

	require.NoError(t, err)
	assert.Equal(t, []string{"payload_mode"}, view.RestartRequired, "the payload mode waits for a restart")
	assert.Equal(t, []string{"log_level"}, view.Applied, "the log level is live")

	cfg := yamlConfig()
	_, err = Register(settings.NewRegistry(store), cfg)
	require.NoError(t, err)
	assert.Equal(t, "full", cfg.MetricsEngine.PayloadMode)
	assert.Equal(t, "error", cfg.Observability.LogLevel)
	assert.Equal(t, 90, cfg.MetricsEngine.EventsDays, "untouched fields follow yaml")
}

// Saving the log level changes the logger that is running, with no restart.
func TestLogLevelAppliesToTheRunningLogger(t *testing.T) {
	logging.Init("info", logging.FormatConsole)
	log := logging.For("test")
	h, err := Register(settings.NewRegistry(&memory{rows: map[string][]byte{}}), yamlConfig())
	require.NoError(t, err)
	require.True(t, log.Core().Enabled(zapcore.InfoLevel))
	next := h.Defaults()
	next.LogLevel = "error"

	_, err = h.Save(next)
	require.NoError(t, err)
	assert.False(t, log.Core().Enabled(zapcore.InfoLevel), "info is now below the level")
	assert.True(t, log.Core().Enabled(zapcore.ErrorLevel))

	_, err = h.Reset()
	require.NoError(t, err)
	assert.True(t, log.Core().Enabled(zapcore.DebugLevel), "reset puts the yaml level (debug) back at once")
}

func TestRegisterRefusesBadYamlDefaults(t *testing.T) {
	cfg := yamlConfig()
	cfg.MetricsEngine.PayloadsDays = 400

	_, err := Register(settings.NewRegistry(&memory{rows: map[string][]byte{}}), cfg)

	require.ErrorContains(t, err, "server.yaml")
}
