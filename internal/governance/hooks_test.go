package governance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"diffractllm/internal/core"
)

// ---------- fixtures owned by this file ----------
//
// The hooks sit on top of every cache, so this file needs the whole package.
// Run it with the package, not as a file list.

const (
	hModel  = "gpt-4o"
	hClient = "client-1"
	hBudget = "budget-1"
)

func hptr[T any](v T) *T { return &v }

func hKey(id, key string, opts ...func(*core.VirtualKey)) *core.VirtualKey {
	compiled, err := core.CompileProviderConfigs(core.VKDirect, []core.ProviderConfig{
		{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}},
	})
	if err != nil {
		panic(err)
	}
	vk := &core.VirtualKey{
		ID: id, Key: key, ClientID: hClient, BudgetID: hBudget,
		IsActive: true, Mode: core.VKDirect, LoadBalancer: core.LBRoundRobin,
		ProviderConfigs: compiled,
	}
	for _, opt := range opts {
		opt(vk)
	}
	return vk
}

func hConfigs(mode core.VKMode, configs ...core.ProviderConfig) func(*core.VirtualKey) {
	return func(vk *core.VirtualKey) {
		compiled, err := core.CompileProviderConfigs(mode, configs)
		if err != nil {
			panic(err)
		}
		vk.Mode = mode
		vk.ProviderConfigs = compiled
	}
}

func hKeys(t *testing.T, keys ...*core.VirtualKey) *VirtualkeyCache {
	t.Helper()
	kc := &VirtualkeyCache{logger: zap.NewNop()}
	kc.LoadVirtualKeys(keys)
	return kc
}

