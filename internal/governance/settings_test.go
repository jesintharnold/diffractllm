package governance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	config "diffractllm/configs"
	"diffractllm/internal/settings"
)

func register(t *testing.T) (*settings.Registry, *settings.Handle[Settings]) {
	t.Helper()
	reg := settings.NewRegistry(testStore(t), zap.NewNop())
	h, err := Register(reg, DefaultSettings())
	require.NoError(t, err)
	return reg, h
}

func TestDefaultSettingsAreWhatTheGatewayDidBefore(t *testing.T) {
	assert.Equal(t, Settings{
		VKeySyncSeconds: 10, BudgetSyncSeconds: 10, BudgetFlushSeconds: 10, UsageFlushSeconds: 10, UsageBufferSize: 100_000,
	}, DefaultSettings())
	require.NoError(t, DefaultSettings().Validate())
}

func TestFromConfigNeedsWholeSeconds(t *testing.T) {
	c := config.DefaultGovernance()
	c.UsageFlushInterval = 1500 * time.Millisecond

	_, err := FromConfig(c)

	require.ErrorContains(t, err, "usage_flush_interval")
}

func TestValidateReportsEveryBadField(t *testing.T) {
	s := DefaultSettings()
	s.VKeySyncSeconds = 0
	s.BudgetSyncSeconds = 7200
	s.UsageBufferSize = 5

	var bad *settings.Error
	require.ErrorAs(t, s.Validate(), &bad)

	assert.Len(t, bad.Fields, 3)
	assert.Equal(t, "must be between 1 and 3600", bad.Fields["vkey_sync_interval_seconds"])
	assert.Equal(t, "must be between 1 and 3600", bad.Fields["budget_sync_interval_seconds"])
	assert.Equal(t, "must be between 1000 and 1000000", bad.Fields["usage_buffer_size"])
}

func TestRegisterRefusesBadYamlDefaults(t *testing.T) {
	_, err := Register(settings.NewRegistry(testStore(t), zap.NewNop()), Settings{})

	require.ErrorContains(t, err, "server.yaml")
}

// What is saved applies from the next start: the running gateway keeps the values it booted with, and
// every governance field is restart-only.
func TestSavedGovernanceSettingsApplyAtTheNextStart(t *testing.T) {
	store := testStore(t)
	reg := settings.NewRegistry(store, zap.NewNop())
	h, err := Register(reg, DefaultSettings())
	require.NoError(t, err)
	g, err := NewGovernance(store, zap.NewNop(), h.Running())
	require.NoError(t, err)
	next := DefaultSettings()
	next.VKeySyncSeconds = 30
	next.UsageBufferSize = 5000

	view, err := h.Save(next)

	require.NoError(t, err)
	assert.Equal(t, next, view.Settings)
	assert.ElementsMatch(t, []string{"vkey_sync_interval_seconds", "usage_buffer_size"}, view.RestartRequired)
	assert.Empty(t, view.Applied)
	raw, err := store.GetSettingOverrides(SettingsScope)
	require.NoError(t, err)
	assert.JSONEq(t, `{"vkey_sync_interval_seconds":30,"usage_buffer_size":5000}`, string(raw))
	assert.Equal(t, 10*time.Second, g.config.vkeySyncInterval, "the running gateway is not touched")

	// A restart: a new registry reads the same table.
	again, err := Register(settings.NewRegistry(store, zap.NewNop()), DefaultSettings())
	require.NoError(t, err)
	restarted, err := NewGovernance(store, zap.NewNop(), again.Running())
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, restarted.config.vkeySyncInterval)
	assert.Equal(t, 10*time.Second, restarted.config.usageFlushInterval, "untouched fields still follow yaml")
	assert.Equal(t, 5000, restarted.UsageBuffer.maxCapacity)
}

func TestResetGovernanceSettingsDropsEveryOverride(t *testing.T) {
	_, h := register(t)
	changed := DefaultSettings()
	changed.BudgetSyncSeconds = 45
	_, err := h.Save(changed)
	require.NoError(t, err)

	view, err := h.Reset()

	require.NoError(t, err)
	assert.Equal(t, DefaultSettings(), view.Settings)
	assert.Empty(t, view.RestartRequired)
}

// A row saved by an older or newer build must never keep the gateway from starting.
func TestRegisterSurvivesAStaleOverride(t *testing.T) {
	store := testStore(t)
	require.NoError(t, store.SaveSettingOverrides(SettingsScope, []byte(`{"usage_buffer_size":3,"retired":1}`)))

	h, err := Register(settings.NewRegistry(store, zap.NewNop()), DefaultSettings())

	require.NoError(t, err)
	assert.Equal(t, DefaultSettings(), h.Running())
}
