package modelcatalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
)

const (
	gpt4o   = "gpt-4o"
	mini    = "gpt-4o-mini"
	vkID    = "vk-1"
	sizeSel = `{"size":"1024x1024"}`
)

func ptr[T any](v T) *T { return &v }

func chatKey(provider core.Provider, model string) core.CatalogKey {
	return core.CatalogKey{Provider: provider, ModelName: model, ModelType: core.ModelTypeChat}
}

func meta(provider core.Provider, model string, kind core.ModelType) core.ModelMetadata {
	return core.ModelMetadata{
		ID: string(provider) + "/" + model, Provider: provider,
		ModelName: model, ModelType: kind,
	}
}

// RawKey carries the selector because model_pricing has a unique index on it.
// Two price points for one model must differ there or the store keeps one.
func variant(provider core.Provider, model, selectorKey string, in, out float64) core.PricingVariant {
	return core.PricingVariant{
		RawKey: model + selectorKey, Provider: provider, ModelName: model, ModelType: core.ModelTypeChat,
		Selectors: core.SelectorSet{Key: selectorKey},
		Pricing:   core.Pricing{InputCostPerToken: ptr(in), OutputCostPerToken: ptr(out)},
	}
}

// scopeVal is the provider name or the virtual key id, depending on scope.
func custom(scope core.ScopeType, model, scopeVal string, in float64) *core.CustomPricing {
	cp := &core.CustomPricing{
		Name: "override", ModelName: model, ModelType: core.ModelTypeChat, ScopeType: scope,
		Pricing: core.Pricing{InputCostPerToken: ptr(in)},
	}
	switch scope {
	case core.ScopeProvider:
		cp.ScopeProvider = ptr(core.Provider(scopeVal))
	case core.ScopeVirtualKey:
		cp.ScopeVirtualkeyID = ptr(scopeVal)
	}
	return cp
}

// The snapshots are atomic pointers, so a test publishes them directly and
// skips the store. A nil argument leaves that snapshot unloaded.
func newCatalog(t *testing.T, models []core.ModelMetadata, variants []core.PricingVariant, customs ...*core.CustomPricing) *ModelCatalog {
	t.Helper()
	c := &ModelCatalog{logger: zap.NewNop()}

	if models != nil {
		metadata := make(map[core.CatalogKey]*core.ModelMetadata, len(models))
		byProvider := make(map[core.Provider]map[string]struct{})
		for i := range models {
			md := &models[i]
			metadata[md.CatalogKey()] = md
			if byProvider[md.Provider] == nil {
				byProvider[md.Provider] = make(map[string]struct{})
			}
			byProvider[md.Provider][md.ModelName] = struct{}{}
		}
		c.models.Store(&ModelSnapshot{entries: models, metadata: metadata, byProvider: byProvider})
	}

	if variants != nil {
		c.basePricing.Store(newBasePricingSnapshot(variants))
	}

	if len(customs) > 0 {
		snap := make(CustomPriceSnapshot)
		for _, cp := range customs {
			ck := cp.CustomPricingKey()
			if snap[ck] == nil {
				snap[ck] = &core.CustomScopePricing{
					Provider:   make(map[core.Provider]*core.CustomPricing),
					VirtualKey: make(map[string]*core.CustomPricing),
				}
			}
			switch cp.ScopeType {
			case core.ScopeGlobal:
				snap[ck].Global = cp
			case core.ScopeProvider:
				snap[ck].Provider[*cp.ScopeProvider] = cp
			case core.ScopeVirtualKey:
				snap[ck].VirtualKey[*cp.ScopeVirtualkeyID] = cp
			}
		}
		c.customPricing.Store(&snap)
	}
	return c
}

func serve(t *testing.T, body string) (*CatalogSource, *http.Client) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return &CatalogSource{URL: server.URL}, server.Client()
}

const feedOneChat = `{
  "gpt-4o": {
    "mode": "chat", "provider": "openai",
    "max_tokens": 128000, "max_input_tokens": 127000, "max_output_tokens": 16384,
    "input_cost_per_token": 0.0000025, "output_cost_per_token": 0.00001,
    "supports_vision": true, "supports_function_calling": true
  }
}`

// ---------- models ----------

func TestModels(t *testing.T) {
	entries := []core.ModelMetadata{
		meta(core.ProviderOpenAI, gpt4o, core.ModelTypeChat),
		meta(core.ProviderOpenAI, mini, core.ModelTypeChat),
		meta(core.ProviderAnthropic, "claude", core.ModelTypeChat),
	}

	tests := []struct {
		name     string
		provider core.Provider
		want     []string
	}{
		{name: "empty provider returns everything", want: []string{gpt4o, mini, "claude"}},
		{name: "filters to one provider", provider: core.ProviderOpenAI, want: []string{gpt4o, mini}},
		{name: "single match", provider: core.ProviderAnthropic, want: []string{"claude"}},
		{name: "unknown provider returns nothing", provider: core.ProviderCohere},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var names []string
			for _, m := range newCatalog(t, entries, nil).Models(tc.provider) {
				names = append(names, m.ModelName)
			}
			assert.Equal(t, tc.want, names)
		})
	}
}

