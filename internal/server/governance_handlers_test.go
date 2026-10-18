package server

import (
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
	g, err := governance.NewGovernance(testStore(t), zap.NewNop())
	require.NoError(t, err)
	ds := &DiffractLLMServer{logger: zap.NewNop(), config: &config.ServerConfig{}, governance: g}
	h, err := ds.routeHandlers()
	require.NoError(t, err)

	code, out := postJSON(t, h, "/v1/admin/governance/jobs/usage_flush/run", nil)

	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, out["error"], "not running")
}

func TestRunGovernanceJobOnlyAcceptsPost(t *testing.T) {
	h, _ := budgetServer(t)

	code, _ := send(t, h, http.MethodGet, "/v1/admin/governance/jobs/usage_flush/run", nil)

	assert.Equal(t, http.StatusMethodNotAllowed, code)
}
