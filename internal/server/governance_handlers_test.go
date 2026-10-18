package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"diffractllm/internal/governance"
	"diffractllm/internal/worker"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	config "diffractllm/configs"
)

func jobStats(ds *DiffractLLMServer, name string) worker.JobStats {
	for _, s := range ds.governance.Stats() {
		if s.Name == name {
			return s
		}
	}
	return worker.JobStats{}
}

// usage_flush does not run at start and its timer is 10 s, so a run inside the test is ours.
func TestRunGovernanceJobRunsAheadOfTheTimer(t *testing.T) {
	h, ds := budgetServer(t)
	require.True(t, jobStats(ds, governance.JobUsageFlush).LastRunAt.IsZero())

	code, out := postJSON(t, h, "/v1/admin/governance/jobs/usage_flush/run", nil)

	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "usage_flush", out["job"])
	assert.Equal(t, "triggered", out["status"])
	require.Eventually(t, func() bool { return !jobStats(ds, governance.JobUsageFlush).LastSuccessAt.IsZero() },
		5*time.Second, 20*time.Millisecond, "the job must actually run")
}

func TestRunGovernanceJobRefusesAnUnknownJob(t *testing.T) {
	h, _ := budgetServer(t)

	code, out := postJSON(t, h, "/v1/admin/governance/jobs/catalog_sync/run", nil)

	assert.Equal(t, http.StatusNotFound, code, "catalog_sync belongs to the catalog, not governance")
	assert.Contains(t, out["error"], "catalog_sync")
}

func TestRunGovernanceJobBeforeStartIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g, err := governance.NewGovernance(testStore(t), zap.NewNop(), governance.DefaultSettings())
	require.NoError(t, err)
	ds := &DiffractLLMServer{logger: zap.NewNop(), config: &config.ServerConfig{}, governance: g}
	h, err := ds.routeHandlers()
	require.NoError(t, err)

	code, out := postJSON(t, h, "/v1/admin/governance/jobs/usage_flush/run", nil)

	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, out["error"], "not running")
}

// settingsServer is the real route tree over a governance with no saved overrides (the store is a
// process-wide singleton, so the row is cleared before and after).
func settingsServer(t *testing.T) (http.Handler, *DiffractLLMServer) {
	t.Helper()
	wipe := func() { require.NoError(t, testStore(t).DB.Exec("DELETE FROM setting_overrides").Error) }
	wipe()
	t.Cleanup(wipe)
	return budgetServer(t)
}

func TestGovernanceSettingsStartAtTheYamlDefaults(t *testing.T) {
	h, _ := settingsServer(t)

	code, raw := send(t, h, http.MethodGet, "/v1/admin/governance/settings", nil)

	require.Equal(t, http.StatusOK, code, string(raw))
	var view governance.SettingsView
	require.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, governance.DefaultSettings(), view.Settings)
	assert.Equal(t, governance.DefaultSettings(), view.Defaults)
}

// The whole form goes in one request; what is saved applies from the next start, so the running
// workers keep their intervals.
func TestPutGovernanceSettingsSavesInBulkWithoutTouchingTheRunningWorkers(t *testing.T) {
	h, ds := settingsServer(t)
	form := governance.DefaultSettings()
	form.VKeySyncSeconds = 30
	form.UsageBufferSize = 5000

	code, raw := send(t, h, http.MethodPut, "/v1/admin/governance/settings", form)

	require.Equal(t, http.StatusOK, code, string(raw))
	var saved governance.SettingsView
	require.NoError(t, json.Unmarshal(raw, &saved))
	assert.Equal(t, form, saved.Settings)

	_, raw = send(t, h, http.MethodGet, "/v1/admin/governance/settings", nil)
	var again governance.SettingsView
	require.NoError(t, json.Unmarshal(raw, &again))
	assert.Equal(t, form, again.Settings, "a later read sees what was saved")
	assert.Equal(t, 10*time.Second, jobStats(ds, governance.JobVirtualKeysSync).Interval, "the running job is unchanged")
}

func TestPutGovernanceSettingsNamesEveryBadField(t *testing.T) {
	h, _ := settingsServer(t)
	form := governance.DefaultSettings()
	form.VKeySyncSeconds = 0
	form.UsageBufferSize = 10

	code, out := put(t, h, "/v1/admin/governance/settings", form)

	assert.Equal(t, http.StatusBadRequest, code)
	fields, _ := out["fields"].(map[string]any)
	assert.Contains(t, fields, "vkey_sync_interval_seconds")
	assert.Contains(t, fields, "usage_buffer_size")
	assert.Len(t, fields, 2, "valid fields are not reported")
}

func TestDeleteGovernanceSettingsResetsToTheYamlDefaults(t *testing.T) {
	h, _ := settingsServer(t)
	form := governance.DefaultSettings()
	form.BudgetFlushSeconds = 60
	code, _ := put(t, h, "/v1/admin/governance/settings", form)
	require.Equal(t, http.StatusOK, code)

	code, raw := send(t, h, http.MethodDelete, "/v1/admin/governance/settings", nil)

	require.Equal(t, http.StatusOK, code, string(raw))
	var view governance.SettingsView
	require.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, governance.DefaultSettings(), view.Settings)
	_, raw = send(t, h, http.MethodGet, "/v1/admin/governance/settings", nil)
	require.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, governance.DefaultSettings(), view.Settings, "the overrides are gone, not just hidden")
}

func TestRunGovernanceJobOnlyAcceptsPost(t *testing.T) {
	h, _ := budgetServer(t)

	code, _ := send(t, h, http.MethodGet, "/v1/admin/governance/jobs/usage_flush/run", nil)

	assert.Equal(t, http.StatusMethodNotAllowed, code)
}