// Models hands back a copy: callers sort it, and live traffic reads the
// snapshot lock-free.
func TestModelsReturnsACopy(t *testing.T) {
	c := newCatalog(t, []core.ModelMetadata{
		meta(core.ProviderOpenAI, gpt4o, core.ModelTypeChat),
		meta(core.ProviderOpenAI, mini, core.ModelTypeChat),
	}, nil)

	got := c.Models("")
	require.Len(t, got, 2)
	got[0].ModelName = "clobbered"

	assert.Equal(t, gpt4o, c.Models("")[0].ModelName, "the snapshot must not be reachable through Models")
}

func TestUnloadedCatalog(t *testing.T) {
	c := &ModelCatalog{logger: zap.NewNop()}

	assert.Nil(t, c.Models(""))
	assert.Zero(t, c.ModelCount(core.ProviderOpenAI))
	assert.Nil(t, c.ModelsForProvider(core.ProviderOpenAI))
	assert.False(t, c.HasModel(core.ProviderOpenAI, gpt4o))
	assert.False(t, c.Ready())
	assert.Nil(t, c.BasePrice(chatKey(core.ProviderOpenAI, gpt4o)))
	assert.Nil(t, c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey))

	md, ok := c.Lookup(chatKey(core.ProviderOpenAI, gpt4o))
	assert.Nil(t, md)
	assert.False(t, ok)
}

// byProvider is keyed by name, so one name under two model types counts once.
// ModelCount answers "how many names", not "how many rows".
func TestModelCount(t *testing.T) {
	c := newCatalog(t, []core.ModelMetadata{
		meta(core.ProviderOpenAI, gpt4o, core.ModelTypeChat),
		meta(core.ProviderOpenAI, gpt4o, core.ModelTypeEmbedding),
		meta(core.ProviderOpenAI, mini, core.ModelTypeChat),
		meta(core.ProviderAnthropic, "claude", core.ModelTypeChat),
	}, nil)

	assert.Equal(t, 2, c.ModelCount(core.ProviderOpenAI))
	assert.Equal(t, 1, c.ModelCount(core.ProviderAnthropic))
	assert.Equal(t, 0, c.ModelCount(core.ProviderCohere))
	assert.Len(t, c.Models(core.ProviderOpenAI), 3, "Models still returns every row")
}

func TestHasModelAndLookup(t *testing.T) {
	c := newCatalog(t, []core.ModelMetadata{meta(core.ProviderOpenAI, gpt4o, core.ModelTypeChat)}, nil)

	assert.True(t, c.HasModel(core.ProviderOpenAI, gpt4o))
	assert.False(t, c.HasModel(core.ProviderOpenAI, "nope"))
	assert.False(t, c.HasModel(core.ProviderAnthropic, gpt4o))
	assert.False(t, c.HasModel(core.ProviderOpenAI, ""))

	md, ok := c.Lookup(chatKey(core.ProviderOpenAI, gpt4o))
	require.True(t, ok)
	assert.Equal(t, gpt4o, md.ModelName)

	_, ok = c.Lookup(core.CatalogKey{
		Provider: core.ProviderOpenAI, ModelName: gpt4o, ModelType: core.ModelTypeEmbedding,
	})
	assert.False(t, ok, "a different model type is a different key")
}

func TestReady(t *testing.T) {
	models := []core.ModelMetadata{meta(core.ProviderOpenAI, gpt4o, core.ModelTypeChat)}
	variants := []core.PricingVariant{variant(core.ProviderOpenAI, gpt4o, "", 1, 2)}

	tests := []struct {
		name     string
		models   []core.ModelMetadata
		variants []core.PricingVariant
		want     bool
	}{
		{name: "models and pricing loaded", models: models, variants: variants, want: true},
		{name: "no pricing", models: models},
		{name: "no models", variants: variants},
		{name: "empty model list", models: []core.ModelMetadata{}, variants: variants},
		{name: "empty pricing list", models: models, variants: []core.PricingVariant{}},
		{name: "nothing loaded"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, newCatalog(t, tc.models, tc.variants).Ready())
		})
	}
}

// ---------- base pricing ----------

// The selector row arrives first; the plain row must still win the base slot.
func TestBaseSnapshotPrefersTheUnselectedRow(t *testing.T) {
	snap := newBasePricingSnapshot([]core.PricingVariant{
		variant(core.ProviderOpenAI, gpt4o, sizeSel, 9, 9),
		variant(core.ProviderOpenAI, gpt4o, "", 1, 2),
	})

	base := snap.base[chatKey(core.ProviderOpenAI, gpt4o)]
	require.NotNil(t, base)
	assert.True(t, base.Selectors.IsEmpty())
	assert.Equal(t, 1.0, *base.Pricing.InputCostPerToken)
	assert.Equal(t, 1, snap.Len())
}

