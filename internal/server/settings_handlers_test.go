package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/governance"
	"diffractllm/internal/observability"
	"diffractllm/internal/observability/logging"
	"diffractllm/internal/settings"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func obsConfig() *config.GatewayConfig {
	return &config.GatewayConfig{
		Observability: &config.Observability{LogLevel: "info"},
		MetricsEngine: &config.MetricsEngineConfig{
			Enabled: true, EventsDays: 90, PayloadsDays: 7, PayloadMode: "errors_only", PayloadMaxKB: 64,
			FlushInterval: 5 * time.Second, BufferCapacity: 5000,
		},
	}
}

func settingsServer(t *testing.T) (http.Handler, *DiffractLLMServer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := testStore(t)
	wipe := func() { require.NoError(t, store.DB.Exec("DELETE FROM setting_overrides").Error) }
	wipe()
	t.Cleanup(wipe)

	logging.Init("info", logging.FormatConsole)
	reg := settings.NewRegistry(store)
	_, err := observability.Register(reg, obsConfig())
	require.NoError(t, err)
	_, err = governance.Register(reg, governance.DefaultSettings())
	require.NoError(t, err)

	ds := &DiffractLLMServer{logger: zap.NewNop(), config: &config.ServerConfig{}, dbStore: store, governance: syncedGovernance(t)}
	ds.SetSettings(reg)
	h, err := ds.routeHandlers()
	require.NoError(t, err)
	return h, ds
}

func obsDefaults(t *testing.T) observability.Settings {
	t.Helper()
	s, err := observability.FromConfig(obsConfig())
	require.NoError(t, err)
	return s
}

