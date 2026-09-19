package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"diffractllm/internal/providerplane"
	"diffractllm/internal/providers"
)

// Calls the handler directly: readiness must not depend on the route tree, so
// the check is testable before a listener exists.
func callReady(t *testing.T, ds *DiffractLLMServer) (int, gin.H) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/ready", nil)

	ds.handleReady(c)

	var body gin.H
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return recorder.Code, body
}

// Liveness says only that the process answers, so it is 200 even with nothing
// loaded. That is the distinction from readiness.
func TestHealthIsAlwaysOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/health", nil)

	(&DiffractLLMServer{}).handleHealth(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"status":"ok"}`, recorder.Body.String())
}

// Readiness is the gate that keeps traffic away while the catalog is empty. A
// request served then would pass ModelAccessHook without a catalog check and
// bill zero, so every check has to be able to fail the probe on its own.
func TestReadyRequiresEveryCheck(t *testing.T) {
	tests := []struct {
		name    string
		build   func(*DiffractLLMServer)
		failing string
	}{
		{
			name:    "listener not up",
			build:   func(ds *DiffractLLMServer) { ds.isReady.Store(false) },
			failing: "listening",
		},
		{
			name: "catalog not loaded",
			build: func(ds *DiffractLLMServer) {
				ds.ModelCatalog = emptyCatalog(t)
			},
			failing: "catalog",
		},
		{
			name: "no live credential",
			build: func(ds *DiffractLLMServer) {
				ds.CredentialPlane = providerplane.NewProviderPlane(nil)
			},
			failing: "credentials",
		},
		{
			name: "no adapter in this build",
			build: func(ds *DiffractLLMServer) {
				ds.ProviderRegistry = providers.NewProviderInstance()
			},
			failing: "adapters",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := readyServer(t)
			tc.build(ds)

			code, body := callReady(t, ds)
			assert.Equal(t, http.StatusServiceUnavailable, code)
			assert.Equal(t, "not ready", body["status"])

			checks, ok := body["checks"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, false, checks[tc.failing], "the failing check must be reported")
		})
	}
}

func TestReadyWhenEverythingIsLoaded(t *testing.T) {
	code, body := callReady(t, readyServer(t))

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "ready", body["status"])

	checks, ok := body["checks"].(map[string]any)
	require.True(t, ok)
	for name, value := range checks {
		assert.Equal(t, true, value, name)
	}
}

// The probe reports every check, not just the first failure, so an operator can
// see at a glance what is missing.
func TestReadyReportsAllChecks(t *testing.T) {
	ds := readyServer(t)
	ds.isReady.Store(false)
	ds.CredentialPlane = providerplane.NewProviderPlane(nil)

	code, body := callReady(t, ds)
	require.Equal(t, http.StatusServiceUnavailable, code)

	checks, ok := body["checks"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, checks, 4)
	assert.Equal(t, false, checks["listening"])
	assert.Equal(t, false, checks["credentials"])
	assert.Equal(t, true, checks["catalog"])
	assert.Equal(t, true, checks["adapters"])
}