// A key with one row needs no variant bucket, so the snapshot drops it.
func TestBaseSnapshotPrunesSingleVariantBuckets(t *testing.T) {
	one := newBasePricingSnapshot([]core.PricingVariant{variant(core.ProviderOpenAI, gpt4o, "", 1, 2)})
	assert.Empty(t, one.variants)

	two := newBasePricingSnapshot([]core.PricingVariant{
		variant(core.ProviderOpenAI, gpt4o, "", 1, 2),
		variant(core.ProviderOpenAI, gpt4o, sizeSel, 9, 9),
	})
	assert.Len(t, two.variants, 1)
}

func TestBaseSnapshotFind(t *testing.T) {
	key := chatKey(core.ProviderOpenAI, gpt4o)
	snap := newBasePricingSnapshot([]core.PricingVariant{
		variant(core.ProviderOpenAI, gpt4o, "", 1, 2),
		variant(core.ProviderOpenAI, gpt4o, sizeSel, 9, 9),
	})

	tests := []struct {
		name      string
		key       core.CatalogKey
		selector  string
		wantInput *float64
	}{
		{name: "matching selector wins", key: key, selector: sizeSel, wantInput: ptr(9.0)},
		{name: "unknown selector falls back to base", key: key, selector: `{"size":"nope"}`, wantInput: ptr(1.0)},
		{name: "empty selector picks base", key: key, selector: core.EmptySelectorKey, wantInput: ptr(1.0)},
		{name: "unknown key returns nil", key: chatKey(core.ProviderOpenAI, "nope"), selector: core.EmptySelectorKey},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := snap.find(tc.key, tc.selector)
			if tc.wantInput == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.wantInput, *got.Pricing.InputCostPerToken)
		})
	}
}

// BasePrice is the list price and must not see overrides - that is why the
// catalog tab and the overrides tab are separate.
func TestBasePriceIgnoresOverrides(t *testing.T) {
	c := newCatalog(t, nil,
		[]core.PricingVariant{variant(core.ProviderOpenAI, gpt4o, "", 1, 2)},
		custom(core.ScopeGlobal, gpt4o, "", 99))

	base := c.BasePrice(chatKey(core.ProviderOpenAI, gpt4o))
	require.NotNil(t, base)
	assert.Equal(t, 1.0, *base.InputCostPerToken, "BasePrice returns the unmerged list price")

	resolved := c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey)
	require.NotNil(t, resolved)
	assert.Equal(t, 99.0, *resolved.InputCostPerToken, "ResolvePrice applies it")

	assert.Nil(t, c.BasePrice(chatKey(core.ProviderOpenAI, "nope")))
}

// ---------- custom pricing and precedence ----------

func TestResolvePricePrecedence(t *testing.T) {
	global := custom(core.ScopeGlobal, gpt4o, "", 10)
	provider := custom(core.ScopeProvider, gpt4o, string(core.ProviderOpenAI), 20)
	virtualKey := custom(core.ScopeVirtualKey, gpt4o, vkID, 30)
	otherProvider := custom(core.ScopeProvider, gpt4o, string(core.ProviderAnthropic), 40)
	otherVK := custom(core.ScopeVirtualKey, gpt4o, "vk-other", 50)

	tests := []struct {
		name      string
		customs   []*core.CustomPricing
		vkID      string
		wantInput float64
	}{
		{name: "no overrides uses base", wantInput: 1},
		{name: "global applies", customs: []*core.CustomPricing{global}, wantInput: 10},
		{name: "provider beats global", customs: []*core.CustomPricing{global, provider}, wantInput: 20},
		{
			name:    "virtual key beats provider and global",
			customs: []*core.CustomPricing{global, provider, virtualKey},
			vkID:    vkID, wantInput: 30,
		},
		{
			name:    "another key falls back to provider",
			customs: []*core.CustomPricing{global, provider, virtualKey},
			vkID:    "vk-someone-else", wantInput: 20,
		},
		{name: "another provider's override does not apply", customs: []*core.CustomPricing{global, otherProvider}, wantInput: 10},
		{
			name:    "another key's override does not apply",
			customs: []*core.CustomPricing{global, otherVK},
			vkID:    vkID, wantInput: 10,
		},
		{
			name:    "unmatched key scope falls to base",
			customs: []*core.CustomPricing{virtualKey},
			vkID:    "vk-someone-else", wantInput: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newCatalog(t, nil,
				[]core.PricingVariant{variant(core.ProviderOpenAI, gpt4o, "", 1, 2)}, tc.customs...)

			got := c.ResolvePrice(tc.vkID, chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantInput, *got.InputCostPerToken)
		})
	}
}