func hBudgetCfg(id string, limit int64, opts ...func(*core.Budget)) *core.Budget {
	b := &core.Budget{
		ID: id, Name: id, BudgetLimit: limit, BudgetUnit: "nano_usd",
		BudgetDuration: "1D", BudgetParseDuration: 24 * time.Hour,
		LastBudgetRefreshAt: time.Now().UTC(),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func hBudgets(t *testing.T, budgets ...*core.Budget) *BudgetCache {
	t.Helper()
	bc := &BudgetCache{logger: zap.NewNop()}
	for _, cfg := range budgets {
		bc.UpsertBudget(cfg)
	}
	return bc
}

func hRctx(t *testing.T, headers map[string]string) *core.DiffractLLMContext {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rc := core.NewDiffractLLMContextPool().Acquire(context.Background(), req, httptest.NewRecorder())
	rc.RequestKind = core.ChatRequest
	return rc
}

// ModelLookup is an interface, so the hooks need no real catalog.
type hFakeCatalog struct {
	ready  bool
	models map[core.Provider]map[string]struct{}
}

func (f *hFakeCatalog) Ready() bool { return f.ready }

func (f *hFakeCatalog) HasModel(provider core.Provider, modelName string) bool {
	names, ok := f.models[provider]
	if !ok {
		return false
	}
	_, ok = names[modelName]
	return ok
}

func hCatalog(entries map[core.Provider][]string) *hFakeCatalog {
	f := &hFakeCatalog{ready: true, models: make(map[core.Provider]map[string]struct{})}
	for provider, names := range entries {
		f.models[provider] = make(map[string]struct{}, len(names))
		for _, name := range names {
			f.models[provider][name] = struct{}{}
		}
	}
	return f
}

func hChatKey(provider core.Provider, model string) core.CatalogKey {
	return core.CatalogKey{Provider: provider, ModelName: model, ModelType: core.ModelTypeChat}
}

// A key whose plaintext passes ValidateKeySignature, so auth tests exercise the
// lookup rather than stopping at the format check.
func mintedKey(t *testing.T) string {
	t.Helper()
	key, _, _, err := core.GenerateVirtualKey()
	require.NoError(t, err)
	require.True(t, core.ValidateKeySignature(key))
	return key
}

func authGovernance(t *testing.T, keys ...*core.VirtualKey) *Governance {
	t.Helper()
	return &Governance{KeyCache: hKeys(t, keys...), logger: zap.NewNop()}
}

// ---------- ValidatevKeyAuth ----------

func TestValidatevKeyAuthSuccessPopulatesTheContext(t *testing.T) {
	key := mintedKey(t)
	vk := hKey("id-1", key)
	g := authGovernance(t, vk)

	rctx := hRctx(t, map[string]string{"x-diffract-key": key})
	require.Nil(t, g.ValidatevKeyAuth(rctx))

	assert.Equal(t, hClient, rctx.ClientID)
	assert.Equal(t, "id-1", rctx.VirtualKeyID)
	assert.Equal(t, hBudget, rctx.BudgetRef)
	assert.Same(t, vk, rctx.VirtualKeyPolicy, "the shared policy is passed by pointer, not copied")
	assert.True(t, rctx.AuthFrozen)
}

// Four header forms are accepted, in this order.
func TestValidatevKeyAuthHeaderSources(t *testing.T) {
	key := mintedKey(t)

	tests := []struct {
		name    string
		headers map[string]string
		wantOK  bool
	}{
		{name: "x-diffract-key", headers: map[string]string{"x-diffract-key": key}, wantOK: true},
		{name: "authorization bearer", headers: map[string]string{"Authorization": "Bearer " + key}, wantOK: true},
		{name: "x-api-key", headers: map[string]string{"x-api-key": key}, wantOK: true},
		{name: "x-goog-api-key", headers: map[string]string{"x-goog-api-key": key}, wantOK: true},
		{
			name:    "bearer with extra whitespace",
			headers: map[string]string{"Authorization": "Bearer   " + key + "  "},
			wantOK:  true,
		},
		{name: "no headers at all"},
		{
			name:    "authorization without the bearer prefix falls through",
			headers: map[string]string{"Authorization": key},
		},
		{
			name:    "lowercase bearer is not accepted",
			headers: map[string]string{"Authorization": "bearer " + key},
		},
		{name: "blank header", headers: map[string]string{"x-diffract-key": "   "}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := authGovernance(t, hKey("id-1", key))
			derr := g.ValidatevKeyAuth(hRctx(t, tc.headers))

			if tc.wantOK {
				assert.Nil(t, derr)
				return
			}
			require.NotNil(t, derr)
			assert.Equal(t, core.CodeAuthFailed, derr.Code)
			assert.Equal(t, http.StatusUnauthorized, derr.StatusCode)
		})
	}
}

// x-diffract-key wins over the other three.
func TestValidatevKeyAuthHeaderPrecedence(t *testing.T) {
	primary := mintedKey(t)
	secondary := mintedKey(t)
	require.NotEqual(t, primary, secondary)

	g := authGovernance(t, hKey("id-primary", primary), hKey("id-secondary", secondary))

	rctx := hRctx(t, map[string]string{
		"x-diffract-key": primary,
		"Authorization":  "Bearer " + secondary,
		"x-api-key":      secondary,
	})
	require.Nil(t, g.ValidatevKeyAuth(rctx))
	assert.Equal(t, "id-primary", rctx.VirtualKeyID)

	// Drop the winner and the next source takes over.
	rctx2 := hRctx(t, map[string]string{
		"Authorization": "Bearer " + secondary,
		"x-api-key":     primary,
	})
	require.Nil(t, g.ValidatevKeyAuth(rctx2))
	assert.Equal(t, "id-secondary", rctx2.VirtualKeyID)
}

func TestValidatevKeyAuthRejections(t *testing.T) {
	key := mintedKey(t)
	other := mintedKey(t)

	tests := []struct {
		name   string
		keys   []*core.VirtualKey
		header string
	}{
		{name: "malformed key", keys: []*core.VirtualKey{hKey("id-1", key)}, header: "not-a-diffract-key"},
		{name: "well formed but unknown", keys: []*core.VirtualKey{hKey("id-1", key)}, header: other},
		{
			name:   "inactive key",
			keys:   []*core.VirtualKey{hKey("id-1", key, func(vk *core.VirtualKey) { vk.IsActive = false })},
			header: key,
		},
		{
			name: "expired key",
			keys: []*core.VirtualKey{hKey("id-1", key, func(vk *core.VirtualKey) {
				vk.ExpiresAt = hptr(time.Now().Add(-time.Hour))
			})},
			header: key,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := authGovernance(t, tc.keys...)
			rctx := hRctx(t, map[string]string{"x-diffract-key": tc.header})

			derr := g.ValidatevKeyAuth(rctx)
			require.NotNil(t, derr)
			assert.Equal(t, core.CodeAuthFailed, derr.Code)

			assert.Empty(t, rctx.VirtualKeyID, "a rejected request must carry no identity")
			assert.Empty(t, rctx.ClientID)
			assert.Nil(t, rctx.VirtualKeyPolicy)
			assert.False(t, rctx.AuthFrozen)
		})
	}
}

