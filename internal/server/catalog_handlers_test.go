package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"maps"
	"net/http/httptest"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"diffractllm/internal/modelcatalog"
	"diffractllm/internal/providerplane"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// catalogServer is the real route tree over a synced one-model catalog; openai has a credential.
// The store is a process-wide singleton, so saved settings are cleared before and after.
func catalogServer(t *testing.T) (http.Handler, *modelcatalog.ModelCatalog, *dbstore.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	store := testStore(t)
	clearSettings := func() { require.NoError(t, store.DB.Exec("DELETE FROM catalog_settings").Error) }
	clearSettings()
	t.Cleanup(clearSettings)

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(testFeed))
	}))
	t.Cleanup(feed.Close)

	catalog := modelcatalog.NewModelCatalog(store, config.ModelCatalogConfig{SourceURL: feed.URL, SyncInterval: time.Hour}, zap.NewNop())
	require.NoError(t, catalog.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = catalog.Shutdown(ctx)
	})
	// A catalog left by an earlier test skips the boot sync, so sync once by hand.
	require.NoError(t, catalog.SyncNow())
	require.Eventually(t, func() bool { return !catalog.Stats()[0].LastSuccessAt.IsZero() },
		5*time.Second, 20*time.Millisecond)

	ds := &DiffractLLMServer{
		logger: zap.NewNop(), config: &config.ServerConfig{},
		ModelCatalog: catalog, ProviderRegistry: registryWithOpenAI(), dbStore: store,
		CredentialPlane: providerplane.NewProviderPlane([]*core.Credential{liveCredential()}), // openai has a key
	}
	h, err := ds.routeHandlers()
	require.NoError(t, err)
	return h, catalog, store
}

func put(t *testing.T, h http.Handler, url string, body any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, url, bytes.NewReader(data)))
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return rec.Code, out
}

func TestCatalogSettingsRoundTrip(t *testing.T) {
	h, catalog, store := catalogServer(t)

	code, body := get(t, h, "/v1/admin/catalog/settings")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{
		"auto_sync": true, "interval_seconds": 3600.0, "timeout_seconds": 10.0,
		"missing_price": "reject", "ledger_unit": "nano-USD", "display_currency": "USD",
	}, body, "seeded from server.yaml")

	next := map[string]any{
		"auto_sync": false, "interval_seconds": 120, "timeout_seconds": 20,
		"missing_price": "charge_zero", "ledger_unit": "nano-USD", "display_currency": "USD",
	}
	code, body = put(t, h, "/v1/admin/catalog/settings", next)
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, "charge_zero", body["missing_price"])

	// Live: the request path and the sync job see it without a restart.
	assert.Equal(t, modelcatalog.MissingPriceChargeZero, catalog.MissingPrice())
	assert.Equal(t, 2*time.Minute, catalog.Stats()[0].Interval)
	// Durable: the row is in the database.
	row, err := store.GetCatalogSettings()
	require.NoError(t, err)
	assert.False(t, row.AutoSync)
	assert.Equal(t, int64(20), row.TimeoutSeconds)
}

func TestCatalogSettingsRejectsBadInput(t *testing.T) {
	h, catalog, _ := catalogServer(t)
	base := map[string]any{"auto_sync": true, "interval_seconds": 300, "timeout_seconds": 10, "missing_price": "reject"}

	for name, change := range map[string]map[string]any{
		"interval under 30s": {"interval_seconds": 5},
		"zero timeout":       {"timeout_seconds": 0},
		"unknown policy":     {"missing_price": "maybe"},
		"currency is fixed":  {"display_currency": "EUR"},
	} {
		body := maps.Clone(base)
		maps.Copy(body, change)
		code, _ := put(t, h, "/v1/admin/catalog/settings", body)
		assert.Equal(t, http.StatusBadRequest, code, name)
	}
	assert.Equal(t, modelcatalog.MissingPriceReject, catalog.MissingPrice(), "nothing was applied")
}

func TestCatalogSummaryAndModelFilters(t *testing.T) {
	h, _, _ := catalogServer(t)

	code, body := get(t, h, "/v1/admin/catalog/summary")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []any{"openai"}, body["providers"])
	assert.Equal(t, 1.0, body["models"])
	assert.Equal(t, 1.0, body["priced"])
	assert.Equal(t, 0.0, body["unpriced"])
	assert.NotEmpty(t, body["last_sync_at"])

	count := func(query string) float64 {
		code, body := get(t, h, "/v1/admin/models/catalog"+query)
		require.Equal(t, http.StatusOK, code, body)
		return body["total"].(float64)
	}
	assert.Equal(t, 1.0, count(""), "every configured provider when none is given")
	assert.Equal(t, 1.0, count("?q=GPT-4"), "search ignores case")
	assert.Equal(t, 0.0, count("?q=claude"))
	assert.Equal(t, 1.0, count("?pricing=priced"))
	assert.Equal(t, 0.0, count("?pricing=unpriced"))
	assert.Equal(t, 0.0, count("?type=embedding"))

	_, body = get(t, h, "/v1/admin/models/catalog")
	model := body["models"].([]any)[0].(map[string]any)
	assert.Equal(t, "chat", model["model_type"], "a name, not the enum's number")

	code, _ = get(t, h, "/v1/admin/models/catalog?pricing=sometimes")
	assert.Equal(t, http.StatusBadRequest, code)
}