// An override merges onto the base, so a field it leaves nil keeps the list
// price. Selector resolution happens first, so it merges onto the variant that
// was actually selected.
func TestResolvePriceMerges(t *testing.T) {
	c := newCatalog(t, nil, []core.PricingVariant{
		variant(core.ProviderOpenAI, gpt4o, "", 1, 2),
		variant(core.ProviderOpenAI, gpt4o, sizeSel, 9, 8),
	}, custom(core.ScopeGlobal, gpt4o, "", 99))

	base := c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey)
	require.NotNil(t, base)
	assert.Equal(t, 99.0, *base.InputCostPerToken, "the override wins where it is set")
	assert.Equal(t, 2.0, *base.OutputCostPerToken, "the base survives where the override is nil")

	sized := c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), sizeSel)
	require.NotNil(t, sized)
	assert.Equal(t, 99.0, *sized.InputCostPerToken)
	assert.Equal(t, 8.0, *sized.OutputCostPerToken, "the selected variant's price must survive")

	stored := c.basePricing.Load().base[chatKey(core.ProviderOpenAI, gpt4o)]
	assert.Equal(t, 1.0, *stored.Pricing.InputCostPerToken, "merging must not write back into the snapshot")
}

// CustomPricingKey drops the provider, so an override keyed on (name, type)
// reaches that model under every provider.
func TestResolvePriceCustomKeyIgnoresProvider(t *testing.T) {
	c := newCatalog(t, nil, []core.PricingVariant{
		variant(core.ProviderOpenAI, gpt4o, "", 1, 2),
		variant(core.ProviderAzure, gpt4o, "", 3, 4),
	}, custom(core.ScopeGlobal, gpt4o, "", 10))

	for _, provider := range []core.Provider{core.ProviderOpenAI, core.ProviderAzure} {
		got := c.ResolvePrice("", chatKey(provider, gpt4o), core.EmptySelectorKey)
		require.NotNil(t, got)
		assert.Equal(t, 10.0, *got.InputCostPerToken, string(provider))
	}
}

func TestResolvePriceNeedsABasePrice(t *testing.T) {
	c := newCatalog(t, nil, []core.PricingVariant{}, custom(core.ScopeGlobal, gpt4o, "", 10))

	assert.Nil(t, c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey),
		"an override on a model with no list price must not invent one")
}

// ---------- the client ----------

func TestFetchParsesAFeed(t *testing.T) {
	src, client := serve(t, feedOneChat)

	models, variants, err := src.Fetch(context.Background(), client)
	require.NoError(t, err)
	require.Len(t, *models, 1)
	require.Len(t, *variants, 1)

	m := (*models)[0]
	assert.Equal(t, core.ProviderOpenAI, m.Provider)
	assert.Equal(t, gpt4o, m.ModelName)
	assert.Equal(t, core.ModelTypeChat, m.ModelType)
	assert.Equal(t, gpt4o, m.SourceRawKey)
	assert.EqualValues(t, 128000, m.Limits.ContextWindow)
	assert.EqualValues(t, 16384, m.Limits.MaxOutputTokens)
	assert.NotZero(t, m.Capability&core.CapVision)

	assert.InDelta(t, 0.0000025, *(*variants)[0].Pricing.InputCostPerToken, 1e-12)
	assert.True(t, (*variants)[0].Selectors.IsEmpty())
	assert.Equal(t, 1, src.TotalSourceCount)
	assert.NotEmpty(t, src.Digest, "a successful fetch must record a digest")
}

// The digest is over the raw bytes: identical feeds hash the same, a changed
// feed does not.
func TestFetchDigestTracksTheBody(t *testing.T) {
	digest := func(body string) string {
		src, client := serve(t, body)
		_, _, err := src.Fetch(context.Background(), client)
		require.NoError(t, err)
		return src.Digest
	}

	assert.Equal(t, digest(feedOneChat), digest(feedOneChat))
	assert.NotEqual(t, digest(feedOneChat), digest(strings.Replace(feedOneChat, "128000", "64000", 1)))
}