// An expiry in the future is fine; only a past one rejects.
func TestValidatevKeyAuthFutureExpiryIsAccepted(t *testing.T) {
	key := mintedKey(t)
	g := authGovernance(t, hKey("id-1", key, func(vk *core.VirtualKey) {
		vk.ExpiresAt = hptr(time.Now().Add(time.Hour))
	}))

	assert.Nil(t, g.ValidatevKeyAuth(hRctx(t, map[string]string{"x-diffract-key": key})))
}

// ---------- ModelAccessHook ----------

func modelHook(t *testing.T, catalog ModelLookup) *ModelAccessHook {
	t.Helper()
	return NewModelAccessHook(catalog, zap.NewNop())
}

func withPolicy(t *testing.T, rctx *core.DiffractLLMContext, vk *core.VirtualKey) *core.DiffractLLMContext {
	t.Helper()
	rctx.VirtualKeyID = vk.ID
	rctx.VirtualKeyPolicy = vk
	return rctx
}

func TestModelAccessHookName(t *testing.T) {
	assert.Equal(t, "model-access", modelHook(t, nil).Name())
	assert.Equal(t, "budget", NewBudgetCheckHook(nil, zap.NewNop()).Name())
	assert.Equal(t, "record_usage", NewRecordHook(nil, nil, zap.NewNop()).Name())
}

func TestModelAccessHookRejections(t *testing.T) {
	catalog := hCatalog(map[core.Provider][]string{core.ProviderOpenAI: {hModel}})

	tests := []struct {
		name     string
		vk       *core.VirtualKey
		model    string
		wantCode core.ErrorCode
	}{
		{name: "no policy", model: hModel, wantCode: core.CodeAuthFailed},
		{
			name: "no model", vk: hKey("id-1", "dk-a"), model: "   ",
			wantCode: core.CodeMissingParameter,
		},
		{
			name: "provider not on the key",
			vk:   hKey("id-1", "dk-a"), model: "azure/" + hModel,
			wantCode: core.CodeForbidden,
		},
		{
			name: "model not allowed on the named provider",
			vk: hKey("id-1", "dk-a", hConfigs(core.VKDirect, core.ProviderConfig{
				Provider: core.ProviderOpenAI, AllowedModels: []string{"gpt-4o-mini"},
			})),
			model:    "openai/" + hModel,
			wantCode: core.CodeForbidden,
		},
		{
			name: "named model missing from the catalog",
			vk:   hKey("id-1", "dk-a"), model: "openai/ghost-model",
			wantCode: core.CodeInvalidParameter,
		},
		{
			name: "bare model not allowed",
			vk: hKey("id-1", "dk-a", hConfigs(core.VKDirect, core.ProviderConfig{
				Provider: core.ProviderOpenAI, AllowedModels: []string{"gpt-4o-mini"},
			})),
			model:    hModel,
			wantCode: core.CodeForbidden,
		},
		{
			name: "bare model missing from the catalog",
			vk:   hKey("id-1", "dk-a"), model: "ghost-model",
			wantCode: core.CodeInvalidParameter,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rctx := hRctx(t, nil)
			rctx.RequestedModel = tc.model
			if tc.vk != nil {
				withPolicy(t, rctx, tc.vk)
			}

			derr := modelHook(t, catalog).Execute(rctx)
			require.NotNil(t, derr)
			assert.Equal(t, tc.wantCode, derr.Code)
			assert.Empty(t, rctx.Modelkey.ModelName, "a rejected request must not pin a model")
		})
	}
}

// The provider/model form pins both halves onto Modelkey, with the model type
// taken from the request kind.
func TestModelAccessHookNamedProvider(t *testing.T) {
	catalog := hCatalog(map[core.Provider][]string{core.ProviderOpenAI: {hModel}})
	rctx := withPolicy(t, hRctx(t, nil), hKey("id-1", "dk-a"))
	rctx.RequestedModel = "openai/" + hModel

	require.Nil(t, modelHook(t, catalog).Execute(rctx))

	assert.Equal(t, core.ProviderOpenAI, rctx.Modelkey.Provider)
	assert.Equal(t, hModel, rctx.Modelkey.ModelName)
	assert.Equal(t, core.ModelTypeChat, rctx.Modelkey.ModelType)
}

