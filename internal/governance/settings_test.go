package governance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	config "diffractllm/configs"
)

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

	var bad *SettingsError
	require.ErrorAs(t, s.Validate(), &bad)

	assert.Len(t, bad.Fields, 3)
	assert.Equal(t, "must be between 1 and 3600", bad.Fields["vkey_sync_interval_seconds"])
	assert.Equal(t, "must be between 1 and 3600", bad.Fields["budget_sync_interval_seconds"])
	assert.Equal(t, "must be between 1000 and 1000000", bad.Fields["usage_buffer_size"])
}

func TestResolveLayersTheOverridesOnTheYamlDefaults(t *testing.T) {
	defaults := DefaultSettings()
	log := zap.NewNop()

	t.Run("nothing saved is the yaml", func(t *testing.T) {
		assert.Equal(t, defaults, Resolve(defaults, nil, log))
	})

	t.Run("an override wins and the rest stays yaml", func(t *testing.T) {
		got := Resolve(defaults, []byte(`{"vkey_sync_interval_seconds":30}`), log)
		want := defaults
		want.VKeySyncSeconds = 30
		assert.Equal(t, want, got)
	})

	t.Run("an out-of-range override falls back for that field only", func(t *testing.T) {
		got := Resolve(defaults, []byte(`{"vkey_sync_interval_seconds":0,"usage_flush_interval_seconds":5}`), log)
		want := defaults
		want.UsageFlushSeconds = 5
		assert.Equal(t, want, got)
	})

	t.Run("an unknown key or unreadable data never stops a start", func(t *testing.T) {
		assert.Equal(t, defaults, Resolve(defaults, []byte(`{"retired_setting":1}`), log))
		assert.Equal(t, defaults, Resolve(defaults, []byte(`not json`), log))
	})
}

func TestDiffKeepsOnlyWhatChanged(t *testing.T) {
	next := DefaultSettings()
	next.BudgetFlushSeconds = 20

	assert.Equal(t, map[string]int64{"budget_flush_interval_seconds": 20}, diff(DefaultSettings(), next))
	assert.Empty(t, diff(DefaultSettings(), DefaultSettings()))
}

// What is saved applies from the next start: the running gateway keeps the values it booted with.
func TestSaveSettingsStoresOnlyTheDifference(t *testing.T) {
	store := testStore(t)
	g := testGovernance(t, store)
	next := DefaultSettings()
	next.VKeySyncSeconds = 30
	next.UsageBufferSize = 5000

	view, err := g.SaveSettings(next)

	require.NoError(t, err)
	assert.Equal(t, next, view.Settings)
	assert.Equal(t, DefaultSettings(), view.Defaults)
	raw, err := store.GetSettingOverrides(SettingsScope)
	require.NoError(t, err)
	assert.JSONEq(t, `{"vkey_sync_interval_seconds":30,"usage_buffer_size":5000}`, string(raw))

	saved, err := g.SavedSettings()
	require.NoError(t, err)
	assert.Equal(t, next, saved.Settings)

	assert.Equal(t, 10*time.Second, g.config.vkeySyncInterval, "the running gateway is not touched")
	restarted := testGovernance(t, store)
	assert.Equal(t, 30*time.Second, restarted.config.vkeySyncInterval)
	assert.Equal(t, 10*time.Second, restarted.config.usageFlushInterval, "untouched fields still follow yaml")
	assert.Equal(t, 5000, restarted.UsageBuffer.maxCapacity)
}

func TestSaveSettingsEqualToYamlRemovesTheOverrides(t *testing.T) {
	store := testStore(t)
	g := testGovernance(t, store)
	changed := DefaultSettings()
	changed.VKeySyncSeconds = 30
	_, err := g.SaveSettings(changed)
	require.NoError(t, err)

	_, err = g.SaveSettings(DefaultSettings())

	require.NoError(t, err)
	raw, err := store.GetSettingOverrides(SettingsScope)
	require.NoError(t, err)
	assert.Nil(t, raw)
}

func TestSaveSettingsRejectsABadFormAndStoresNothing(t *testing.T) {
	store := testStore(t)
	g := testGovernance(t, store)
	bad := DefaultSettings()
	bad.UsageBufferSize = 5

	_, err := g.SaveSettings(bad)

	var invalid *SettingsError
	require.ErrorAs(t, err, &invalid)
	raw, err := store.GetSettingOverrides(SettingsScope)
	require.NoError(t, err)
	assert.Nil(t, raw)
}

func TestResetSettingsDropsEveryOverride(t *testing.T) {
	store := testStore(t)
	g := testGovernance(t, store)
	changed := DefaultSettings()
	changed.BudgetSyncSeconds = 45
	_, err := g.SaveSettings(changed)
	require.NoError(t, err)

	view, err := g.ResetSettings()

	require.NoError(t, err)
	assert.Equal(t, DefaultSettings(), view.Settings)
	raw, err := store.GetSettingOverrides(SettingsScope)
	require.NoError(t, err)
	assert.Nil(t, raw)
	assert.Equal(t, 10*time.Second, testGovernance(t, store).config.budgetSyncInterval)
}

// A row saved by an older or newer build must never keep the gateway from starting.
func TestNewGovernanceSurvivesAStaleOverride(t *testing.T) {
	store := testStore(t)
	require.NoError(t, store.SaveSettingOverrides(SettingsScope, []byte(`{"usage_buffer_size":3,"retired":1}`)))

	g := testGovernance(t, store)

	assert.Equal(t, DefaultSettings().UsageBufferSize, int64(g.UsageBuffer.maxCapacity))
}

func TestNewGovernanceRefusesBadYamlDefaults(t *testing.T) {
	_, err := NewGovernance(testStore(t), zap.NewNop(), Settings{})

	require.ErrorContains(t, err, "server.yaml")
}