func TestFetchFailures(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			src := &CatalogSource{URL: server.URL}
			_, _, err := src.Fetch(context.Background(), server.Client())
			server.Close()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "status")
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := server.URL
		server.Close()

		_, _, err := (&CatalogSource{URL: url}).Fetch(context.Background(), http.DefaultClient)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetch")
	})

	t.Run("cancelled context", func(t *testing.T) {
		src, client := serve(t, feedOneChat)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := src.Fetch(ctx, client)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("malformed json", func(t *testing.T) {
		bodies := map[string]string{
			"truncated":        `{"gpt-4o": {"mode": "chat", "provider": "openai"`,
			"not an object":    `[1,2,3]`,
			"unknown mode":     `{"x": {"mode": "telepathy", "provider": "openai"}}`,
			"missing provider": `{"x": {"mode": "chat"}}`,
			"empty body":       ``,
		}
		for name, body := range bodies {
			src, client := serve(t, body)
			_, _, err := src.Fetch(context.Background(), client)
			assert.Error(t, err, name)
		}
	})
}

func TestFetchEmptyFeed(t *testing.T) {
	src, client := serve(t, `{}`)

	models, variants, err := src.Fetch(context.Background(), client)
	require.NoError(t, err)
	assert.Empty(t, *models)
	assert.Empty(t, *variants)
	assert.Zero(t, src.TotalSourceCount)
}

// A row with no mode is a control row in the feed, not a model. It is counted
// and skipped rather than failing the whole sync.
func TestFetchSkipsControlRows(t *testing.T) {
	src, client := serve(t, `{
	  "sample_spec": {"provider": "openai", "notes": "not a model"},
	  "gpt-4o": {"mode": "chat", "provider": "openai"},
	  "another_control": {"mode": "   "}
	}`)

	models, _, err := src.Fetch(context.Background(), client)
	require.NoError(t, err)
	assert.Len(t, *models, 1)
	assert.Equal(t, 2, src.TotalUnknown)
	assert.Equal(t, 3, src.TotalSourceCount)
}

func TestFetchNilClientUsesDefault(t *testing.T) {
	src, _ := serve(t, feedOneChat)

	_, _, err := src.Fetch(context.Background(), nil)
	assert.NoError(t, err, "a nil client must fall back to http.DefaultClient")
}

// ---------- parsing ----------

func TestUnmarshal(t *testing.T) {
	t.Run("provider aliases", func(t *testing.T) {
		aliases := map[string]core.Provider{
			"azure_text":                    "azure",
			"text-completion-openai":        "openai",
			"text-completion-inception":     "inception",
			"fireworks_ai-embedding-models": "fireworks_ai",
			"amazon_nova":                   "amazon-nova",
			"openai":                        "openai",
			"some-new-provider":             "some-new-provider",
		}
		for feed, want := range aliases {
			var s SourceSchema
			require.NoError(t, json.Unmarshal([]byte(`{"mode":"chat","provider":"`+feed+`"}`), &s))
			assert.Equal(t, want, s.diffractProvider, feed)
		}
	})

	t.Run("control row", func(t *testing.T) {
		var s SourceSchema
		assert.ErrorIs(t, json.Unmarshal([]byte(`{"provider":"openai"}`), &s), errControlRow)
	})

	// The feed nests the three search-context prices; the IR keeps them flat.
	t.Run("search context is lifted", func(t *testing.T) {
		var s SourceSchema
		require.NoError(t, json.Unmarshal([]byte(`{
		  "mode":"chat","provider":"openai",
		  "search_context_cost_per_query": {
		    "search_context_size_low": 0.01,
		    "search_context_size_medium": 0.02,
		    "search_context_size_high": 0.03
		  }}`), &s))

		assert.Equal(t, 0.01, *s.SearchContextCostPerQueryLow)
		assert.Equal(t, 0.02, *s.SearchContextCostPerQueryMedium)
		assert.Equal(t, 0.03, *s.SearchContextCostPerQueryHigh)
	})

	t.Run("capability bits", func(t *testing.T) {
		var s SourceSchema
		require.NoError(t, json.Unmarshal([]byte(`{
		  "mode":"chat","provider":"openai",
		  "supports_vision": true, "supports_function_calling": true,
		  "supports_reasoning": false}`), &s))

		assert.NotZero(t, s.diffractCapabilities&core.CapVision)
		assert.NotZero(t, s.diffractCapabilities&core.CapFunctionCalling)
		assert.Zero(t, s.diffractCapabilities&core.CapReasoning, "false must not set the bit")
		assert.Zero(t, s.diffractCapabilities&core.CapAudioInput, "absent must not set the bit")
	})
}

func TestIsQualityMarker(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  bool
	}{
		{name: "quality then size", parts: []string{"hd", "1024-x-1024", "dall-e-3"}, want: true},
		{name: "max in size", parts: []string{"high", "max-x-max", "model"}, want: true},
		{name: "unknown quality word", parts: []string{"ultra", "1024-x-1024", "model"}},
		{name: "second part is not a size", parts: []string{"hd", "dall-e-3", "x"}},
		{name: "too few parts", parts: []string{"hd", "1024-x-1024"}},
		{name: "empty", parts: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isQualityMarker(tc.parts))
		})
	}
}

