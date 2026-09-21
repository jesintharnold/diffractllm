package dbstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"diffractllm/internal/core"
)

const priceModel = "gpt-4o"

func seedBasePrice(t *testing.T, s *Store, provider core.Provider, model string, in float64) {
	t.Helper()
	require.NoError(t, s.BulkSyncModelPricing([]core.PricingVariant{{
		RawKey: string(provider) + "/" + model, Provider: provider,
		ModelName: model, ModelType: core.ModelTypeChat,
		Selectors: core.SelectorSet{},
		Pricing:   core.Pricing{InputCostPerToken: tptr(in), OutputCostPerToken: tptr(in * 4)},
	}}))
}

func overrideRequest(scope core.ScopeType, model string, opts ...func(*core.CustomPricingRequest)) core.CustomPricingRequest {
	r := core.CustomPricingRequest{
		Name: "override-" + string(scope), ModelName: model, ModelType: "chat",
		ScopeType: scope,
		Pricing:   core.Pricing{InputCostPerToken: tptr(9.0)},
	}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func scopedToProvider(p core.Provider) func(*core.CustomPricingRequest) {
	return func(r *core.CustomPricingRequest) { r.ScopeProvider = &p }
}

func scopedToKey(id string) func(*core.CustomPricingRequest) {
	return func(r *core.CustomPricingRequest) { r.ScopeVirtualkeyID = &id }
}

// ---------- the base price gate ----------

// An override is a modifier on a list price, never a price of its own. Without
// that gate a typo in the model name would create a row that never resolves.
func TestCreateCustomPricingRequiresABasePrice(t *testing.T) {
	s := store(t)

	_, err := s.CreateCustomPricing(overrideRequest(core.ScopeGlobal, "gpt-4o-typo"))
	require.Error(t, err)

	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)
	_, err = s.CreateCustomPricing(overrideRequest(core.ScopeGlobal, priceModel))
	assert.NoError(t, err, "a model with a list price is accepted")
}

// A provider-scoped override needs a base price for THAT provider, not just for
// the model name somewhere.
func TestCreateCustomPricingProviderScopeNeedsThatProvidersBase(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

	_, err := s.CreateCustomPricing(overrideRequest(
		core.ScopeProvider, priceModel, scopedToProvider(core.ProviderAzure)))
	require.Error(t, err, "azure has no list price for this model")

	_, err = s.CreateCustomPricing(overrideRequest(
		core.ScopeProvider, priceModel, scopedToProvider(core.ProviderOpenAI)))
	assert.NoError(t, err)
}

// A different model type is a different price, so the gate has to check the type
// too rather than matching on name alone.
func TestCreateCustomPricingChecksTheModelType(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

	embedding := overrideRequest(core.ScopeGlobal, priceModel)
	embedding.ModelType = "embedding"

	_, err := s.CreateCustomPricing(embedding)
	assert.Error(t, err, "only the chat price exists, so an embedding override has no base")
}

// ---------- ScopeRef ----------

// ScopeRef is what makes the unique index discriminate between scopes, so
// BeforeSave has to derive it from the scope rather than leaving it empty.
func TestCustomPricingScopeRef(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

	global, err := s.CreateCustomPricing(overrideRequest(core.ScopeGlobal, priceModel))
	require.NoError(t, err)
	assert.Empty(t, global.ScopeRef, "global has no reference")

	provider, err := s.CreateCustomPricing(overrideRequest(
		core.ScopeProvider, priceModel, scopedToProvider(core.ProviderOpenAI)))
	require.NoError(t, err)
	require.NotNil(t, provider.ScopeProviderID)
	assert.Equal(t, *provider.ScopeProviderID, provider.ScopeRef,
		"a provider scope references the provider row id")

	key, err := s.CreateCustomPricing(overrideRequest(
		core.ScopeVirtualKey, priceModel, scopedToKey("vk-1")))
	require.NoError(t, err)
	assert.Equal(t, "vk-1", key.ScopeRef)
}

// ---------- duplicate scopes ----------