// A bare model leaves the provider empty for the selection engine to fill.
func TestModelAccessHookBareModel(t *testing.T) {
	catalog := hCatalog(map[core.Provider][]string{core.ProviderOpenAI: {hModel}})
	rctx := withPolicy(t, hRctx(t, nil), hKey("id-1", "dk-a"))
	rctx.RequestedModel = hModel

	require.Nil(t, modelHook(t, catalog).Execute(rctx))

	assert.Empty(t, rctx.Modelkey.Provider, "the provider is chosen later, by selection")
	assert.Equal(t, hModel, rctx.Modelkey.ModelName)
	assert.Equal(t, core.ModelTypeChat, rctx.Modelkey.ModelType)
}

// An unknown prefix is not a provider, so it stays part of the model name.
func TestModelAccessHookUnknownPrefixIsPartOfTheName(t *testing.T) {
	catalog := hCatalog(map[core.Provider][]string{core.ProviderOpenAI: {"ft:gpt-4o"}})
	rctx := withPolicy(t, hRctx(t, nil), hKey("id-1", "dk-a"))
	rctx.RequestedModel = "ft:gpt-4o"

	require.Nil(t, modelHook(t, catalog).Execute(rctx))
	assert.Equal(t, "ft:gpt-4o", rctx.Modelkey.ModelName)
	assert.Empty(t, rctx.Modelkey.Provider)
}

// A bare model is checked against every provider the key carries, so one
// provider having it is enough.
func TestModelAccessHookBareModelAcrossProviders(t *testing.T) {
	catalog := hCatalog(map[core.Provider][]string{core.ProviderAzure: {hModel}})
	rctx := withPolicy(t, hRctx(t, nil), hKey("id-1", "dk-a", hConfigs(core.VKWeighted,
		core.ProviderConfig{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}, Weight: 0.5},
		core.ProviderConfig{Provider: core.ProviderAzure, AllowedModels: []string{"*"}, Weight: 0.5},
	)))
	rctx.RequestedModel = hModel

	require.Nil(t, modelHook(t, catalog).Execute(rctx),
		"azure has it, so the request is allowed even though openai does not")
}

// The catalog check is skipped until the first sync publishes. In the real boot
// order the gateway starts the catalog before the listener, so this branch is
// not reachable while serving - these cases pin the behaviour, not a risk.
func TestModelAccessHookSkipsTheCatalogUntilItIsReady(t *testing.T) {
	for _, catalog := range []ModelLookup{nil, &hFakeCatalog{ready: false}} {
		for _, model := range []string{"openai/ghost", "ghost"} {
			rctx := withPolicy(t, hRctx(t, nil), hKey("id-1", "dk-a"))
			rctx.RequestedModel = model

			assert.Nil(t, modelHook(t, catalog).Execute(rctx), model)
			assert.Equal(t, "ghost", rctx.Modelkey.ModelName)
		}
	}
}

// The model type comes from the request kind, so the same name under a
// different endpoint produces a different key.
func TestModelAccessHookModelTypeFollowsTheRequestKind(t *testing.T) {
	catalog := hCatalog(map[core.Provider][]string{core.ProviderOpenAI: {"text-embedding-3-small"}})
	rctx := withPolicy(t, hRctx(t, nil), hKey("id-1", "dk-a"))
	rctx.RequestKind = core.EmbeddingRequest
	rctx.RequestedModel = "openai/text-embedding-3-small"

	require.Nil(t, modelHook(t, catalog).Execute(rctx))
	assert.Equal(t, core.ModelTypeEmbedding, rctx.Modelkey.ModelType)
}

// ---------- BudgetHook ----------