// The raw key carries the provider prefix and, for image models, the selector
// markers. Both come out of the model name.
func TestModelParserKeyShapes(t *testing.T) {
	tests := []struct {
		name, rawKey, mode, wantModel, wantSelector string
	}{
		{name: "bare key", rawKey: gpt4o, mode: "chat", wantModel: gpt4o},
		{name: "provider prefix stripped", rawKey: "openai/gpt-4o", mode: "chat", wantModel: gpt4o},
		{name: "prefix kept when it is not the provider", rawKey: "ft:gpt-4o", mode: "chat", wantModel: "ft:gpt-4o"},
		{name: "nested path kept", rawKey: "openai/org/gpt-4o", mode: "chat", wantModel: "org/gpt-4o"},
		{
			name: "size marker becomes a selector", rawKey: "1024-x-1024/dall-e-3",
			mode: "image_generation", wantModel: "dall-e-3", wantSelector: sizeSel,
		},
		{
			name: "quality and size markers", rawKey: "hd/1024-x-1024/dall-e-3",
			mode: "image_generation", wantModel: "dall-e-3",
			wantSelector: `{"quality":"hd","size":"1024x1024"}`,
		},
		{
			name: "steps marker", rawKey: "50-steps/flux",
			mode: "image_generation", wantModel: "flux", wantSelector: `{"steps":"50"}`,
		},
		{
			name: "chat mode does not consume markers", rawKey: "1024-x-1024/weird-model",
			mode: "chat", wantModel: "1024-x-1024/weird-model",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				tc.rawKey: map[string]any{"mode": tc.mode, "provider": "openai"},
			})
			require.NoError(t, err)

			rows, err := (&CatalogSource{}).modelParser(strings.NewReader(string(body)))
			require.NoError(t, err)
			require.Len(t, rows, 1)

			assert.Equal(t, tc.wantModel, rows[0].diffractModelName)
			assert.Equal(t, tc.rawKey, rows[0].diffractRawKey)

			want := tc.wantSelector
			if want == "" {
				want = core.EmptySelectorKey
			}
			assert.Equal(t, want, rows[0].diffractSelectorSet.CanonicalKey())
		})
	}
}

func TestModelParserRejects(t *testing.T) {
	tests := []struct {
		name, rawKey, mode, wantErr string
	}{
		{name: "duplicate size", rawKey: "1024-x-1024/512-x-512/dall-e-3", mode: "image_generation", wantErr: "duplicate"},
		{name: "duplicate steps", rawKey: "50-steps/25-steps/flux", mode: "image_generation", wantErr: "duplicate"},
		{name: "empty model name", rawKey: "openai/", mode: "chat", wantErr: "empty model name"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				tc.rawKey: map[string]any{"mode": tc.mode, "provider": "openai"},
			})
			require.NoError(t, err)

			_, err = (&CatalogSource{}).modelParser(strings.NewReader(string(body)))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// One model with several selector variants is one metadata row and one pricing
// row per variant. The same name under two modes is two models.
func TestBuildDeduplicatesOnCatalogKey(t *testing.T) {
	src, client := serve(t, `{
	  "1024-x-1024/dall-e-3":    {"mode":"image_generation","provider":"openai"},
	  "512-x-512/dall-e-3":      {"mode":"image_generation","provider":"openai"},
	  "hd/1024-x-1024/dall-e-3": {"mode":"image_generation","provider":"openai"},
	  "text-embedding-3-small":        {"mode":"embedding","provider":"openai"},
	  "openai/text-embedding-3-small": {"mode":"chat","provider":"openai"}
	}`)

	models, variants, err := src.Fetch(context.Background(), client)
	require.NoError(t, err)

	assert.Len(t, *models, 3, "dall-e-3 once, the embedding name twice for two modes")
	assert.Len(t, *variants, 5, "every price point survives")

	selectorKeys := make(map[string]struct{})
	for _, v := range *variants {
		if v.ModelName == "dall-e-3" {
			selectorKeys[v.Selectors.CanonicalKey()] = struct{}{}
		}
	}
	assert.Len(t, selectorKeys, 3, "each dall-e-3 variant carries a distinct selector key")
}

// End to end without a database: a fetched feed resolves to a price through the
// same snapshots live traffic reads.
func TestFetchedFeedResolvesThroughTheSnapshot(t *testing.T) {
	src, client := serve(t, feedOneChat)

	models, variants, err := src.Fetch(context.Background(), client)
	require.NoError(t, err)

	c := newCatalog(t, *models, *variants)
	require.True(t, c.Ready())
	assert.True(t, c.HasModel(core.ProviderOpenAI, gpt4o))

	got := c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey)
	require.NotNil(t, got)
	assert.InDelta(t, 0.0000025, *got.InputCostPerToken, 1e-12)
}

// ---------- the load paths and the worker ----------

// NewStore is a sync.Once singleton, so one database serves the whole test
// binary. Each caller gets it empty except for the seeded provider rows.
func testStore(t *testing.T) *dbstore.Store {
	t.Helper()

	path := filepath.Join(os.TempDir(), "diffractllm_catalog_test.db")
	store, err := dbstore.NewStore(path, strings.Repeat("k", 32), zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	require.NoError(t, store.Seed(false))

	for _, table := range []string{"model_pricing_override", "model_pricing", "model_metadata"} {
		require.NoError(t, store.DB.Exec("DELETE FROM "+table).Error)
	}
	return store
}

func startCatalog(t *testing.T, store *dbstore.Store, cfg config.ModelCatalogConfig) *ModelCatalog {
	t.Helper()
	c := NewModelCatalog(store, cfg, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		assert.NoError(t, c.Shutdown(shutdownCtx))
		cancel()
	})
	return c
}

func TestLoadAllOnAnEmptyDatabase(t *testing.T) {
	c := NewModelCatalog(testStore(t), config.ModelCatalogConfig{}, zap.NewNop())
	require.NoError(t, c.loadAll())

	assert.False(t, c.Ready(), "nothing synced yet")
	assert.Empty(t, c.Models(""))
	assert.NotNil(t, c.models.Load(), "an empty table still publishes a snapshot")
	assert.NotNil(t, c.basePricing.Load())
	assert.NotNil(t, c.customPricing.Load())
}