// One row per (model, type, scope, ref). A second override on the same scope is
// a duplicate, not an update, so a double POST cannot silently reprice.
func TestCreateCustomPricingRejectsDuplicateScopes(t *testing.T) {
	tests := []struct {
		name  string
		opts  []func(*core.CustomPricingRequest)
		scope core.ScopeType
	}{
		{name: "global", scope: core.ScopeGlobal},
		{
			name: "provider", scope: core.ScopeProvider,
			opts: []func(*core.CustomPricingRequest){scopedToProvider(core.ProviderOpenAI)},
		},
		{
			name: "virtualkey", scope: core.ScopeVirtualKey,
			opts: []func(*core.CustomPricingRequest){scopedToKey("vk-1")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := store(t)
			seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

			_, err := s.CreateCustomPricing(overrideRequest(tc.scope, priceModel, tc.opts...))
			require.NoError(t, err)

			_, err = s.CreateCustomPricing(overrideRequest(tc.scope, priceModel, tc.opts...))
			assert.Error(t, err, "the unique index must refuse a second row on this scope")
		})
	}
}

// The three scopes differ in scope_type and scope_ref, so they coexist on one
// model - that is what makes the precedence chain possible.
func TestThreeScopesCoexistOnOneModel(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

	for _, req := range []core.CustomPricingRequest{
		overrideRequest(core.ScopeGlobal, priceModel),
		overrideRequest(core.ScopeProvider, priceModel, scopedToProvider(core.ProviderOpenAI)),
		overrideRequest(core.ScopeVirtualKey, priceModel, scopedToKey("vk-1")),
	} {
		_, err := s.CreateCustomPricing(req)
		require.NoError(t, err, req.Name)
	}

	rows, err := s.ListCustomPricing()
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}

// Two virtual keys can each hold their own override on the same model, because
// scope_ref differs.
func TestTwoVirtualKeysCanOverrideTheSameModel(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

	_, err := s.CreateCustomPricing(overrideRequest(core.ScopeVirtualKey, priceModel, scopedToKey("vk-1")))
	require.NoError(t, err)
	_, err = s.CreateCustomPricing(overrideRequest(core.ScopeVirtualKey, priceModel, scopedToKey("vk-2")))
	assert.NoError(t, err)
}

// ---------- filtered listing ----------

// The admin list is scope-aware: no filter lists everything, a filter narrows to
// one scope. That is what drives the two console tabs.
func TestListCustomPricingFiltered(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)
	seedBasePrice(t, s, core.ProviderOpenAI, "gpt-4o-mini", 2)

	for _, req := range []core.CustomPricingRequest{
		overrideRequest(core.ScopeGlobal, priceModel),
		overrideRequest(core.ScopeProvider, priceModel, scopedToProvider(core.ProviderOpenAI)),
		overrideRequest(core.ScopeVirtualKey, priceModel, scopedToKey("vk-1")),
		overrideRequest(core.ScopeGlobal, "gpt-4o-mini"),
	} {
		_, err := s.CreateCustomPricing(req)
		require.NoError(t, err, req.Name+"/"+req.ModelName)
	}

	tests := []struct {
		name   string
		filter CustomPricingFilters
		want   int
	}{
		{name: "no filter lists everything", want: 4},
		{name: "global scope", filter: CustomPricingFilters{ScopeType: string(core.ScopeGlobal)}, want: 2},
		{name: "provider scope", filter: CustomPricingFilters{ScopeType: string(core.ScopeProvider)}, want: 1},
		{
			name:   "one virtual key",
			filter: CustomPricingFilters{VirtualKeyID: "vk-1"}, want: 1,
		},
		{
			name:   "an unknown virtual key has nothing",
			filter: CustomPricingFilters{VirtualKeyID: "vk-nope"},
		},
		{name: "one model", filter: CustomPricingFilters{ModelName: priceModel}, want: 3},
		{
			name:   "one provider",
			filter: CustomPricingFilters{Provider: string(core.ProviderOpenAI)}, want: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.ListCustomPricingFiltered(tc.filter)
			require.NoError(t, err)
			assert.Len(t, rows, tc.want)
		})
	}
}