func TestBudgetHook(t *testing.T) {
	t.Run("no budget reference", func(t *testing.T) {
		hook := NewBudgetCheckHook(hBudgets(t), zap.NewNop())

		derr := hook.Execute(hRctx(t, nil))
		require.NotNil(t, derr)
		assert.Equal(t, core.CodeInvalidBudget, derr.Code)
	})

	t.Run("reference not in the cache", func(t *testing.T) {
		hook := NewBudgetCheckHook(hBudgets(t), zap.NewNop())
		rctx := hRctx(t, nil)
		rctx.BudgetRef = "budget-1"

		derr := hook.Execute(rctx)
		require.NotNil(t, derr)
		assert.Equal(t, core.CodeInvalidBudget, derr.Code)

		// The id is an internal detail, so it goes to the log and not to the
		// client's message.
		assert.Equal(t, "Budget configuration error", derr.Message)
		assert.Contains(t, derr.Details["internal_detail"], "budget-1")
	})

	t.Run("within budget", func(t *testing.T) {
		hook := NewBudgetCheckHook(hBudgets(t, hBudgetCfg("budget-1", 10_000)), zap.NewNop())
		rctx := hRctx(t, nil)
		rctx.BudgetRef = "budget-1"

		assert.Nil(t, hook.Execute(rctx))
	})

	t.Run("exhausted", func(t *testing.T) {
		bc := hBudgets(t, hBudgetCfg("budget-1", 100))
		budget, found := bc.LookupBudget("budget-1")
		require.True(t, found)
		budget.RecordUsage(100)

		rctx := hRctx(t, nil)
		rctx.BudgetRef = "budget-1"

		derr := NewBudgetCheckHook(bc, zap.NewNop()).Execute(rctx)
		require.NotNil(t, derr)
		assert.Equal(t, core.CodeBudgetExceeded, derr.Code)

		// NewBudgetExceeded answers 400. server-plan.md §11 expects 429, which
		// is also the conventional status for a quota condition. Pinned as-is;
		// change both together if the plan wins.
		assert.Equal(t, http.StatusBadRequest, derr.StatusCode)
	})

	t.Run("exhausted but not enforced", func(t *testing.T) {
		bc := hBudgets(t, hBudgetCfg("budget-1", 100, func(b *core.Budget) {
			b.Enforce = hptr(false)
		}))
		budget, found := bc.LookupBudget("budget-1")
		require.True(t, found)
		budget.RecordUsage(999_999)

		rctx := hRctx(t, nil)
		rctx.BudgetRef = "budget-1"

		assert.Nil(t, NewBudgetCheckHook(bc, zap.NewNop()).Execute(rctx),
			"an unenforced budget reports spend without blocking")
	})
}

// ---------- RecordHook ----------

func recordRctx(t *testing.T, usage *core.Usage, cost float64) *core.DiffractLLMContext {
	t.Helper()
	rctx := hRctx(t, nil)
	rctx.ClientID = hClient
	rctx.BudgetRef = "budget-1"
	rctx.Modelkey = hChatKey(core.ProviderOpenAI, hModel)
	rctx.UpstreamModel = "gpt-4o-2024-08-06"
	rctx.Usage = usage
	rctx.Cost = cost
	rctx.ResponseBytes = 2048
	rctx.ResponseStatus = 200
	rctx.StartedAt = time.Now().UTC()
	return rctx
}

// No usage means the provider was never reached or never reported, so there is
// nothing to bill and nothing to record.
func TestRecordHookWithoutUsageRecordsNothing(t *testing.T) {
	bc := hBudgets(t, hBudgetCfg("budget-1", 10_000))
	ub := NewUsageBuffer(10, zap.NewNop())

	require.Nil(t, NewRecordHook(bc, ub, zap.NewNop()).Execute(recordRctx(t, nil, 1.5)))

	assert.Zero(t, ub.Len())
	budget, found := bc.LookupBudget("budget-1")
	require.True(t, found)
	assert.Zero(t, budget.WindowCost.Load())
	assert.Zero(t, budget.WindowReqs.Load())
}

func TestRecordHookBuffersAndAccrues(t *testing.T) {
	bc := hBudgets(t, hBudgetCfg("budget-1", 10_000_000_000))
	ub := NewUsageBuffer(10, zap.NewNop())

	rctx := recordRctx(t, &core.Usage{InputTokens: 120, OutputTokens: 42}, 0.0000025)
	require.Nil(t, NewRecordHook(bc, ub, zap.NewNop()).Execute(rctx))

	records := ub.Drain()
	require.Len(t, records, 1)
	r := records[0]

	assert.Equal(t, hClient, r.ClientID)
	assert.Equal(t, "budget-1", r.BudgetID)
	assert.Equal(t, "openai", r.Backend, "the backend is the resolved provider")
	assert.Equal(t, "gpt-4o-2024-08-06", r.ModelID, "the upstream deployment, not the alias")
	assert.Equal(t, hModel, r.ModelName)
	assert.EqualValues(t, 120, r.InputTokens)
	assert.EqualValues(t, 42, r.OutputTokens)
	assert.Equal(t, 2048, r.ResponseBytes)
	assert.Equal(t, 200, r.ResponseStatus)
	assert.Equal(t, rctx.StartedAt, r.RequestedAt)

	// Cost is stored in nano-USD, converted once here.
	want := core.ToNanoUSD(0.0000025)
	assert.Equal(t, want, r.Cost)

	budget, found := bc.LookupBudget("budget-1")
	require.True(t, found)
	assert.Equal(t, want, budget.WindowCost.Load(), "the budget accrues the same nano amount")
	assert.EqualValues(t, 1, budget.WindowReqs.Load())
}