// The round trip the pure tests cannot reach: rows in, snapshot out.
func TestLoadFromTheStore(t *testing.T) {
	store := testStore(t)
	require.NoError(t, store.BulkSyncModelMetadata([]core.ModelMetadata{
		meta(core.ProviderOpenAI, gpt4o, core.ModelTypeChat),
		meta(core.ProviderOpenAI, mini, core.ModelTypeChat),
		meta(core.ProviderAnthropic, "claude", core.ModelTypeChat),
	}))
	require.NoError(t, store.BulkSyncModelPricing([]core.PricingVariant{
		variant(core.ProviderOpenAI, gpt4o, "", 1, 2),
		variant(core.ProviderOpenAI, gpt4o, sizeSel, 9, 8),
	}))

	c := NewModelCatalog(store, config.ModelCatalogConfig{}, zap.NewNop())
	require.NoError(t, c.loadAll())

	assert.True(t, c.Ready())
	assert.Equal(t, 2, c.ModelCount(core.ProviderOpenAI))
	assert.Equal(t, 1, c.ModelCount(core.ProviderAnthropic))

	md, ok := c.Lookup(chatKey(core.ProviderOpenAI, gpt4o))
	require.True(t, ok)
	assert.NotEmpty(t, md.ID, "the store assigns an id the feed does not have")

	base := c.BasePrice(chatKey(core.ProviderOpenAI, gpt4o))
	require.NotNil(t, base)
	assert.Equal(t, 1.0, *base.InputCostPerToken, "the unselected row wins the base slot")

	sized := c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), sizeSel)
	require.NotNil(t, sized)
	assert.Equal(t, 9.0, *sized.InputCostPerToken, "the selector variant survives the round trip")
}

// A model whose provider has no row is dropped, not fatal.
func TestBulkSyncSkipsUnknownProviders(t *testing.T) {
	store := testStore(t)
	require.NoError(t, store.BulkSyncModelMetadata([]core.ModelMetadata{
		meta(core.ProviderOpenAI, gpt4o, core.ModelTypeChat),
		meta("not-a-provider", "ghost", core.ModelTypeChat),
	}))

	c := NewModelCatalog(store, config.ModelCatalogConfig{}, zap.NewNop())
	require.NoError(t, c.loadModels())

	assert.Equal(t, 1, c.ModelCount(core.ProviderOpenAI))
	assert.False(t, c.HasModel("not-a-provider", "ghost"))
}

func TestLoadCustomPricingGroupsByScope(t *testing.T) {
	store := testStore(t)
	require.NoError(t, store.BulkSyncModelPricing([]core.PricingVariant{
		variant(core.ProviderOpenAI, gpt4o, "", 1, 2),
	}))

	for _, req := range []core.CustomPricingRequest{
		{
			Name: "global", ModelName: gpt4o, ModelType: "chat", ScopeType: core.ScopeGlobal,
			Pricing: core.Pricing{InputCostPerToken: ptr(10.0)},
		},
		{
			Name: "provider", ModelName: gpt4o, ModelType: "chat", ScopeType: core.ScopeProvider,
			ScopeProvider: ptr(core.ProviderOpenAI),
			Pricing:       core.Pricing{InputCostPerToken: ptr(20.0)},
		},
	} {
		_, err := store.CreateCustomPricing(req)
		require.NoError(t, err, req.Name)
	}

	c := NewModelCatalog(store, config.ModelCatalogConfig{}, zap.NewNop())
	require.NoError(t, c.loadAll())

	scoped := (*c.customPricing.Load())[core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeChat}]
	require.NotNil(t, scoped, "the snapshot key drops the provider")
	require.NotNil(t, scoped.Global)
	require.Contains(t, scoped.Provider, core.ProviderOpenAI)

	got := c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey)
	require.NotNil(t, got)
	assert.Equal(t, 20.0, *got.InputCostPerToken, "provider scope beats global after a real load")
}

func TestReloadCustomPricingPublishesANewSnapshot(t *testing.T) {
	store := testStore(t)
	require.NoError(t, store.BulkSyncModelPricing([]core.PricingVariant{
		variant(core.ProviderOpenAI, gpt4o, "", 1, 2),
	}))

	c := NewModelCatalog(store, config.ModelCatalogConfig{}, zap.NewNop())
	require.NoError(t, c.loadAll())

	price := func() float64 {
		got := c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey)
		require.NotNil(t, got)
		return *got.InputCostPerToken
	}
	assert.Equal(t, 1.0, price())

	_, err := store.CreateCustomPricing(core.CustomPricingRequest{
		Name: "global", ModelName: gpt4o, ModelType: "chat", ScopeType: core.ScopeGlobal,
		Pricing: core.Pricing{InputCostPerToken: ptr(77.0)},
	})
	require.NoError(t, err)

	assert.Equal(t, 1.0, price(), "the snapshot must not see the row until it is reloaded")
	require.NoError(t, c.ReloadCustomPricing())
	assert.Equal(t, 77.0, price())
}

