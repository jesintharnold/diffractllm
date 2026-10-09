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

func send(t *testing.T, h http.Handler, method, url string, body any) (int, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, url, bytes.NewReader(data)))
	return rec.Code, rec.Body.Bytes()
}

func put(t *testing.T, h http.Handler, url string, body any) (int, map[string]any) {
	t.Helper()
	code, raw := send(t, h, http.MethodPut, url, body)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out), string(raw))
	return code, out
}

func TestListAllCredentials(t *testing.T) {
	h, _, store := catalogServer(t)
	row, err := store.CreateCredential(&core.Credential{
		Provider: core.ProviderOpenAI, Name: "all-creds-test", APIKey: "sk-test-not-real",
		Enabled: true, Endpoint: "https://api.openai.com", AllowedModels: []string{"*"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.DeleteCredential(row.ID) })

	code, raw := send(t, h, http.MethodGet, "/v1/admin/credentials", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var creds []core.Credential
	require.NoError(t, json.Unmarshal(raw, &creds))
	found := false
	for _, c := range creds {
		if c.ID == row.ID {
			found = true
			assert.Equal(t, core.ProviderOpenAI, c.Provider)
			assert.Equal(t, dbstore.SecretMask, c.APIKey)
		}
	}
	assert.True(t, found, "the created credential is missing from the list")
}

// Overrides come back with model_type as a name on create, update and list.
func TestCustomPricingModelTypeIsAName(t *testing.T) {
	h, _, _ := catalogServer(t)

	code, raw := send(t, h, http.MethodPost, "/v1/admin/pricing/custom", map[string]any{
		"name": "test global", "model_name": "gpt-4o", "model_type": "chat", "scope_type": "global",
		"pricing": map[string]any{"input_cost_per_token": 0.000002},
	})
	require.Equal(t, http.StatusCreated, code, string(raw))
	var created map[string]any
	require.NoError(t, json.Unmarshal(raw, &created))
	id := created["id"].(string)
	t.Cleanup(func() { send(t, h, http.MethodDelete, "/v1/admin/pricing/custom/"+id, nil) })
	assert.Equal(t, "chat", created["model_type"])

	code, raw = send(t, h, http.MethodPut, "/v1/admin/pricing/custom/"+id, map[string]any{"input_cost_per_token": 0.0000015})
	require.Equal(t, http.StatusOK, code, string(raw))
	assert.Contains(t, string(raw), `"model_type":"chat"`)

	code, raw = send(t, h, http.MethodGet, "/v1/admin/pricing/custom", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, string(raw), `"model_type":"chat"`)
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
