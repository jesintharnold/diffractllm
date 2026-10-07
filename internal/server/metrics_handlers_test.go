package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	metricsengine "diffractllm/internal/metrics-engine"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var mt0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// metricsServer is the real route tree over a real DuckDB holding two events; nil engine = disabled.
func metricsServer(t *testing.T, enabled bool) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ds := &DiffractLLMServer{logger: zap.NewNop(), config: &config.ServerConfig{}}

	if enabled {
		store := metricsengine.NewDuckDBStore(zap.NewNop(), context.Background())
		require.NoError(t, store.Init(filepath.Join(t.TempDir(), "m.duckdb"), context.Background()))
		t.Cleanup(func() { _ = store.Close() })
		event := func(id string, at time.Duration, result metricsengine.Result, status int) *metricsengine.Event {
			return &metricsengine.Event{
				SchemaVersion: metricsengine.SchemaVersion, ID: id, Kind: metricsengine.KindLLM,
				RequestID:   "request-" + id,
				RequestKind: core.ChatRequest, RequestEndpoint: "/openai/v1/chat/completions",
				Outcome:      metricsengine.Outcome{Result: result, HTTPStatus: status},
				Timing:       metricsengine.Timing{Start: mt0.Add(at), TotalUS: 1000},
				PayloadState: metricsengine.PayloadNone,
			}
		}
		require.NoError(t, store.Write(context.Background(), []*metricsengine.Event{
			event("ok-1", time.Minute, metricsengine.ResultOK, 200),
			event("rej-1", 2*time.Minute, metricsengine.ResultRejected, 401),
		}))
		ds.SetMetrics(metricsengine.NewMetricsEngine(store, &config.MetricsEngineConfig{
			BufferCapacity: 10, FlushInterval: time.Hour, QueryTimeout: 5 * time.Second,
			MaxPage: 200, MaxWindow: 90 * 24 * time.Hour,
		}, zap.NewNop()))
	}

	h, err := ds.routeHandlers()
	require.NoError(t, err)
	return h
}

func get(t *testing.T, h http.Handler, url string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return rec.Code, body
}

const hour = "from=2026-10-01T10:00:00Z&to=2026-10-01T11:00:00Z"

func TestMetricsEndpoints(t *testing.T) {
	h := metricsServer(t, true)

	code, body := get(t, h, "/v1/admin/metrics/stats?"+hour)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, 2.0, body["requests"])
	assert.Equal(t, 1.0, body["errors"])

	code, body = get(t, h, "/v1/admin/metrics/requests/summary?"+hour)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, 60.0, body["bucket_seconds"])
	assert.Len(t, body["points"], 60)

	code, body = get(t, h, "/v1/admin/metrics/requests?"+hour+"&limit=1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, 2.0, body["total"])
	assert.Len(t, body["rows"], 1)

	code, body = get(t, h, "/v1/admin/metrics/requests/ok-1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "ok-1", body["id"])
	assert.Equal(t, "request-ok-1", body["request_id"])

	code, body = get(t, h, "/v1/admin/metrics/requests/request-ok-1")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "ok-1", body["id"])

	code, _ = get(t, h, "/v1/admin/metrics/requests/missing")
	assert.Equal(t, http.StatusNotFound, code)

	code, _ = get(t, h, "/v1/admin/metrics/requests/ok-1/payload") // no body was kept
	assert.Equal(t, http.StatusNotFound, code)

	code, body = get(t, h, "/v1/admin/metrics/virtual-keys/top?"+hour)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, []any{}, body["rows"]) // seeded events carry no key: [] not null

	code, body = get(t, h, "/v1/admin/metrics/budgets/b1?"+hour)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "b1", body["budget_id"])
	assert.Equal(t, 60.0, body["bucket_seconds"])
	assert.Len(t, body["points"], 60)
}

func TestMetricsEndpointsRejectBadParams(t *testing.T) {
	h := metricsServer(t, true)
	for _, url := range []string{
		"/v1/admin/metrics/stats", // no range
		"/v1/admin/metrics/stats?from=yesterday&to=2026-10-01T11:00:00Z",            // not RFC3339
		"/v1/admin/metrics/stats?from=2026-10-01T10:00:00Z&to=2026-10-01T10:30:00Z", // under 1h
		"/v1/admin/metrics/requests?" + hour + "&limit=abc",                         // not a number
		"/v1/admin/metrics/requests?" + hour + "&limit=500",                         // over max_page
		"/v1/admin/metrics/virtual-keys/top?" + hour + "&limit=51",
	} {
		code, body := get(t, h, url)
		assert.Equal(t, http.StatusBadRequest, code, url)
		assert.NotEmpty(t, body["error"], url)
	}
}

func TestMetricsEndpointsAnswer503WhenDisabled(t *testing.T) {
	code, body := get(t, metricsServer(t, false), "/v1/admin/metrics/stats?"+hour)
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, "metrics engine is disabled", body["error"])
}