// RunAtStart fires the sync once during Start because an empty catalog is not
// Ready. That is fetch, write and reload end to end.
func TestStartRunsTheSyncJobOnFirstBoot(t *testing.T) {
	src, _ := serve(t, feedOneChat)
	c := startCatalog(t, testStore(t), config.ModelCatalogConfig{
		SourceURL: src.URL, SyncInterval: time.Hour,
	})

	require.Eventually(t, c.Ready, 5*time.Second, 20*time.Millisecond,
		"RunAtStart must sync and publish before the interval elapses")

	assert.True(t, c.HasModel(core.ProviderOpenAI, gpt4o))
	got := c.ResolvePrice("", chatKey(core.ProviderOpenAI, gpt4o), core.EmptySelectorKey)
	require.NotNil(t, got)
	assert.InDelta(t, 0.0000025, *got.InputCostPerToken, 1e-12)

	stats := c.Stats()
	require.Len(t, stats, 1)
	assert.Equal(t, JobCatalogSync, stats[0].Name)
	assert.Empty(t, stats[0].LastError)
	assert.False(t, stats[0].LastSuccessAt.IsZero())
	assert.Equal(t, 1, stats[0].LastDetail["models_fetched"])
	assert.NotEmpty(t, stats[0].LastDetail["digest"])
}

// Counting fetches, not timestamps: a sync takes under a millisecond, so two
// runs can stamp the same LastSuccessAt and no comparison of it can be trusted.
func TestSyncNowTriggersTheJob(t *testing.T) {
	var fetches atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		_, _ = w.Write([]byte(feedOneChat))
	}))
	defer server.Close()

	c := startCatalog(t, testStore(t), config.ModelCatalogConfig{
		SourceURL: server.URL, SyncInterval: time.Hour,
	})
	require.Eventually(t, c.Ready, 5*time.Second, 20*time.Millisecond)
	require.EqualValues(t, 1, fetches.Load(), "RunAtStart fetches once")

	require.NoError(t, c.SyncNow())
	assert.Eventually(t, func() bool { return fetches.Load() == 2 },
		5*time.Second, 20*time.Millisecond, "SyncNow must run the job again")
	assert.Empty(t, c.Stats()[0].LastError)

	assert.Error(t, c.workers.Trigger("not_a_job"))
}

// A failing feed leaves the previous snapshot serving and records the error.
func TestSyncFailureKeepsTheLastGoodSnapshot(t *testing.T) {
	var body atomic.Value
	body.Store(feedOneChat)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b := body.Load().(string); b != "" {
			_, _ = w.Write([]byte(b))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := startCatalog(t, testStore(t), config.ModelCatalogConfig{
		SourceURL: server.URL, SyncInterval: time.Hour,
	})
	require.Eventually(t, c.Ready, 5*time.Second, 20*time.Millisecond)

	body.Store("")
	require.NoError(t, c.SyncNow())
	require.Eventually(t, func() bool { return c.Stats()[0].LastError != "" },
		5*time.Second, 20*time.Millisecond, "the failure must be recorded")

	assert.True(t, c.Ready(), "a failed sync must not clear the catalog")
	assert.True(t, c.HasModel(core.ProviderOpenAI, gpt4o))
	assert.Equal(t, 1, c.Stats()[0].ConsecutiveFailures)
}

func TestSyncCatalogReportsWhatItFetched(t *testing.T) {
	src, _ := serve(t, `{
	  "sample_spec": {"provider":"openai"},
	  "gpt-4o":      {"mode":"chat","provider":"openai","input_cost_per_token":0.001},
	  "1024-x-1024/dall-e-3": {"mode":"image_generation","provider":"openai"},
	  "512-x-512/dall-e-3":   {"mode":"image_generation","provider":"openai"}
	}`)

	c := NewModelCatalog(testStore(t), config.ModelCatalogConfig{SourceURL: src.URL}, zap.NewNop())

	detail, err := c.syncCatalog(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, detail["models_fetched"], "gpt-4o and dall-e-3")
	assert.Equal(t, 3, detail["variants_fetched"], "dall-e-3 has two price points")
	assert.Equal(t, 1, detail["models_unknown"], "the control row is counted, not fatal")
	assert.NotEmpty(t, detail["digest"])

	assert.True(t, c.HasModel(core.ProviderOpenAI, "dall-e-3"), "syncCatalog reloads before returning")
}

func TestSyncCatalogFetchError(t *testing.T) {
	c := NewModelCatalog(testStore(t), config.ModelCatalogConfig{
		SourceURL: "http://127.0.0.1:1/nope",
	}, zap.NewNop())

	detail, err := c.syncCatalog(context.Background())
	require.Error(t, err)
	assert.Nil(t, detail)
	assert.Contains(t, err.Error(), "fetch catalog feed")
}