// A zero or negative cost still records the request: the token counts are real
// even when the model is unpriced.
func TestRecordHookZeroCostStillRecords(t *testing.T) {
	bc := hBudgets(t, hBudgetCfg("budget-1", 10_000))
	ub := NewUsageBuffer(10, zap.NewNop())

	require.Nil(t, NewRecordHook(bc, ub, zap.NewNop()).Execute(
		recordRctx(t, &core.Usage{InputTokens: 10, OutputTokens: 5}, 0)))

	records := ub.Drain()
	require.Len(t, records, 1)
	assert.Zero(t, records[0].Cost)
	assert.EqualValues(t, 10, records[0].InputTokens)

	budget, found := bc.LookupBudget("budget-1")
	require.True(t, found)
	assert.Zero(t, budget.WindowCost.Load())
	assert.EqualValues(t, 1, budget.WindowReqs.Load(), "the request still counts")
}

// An unknown budget reference must not lose the usage row. The ledger is the
// audit trail; the budget accrual is best effort.
func TestRecordHookUnknownBudgetStillBuffers(t *testing.T) {
	ub := NewUsageBuffer(10, zap.NewNop())

	rctx := recordRctx(t, &core.Usage{InputTokens: 10, OutputTokens: 5}, 0.001)
	rctx.BudgetRef = "no-such-budget"

	require.Nil(t, NewRecordHook(hBudgets(t), ub, zap.NewNop()).Execute(rctx))

	records := ub.Drain()
	require.Len(t, records, 1)
	assert.Equal(t, "no-such-budget", records[0].BudgetID)
}

// ---------- RegisterHooks ----------

func TestRegisterHooks(t *testing.T) {
	engine := core.NewHookEngine(zap.NewNop())
	g := &Governance{
		KeyCache:    hKeys(t),
		BudgetCache: hBudgets(t, hBudgetCfg("budget-1", 10_000)),
		UsageBuffer: NewUsageBuffer(10, zap.NewNop()),
		logger:      zap.NewNop(),
	}

	RegisterHooks(engine, zap.NewNop(), g, hCatalog(map[core.Provider][]string{
		core.ProviderOpenAI: {hModel},
	}))

	// Admission runs model access then the budget check; settlement runs after
	// the provider, so a request that never reached one is never charged.
	rctx := withPolicy(t, hRctx(t, nil), hKey("id-1", "dk-a"))
	rctx.RequestedModel = hModel
	rctx.BudgetRef = "budget-1"

	require.Nil(t, engine.RunPreCallHooks(rctx))
	assert.Equal(t, hModel, rctx.Modelkey.ModelName, "the model-access hook ran")

	rctx.Usage = &core.Usage{InputTokens: 10, OutputTokens: 5}
	rctx.Cost = 0.002
	engine.RunPostProviderHooks(rctx)

	assert.Equal(t, 1, g.UsageBuffer.Len(), "the record hook is on the post-provider stage")
	budget, found := g.BudgetCache.LookupBudget("budget-1")
	require.True(t, found)
	assert.Equal(t, core.ToNanoUSD(0.002), budget.WindowCost.Load())
}

// The budget check is on the admission stage, so an exhausted budget stops the
// request before a provider is ever selected.
func TestRegisterHooksBudgetBlocksAdmission(t *testing.T) {
	engine := core.NewHookEngine(zap.NewNop())
	bc := hBudgets(t, hBudgetCfg("budget-1", 100))
	budget, found := bc.LookupBudget("budget-1")
	require.True(t, found)
	budget.RecordUsage(500)

	g := &Governance{
		KeyCache: hKeys(t), BudgetCache: bc,
		UsageBuffer: NewUsageBuffer(10, zap.NewNop()), logger: zap.NewNop(),
	}
	RegisterHooks(engine, zap.NewNop(), g, hCatalog(map[core.Provider][]string{
		core.ProviderOpenAI: {hModel},
	}))

	rctx := withPolicy(t, hRctx(t, nil), hKey("id-1", "dk-a"))
	rctx.RequestedModel = hModel
	rctx.BudgetRef = "budget-1"

	derr := engine.RunPreCallHooks(rctx)
	require.NotNil(t, derr)
	assert.Equal(t, core.CodeBudgetExceeded, derr.Code)
}