// ---------- ToCore and delete ----------

// The catalog groups overrides by CustomPricingKey, which drops the provider, so
// the scope has to survive ToCore for the precedence chain to work.
func TestCustomPricingToCore(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

	row, err := s.CreateCustomPricing(overrideRequest(
		core.ScopeProvider, priceModel, scopedToProvider(core.ProviderOpenAI)))
	require.NoError(t, err)

	rows, err := s.ListCustomPricing()
	require.NoError(t, err)
	require.Len(t, rows, 1)

	got := rows[0].ToCore()
	assert.Equal(t, row.ID, got.ID)
	assert.Equal(t, priceModel, got.ModelName)
	assert.Equal(t, core.ModelTypeChat, got.ModelType)
	assert.Equal(t, core.ScopeProvider, got.ScopeType)
	require.NotNil(t, got.ScopeProvider)
	assert.Equal(t, core.ProviderOpenAI, *got.ScopeProvider, "the provider name, not its row id")
	require.NotNil(t, got.Pricing.InputCostPerToken)
	assert.Equal(t, 9.0, *got.Pricing.InputCostPerToken)

	// The key the catalog files it under has no provider.
	assert.Equal(t, core.CatalogKey{ModelName: priceModel, ModelType: core.ModelTypeChat},
		got.CustomPricingKey())
}

func TestDeleteCustomPricing(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

	row, err := s.CreateCustomPricing(overrideRequest(core.ScopeGlobal, priceModel))
	require.NoError(t, err)

	require.NoError(t, s.DeleteCustomPricing(row.ID))

	rows, err := s.ListCustomPricing()
	require.NoError(t, err)
	assert.Empty(t, rows)

	assert.Error(t, s.DeleteCustomPricing(row.ID), "deleting twice must report the miss")
}

// Deleting an override frees the scope, so the same one can be created again.
func TestDeleteCustomPricingFreesTheScope(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)

	row, err := s.CreateCustomPricing(overrideRequest(core.ScopeGlobal, priceModel))
	require.NoError(t, err)
	require.NoError(t, s.DeleteCustomPricing(row.ID))

	_, err = s.CreateCustomPricing(overrideRequest(core.ScopeGlobal, priceModel))
	assert.NoError(t, err)
}

// ---------- base pricing round trip ----------

// BulkSyncModelPricing is upsert-by-raw-key, so a re-sync updates rather than
// duplicating - the catalog sync runs every interval.
func TestBulkSyncModelPricingIsIdempotent(t *testing.T) {
	s := store(t)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 1)
	seedBasePrice(t, s, core.ProviderOpenAI, priceModel, 5)

	rows, err := s.ListModelPricing()
	require.NoError(t, err)
	require.Len(t, rows, 1, "the same raw key must update, not insert")

	got := rows[0].ToCore()
	require.NotNil(t, got.Pricing.InputCostPerToken)
	assert.Equal(t, 5.0, *got.Pricing.InputCostPerToken, "the newer price wins")
	assert.Equal(t, core.ProviderOpenAI, got.Provider)
}

// A price for a provider with no row is dropped rather than failing the sync.
func TestBulkSyncModelPricingSkipsUnknownProviders(t *testing.T) {
	s := store(t)

	require.NoError(t, s.BulkSyncModelPricing([]core.PricingVariant{
		{
			RawKey: "openai/gpt-4o", Provider: core.ProviderOpenAI,
			ModelName: priceModel, ModelType: core.ModelTypeChat,
			Pricing: core.Pricing{InputCostPerToken: tptr(1.0)},
		},
		{
			RawKey: "ghost/model", Provider: "not-a-provider",
			ModelName: "ghost", ModelType: core.ModelTypeChat,
			Pricing: core.Pricing{InputCostPerToken: tptr(1.0)},
		},
	}))

	rows, err := s.ListModelPricing()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, priceModel, rows[0].ModelName)
}
