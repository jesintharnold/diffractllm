package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"diffractllm/internal/governance"
	"diffractllm/internal/modelcatalog"
	"diffractllm/internal/providerplane"
	"diffractllm/internal/providers"
	openaiprovider "diffractllm/internal/providers/openai"
)

// A one-model feed, enough for the catalog to publish a non-empty snapshot.
const testFeed = `{
  "gpt-4o": {
    "mode": "chat", "provider": "openai",
    "max_tokens": 128000,
    "input_cost_per_token": 0.0000025, "output_cost_per_token": 0.00001
  }
}`

func testStore(t *testing.T) *dbstore.Store {
	t.Helper()

	path := filepath.Join(os.TempDir(), "diffractllm_server_test.db")
	s, err := dbstore.NewStore(path, strings.Repeat("s", 32), zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	require.NoError(t, s.Seed(false))
	return s
}

// A catalog with a store but never started, so no snapshot is published.
func emptyCatalog(t *testing.T) *modelcatalog.ModelCatalog {
	t.Helper()
	return modelcatalog.NewModelCatalog(testStore(t), config.ModelCatalogConfig{}, zap.NewNop())
}

// A catalog that has actually synced. Ready() reads unexported snapshots, so
// there is no way to fake it from this package - which makes this the real
// integration path rather than a stub.
func loadedCatalog(t *testing.T) *modelcatalog.ModelCatalog {
	t.Helper()

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(testFeed))
	}))
	t.Cleanup(feed.Close)

	catalog := modelcatalog.NewModelCatalog(testStore(t), config.ModelCatalogConfig{
		SourceURL: feed.URL, SyncInterval: time.Hour,
	}, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, catalog.Start(ctx))
	t.Cleanup(func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = catalog.Shutdown(shutdownCtx)
		cancel()
	})

	require.Eventually(t, catalog.Ready, 5*time.Second, 20*time.Millisecond,
		"the catalog must publish before the readiness test runs")
	return catalog
}

func liveCredential() *core.Credential {
	return &core.Credential{
		ID: "cred-1", Provider: core.ProviderOpenAI, Name: "cred-1",
		APIKey: "sk-test", Enabled: true, Endpoint: "https://api.openai.com",
		AllowedModels: []string{"*"},
	}
}

func registryWithOpenAI() *providers.ProviderInstance {
	registry := providers.NewProviderInstance()
	registry.Register(openaiprovider.New(nil, zap.NewNop()))
	return registry
}

// Every readiness check satisfied. Each test then breaks exactly one.
// Governance whose RunAtStart syncs have actually run, so Stats() reports a
// success. The caches are empty - that is the point: no rows is still synced.
func syncedGovernance(t *testing.T) *governance.Governance {
	t.Helper()

	g, err := governance.NewGovernance(testStore(t), zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, g.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = g.Shutdown(ctx)
	})
	return g
}

func hookEngineWithHooks(t *testing.T) *core.HookEngine {
	t.Helper()
	engine := core.NewHookEngine(zap.NewNop())
	require.NoError(t, engine.AddPostCallHook(noopHook{}))
	return engine
}

type noopHook struct{}

func (noopHook) Name() string                                            { return "noop" }
func (noopHook) Execute(*core.DiffractLLMContext) *core.DiffractLLMError { return nil }

func readyServer(t *testing.T) *DiffractLLMServer {
	t.Helper()
	ds := &DiffractLLMServer{
		logger:           zap.NewNop(),
		ModelCatalog:     loadedCatalog(t),
		CredentialPlane:  providerplane.NewProviderPlane([]*core.Credential{liveCredential()}),
		ProviderRegistry: registryWithOpenAI(),
		governance:       syncedGovernance(t),
		HookEngine:       hookEngineWithHooks(t),
	}
	ds.isReady.Store(true)
	return ds
}