func TestSettingsStartAtTheYamlDefaults(t *testing.T) {
	h, _ := settingsServer(t)

	code, raw := send(t, h, http.MethodGet, "/v1/admin/settings/governance", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var gov settings.View[governance.Settings]
	require.NoError(t, json.Unmarshal(raw, &gov))
	assert.Equal(t, governance.DefaultSettings(), gov.Settings)
	assert.Equal(t, governance.DefaultSettings(), gov.Defaults)
	assert.Equal(t, []string{}, gov.RestartRequired, "an empty list, not null")

	code, raw = send(t, h, http.MethodGet, "/v1/admin/settings/observability", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var obs settings.View[observability.Settings]
	require.NoError(t, json.Unmarshal(raw, &obs))
	assert.Equal(t, obsDefaults(t), obs.Settings)
}

// The whole form goes in one request. A restart-only field is stored and listed as waiting; the
// running workers keep what they started with.
func TestSavedGovernanceFieldsWaitForARestart(t *testing.T) {
	h, ds := settingsServer(t)
	form := governance.DefaultSettings()
	form.VKeySyncSeconds = 30
	form.UsageBufferSize = 5000

	code, raw := send(t, h, http.MethodPut, "/v1/admin/settings/governance", form)

	require.Equal(t, http.StatusOK, code, string(raw))
	var saved settings.View[governance.Settings]
	require.NoError(t, json.Unmarshal(raw, &saved))
	assert.Equal(t, form, saved.Settings)
	assert.ElementsMatch(t, []string{"vkey_sync_interval_seconds", "usage_buffer_size"}, saved.RestartRequired)
	assert.Empty(t, saved.Applied)
	assert.Equal(t, 10*time.Second, jobStats(ds, governance.JobVirtualKeysSync).Interval, "the running job is unchanged")

	_, raw = send(t, h, http.MethodGet, "/v1/admin/settings/governance", nil)
	var again settings.View[governance.Settings]
	require.NoError(t, json.Unmarshal(raw, &again))
	assert.Equal(t, form, again.Settings, "a later read sees what was saved")
	assert.NotEmpty(t, again.RestartRequired, "and still shows what is waiting")
}

// The log level is live: it changes the running logger and is not listed as waiting.
func TestSavedLogLevelAppliesAtOnce(t *testing.T) {
	h, _ := settingsServer(t)
	logging.Init("info", logging.FormatConsole)
	log := logging.For("test")
	form := obsDefaults(t)
	form.LogLevel = "error"
	form.FlushSeconds = 30

	code, raw := send(t, h, http.MethodPut, "/v1/admin/settings/observability", form)

	require.Equal(t, http.StatusOK, code, string(raw))
	var saved settings.View[observability.Settings]
	require.NoError(t, json.Unmarshal(raw, &saved))
	assert.Equal(t, []string{"log_level"}, saved.Applied)
	assert.Equal(t, []string{"flush_interval_seconds"}, saved.RestartRequired)
	assert.False(t, log.Core().Enabled(zapcore.InfoLevel), "the running logger is on error now")

	code, _ = send(t, h, http.MethodDelete, "/v1/admin/settings/observability", nil)
	require.Equal(t, http.StatusOK, code)
	assert.True(t, log.Core().Enabled(zapcore.InfoLevel), "reset puts the yaml level back at once")
}

func TestSaveNamesEveryBadField(t *testing.T) {
	h, _ := settingsServer(t)
	form := obsDefaults(t)
	form.PayloadMode = "everything"
	form.EventsDays, form.PayloadsDays = 30, 60

	code, out := put(t, h, "/v1/admin/settings/observability", form)

	assert.Equal(t, http.StatusBadRequest, code)
	fields, _ := out["fields"].(map[string]any)
	assert.Contains(t, fields, "payload_mode")
	assert.Contains(t, fields, "payload_retention_days", "the cross-field rule is reported too")
}

// A form must carry every field, so leaving one out cannot quietly switch it off.
func TestSaveNeedsTheWholeForm(t *testing.T) {
	h, _ := settingsServer(t)

	code, out := put(t, h, "/v1/admin/settings/observability", map[string]any{"log_level": "debug"})

	assert.Equal(t, http.StatusBadRequest, code)
	fields, _ := out["fields"].(map[string]any)
	assert.Equal(t, "is required", fields["request_log_enabled"])
	assert.NotContains(t, fields, "log_level")
}

func TestSaveRejectsAFormThatIsNotJSON(t *testing.T) {
	h, _ := settingsServer(t)

	code, _ := send(t, h, http.MethodPut, "/v1/admin/settings/governance", "not an object")

	assert.Equal(t, http.StatusBadRequest, code)
}

func TestDeleteResetsToTheYamlDefaults(t *testing.T) {
	h, _ := settingsServer(t)
	form := governance.DefaultSettings()
	form.BudgetFlushSeconds = 60
	code, _ := put(t, h, "/v1/admin/settings/governance", form)
	require.Equal(t, http.StatusOK, code)

	code, raw := send(t, h, http.MethodDelete, "/v1/admin/settings/governance", nil)

	require.Equal(t, http.StatusOK, code, string(raw))
	var view settings.View[governance.Settings]
	require.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, governance.DefaultSettings(), view.Settings)
	_, raw = send(t, h, http.MethodGet, "/v1/admin/settings/governance", nil)
	require.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, governance.DefaultSettings(), view.Settings, "the overrides are gone, not just hidden")
}

func TestAnUnknownSettingsPageIs404(t *testing.T) {
	h, _ := settingsServer(t)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		code, _ := send(t, h, method, "/v1/admin/settings/gateway", map[string]any{})
		assert.Equal(t, http.StatusNotFound, code, method)
	}
}

func TestSettingsAreUnavailableWithoutARegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds := &DiffractLLMServer{logger: zap.NewNop(), config: &config.ServerConfig{}}
	h, err := ds.routeHandlers()
	require.NoError(t, err)

	code, _ := send(t, h, http.MethodGet, "/v1/admin/settings/governance", nil)

	assert.Equal(t, http.StatusServiceUnavailable, code)
}
