package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pcfg(provider Provider, weight float32, allowed ...string) ProviderConfig {
	if len(allowed) == 0 {
		allowed = []string{"*"}
	}
	return ProviderConfig{Provider: provider, AllowedModels: allowed, Weight: weight}
}

// Weighted mode requires the weights to sum to 1.0, so an all-zero config or a
// partial split is refused at compile time rather than degenerating later in
// pickProvider.
func TestCompileProviderConfigsWeightValidation(t *testing.T) {
	tests := []struct {
		name    string
		mode    VKMode
		configs []ProviderConfig
		wantErr string
	}{
		{
			name:    "weighted all zero is rejected",
			mode:    VKWeighted,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 0), pcfg(ProviderAzure, 0)},
			wantErr: "must sum to 1.0",
		},
		{
			name:    "weighted under one is rejected",
			mode:    VKWeighted,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 0.3), pcfg(ProviderAzure, 0.3)},
			wantErr: "must sum to 1.0",
		},
		{
			name:    "weighted over one is rejected",
			mode:    VKWeighted,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 0.7), pcfg(ProviderAzure, 0.7)},
			wantErr: "must sum to 1.0",
		},
		{
			name:    "negative weight is rejected",
			mode:    VKWeighted,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, -0.5), pcfg(ProviderAzure, 1.5)},
			wantErr: "weight must be finite",
		},
		{
			name:    "weight above one is rejected",
			mode:    VKWeighted,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 1.5)},
			wantErr: "weight must be finite",
		},
		{
			name:    "weighted summing to one is accepted",
			mode:    VKWeighted,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 0.5), pcfg(ProviderAzure, 0.5)},
		},
		{
			name:    "weighted single provider at one is accepted",
			mode:    VKWeighted,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 1)},
		},
		{
			name:    "direct ignores weight entirely",
			mode:    VKDirect,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 0)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CompileProviderConfigs(tc.mode, tc.configs)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, len(tc.configs))
		})
	}
}

func TestCompileProviderConfigsStructuralValidation(t *testing.T) {
	tests := []struct {
		name    string
		mode    VKMode
		configs []ProviderConfig
		wantErr string
	}{
		{name: "no configs", mode: VKDirect, wantErr: "at least one provider config"},
		{
			name:    "direct with two providers",
			mode:    VKDirect,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 0), pcfg(ProviderAzure, 0)},
			wantErr: "exactly one provider config",
		},
		{
			name:    "empty provider name",
			mode:    VKDirect,
			configs: []ProviderConfig{pcfg("   ", 0)},
			wantErr: "provider cannot be empty",
		},
		{
			name:    "duplicate provider",
			mode:    VKWeighted,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 0.5), pcfg(ProviderOpenAI, 0.5)},
			wantErr: "duplicate provider",
		},
		{
			name:    "no allowed models",
			mode:    VKDirect,
			configs: []ProviderConfig{{Provider: ProviderOpenAI}},
			wantErr: "at least one allowed model",
		},
		{
			name:    "empty allowed model entry",
			mode:    VKDirect,
			configs: []ProviderConfig{pcfg(ProviderOpenAI, 0, "gpt-4o", "  ")},
			wantErr: "contains an empty model",
		},
		{
			name: "blocking everything is rejected",
			mode: VKDirect,
			configs: []ProviderConfig{{
				Provider: ProviderOpenAI, AllowedModels: []string{"*"}, BlockedModels: []string{"*"},
			}},
			wantErr: "cannot block every model",
		},
		{
			name: "empty blocked model entry",
			mode: VKDirect,
			configs: []ProviderConfig{{
				Provider: ProviderOpenAI, AllowedModels: []string{"*"}, BlockedModels: []string{" "},
			}},
			wantErr: "empty blocked model",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileProviderConfigs(tc.mode, tc.configs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// Duplicates inside one config's lists are collapsed rather than rejected.
func TestCompileProviderConfigsDeduplicates(t *testing.T) {
	got, err := CompileProviderConfigs(VKDirect, []ProviderConfig{{
		Provider:      ProviderOpenAI,
		AllowedModels: []string{"gpt-4o", "gpt-4o", "gpt-4o-mini"},
		BlockedModels: []string{"o1", "o1"},
	}})
	require.NoError(t, err)
	require.Len(t, got, 1)

	assert.Equal(t, []string{"gpt-4o", "gpt-4o-mini"}, got[0].AllowedModels)
	assert.Equal(t, []string{"o1"}, got[0].BlockedModels)
}

// Blocked wins over the allowlist, including over the wildcard, so an operator
// can carve one model out without enumerating everything else.
func TestIsModelAllowed(t *testing.T) {
	tests := []struct {
		name     string
		allowed  []string
		blocked  []string
		model    string
		provider Provider
		want     bool
	}{
		{name: "wildcard allows", allowed: []string{"*"}, model: "gpt-4o", want: true},
		{name: "explicit allows", allowed: []string{"gpt-4o"}, model: "gpt-4o", want: true},
		{name: "not in the list", allowed: []string{"gpt-4o-mini"}, model: "gpt-4o"},
		{
			name: "blocked beats wildcard", allowed: []string{"*"},
			blocked: []string{"gpt-4o"}, model: "gpt-4o",
		},
		{
			name: "blocked beats explicit", allowed: []string{"gpt-4o"},
			blocked: []string{"gpt-4o"}, model: "gpt-4o",
		},
		{
			name: "another model stays allowed under a block", allowed: []string{"*"},
			blocked: []string{"o1"}, model: "gpt-4o", want: true,
		},
		{name: "empty model name", allowed: []string{"*"}},
		{
			name: "wrong provider", allowed: []string{"*"}, model: "gpt-4o",
			provider: ProviderAzure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := CompileProviderConfigs(VKDirect, []ProviderConfig{{
				Provider: ProviderOpenAI, AllowedModels: tc.allowed, BlockedModels: tc.blocked,
			}})
			require.NoError(t, err)

			provider := tc.provider
			if provider == "" {
				provider = ProviderOpenAI
			}
			assert.Equal(t, tc.want, compiled[0].IsModelAllowed(CatalogKey{
				Provider: provider, ModelName: tc.model, ModelType: ModelTypeChat,
			}))
		})
	}
}

func TestIsModelAllowedNilConfig(t *testing.T) {
	var config *ProviderConfig
	assert.False(t, config.IsModelAllowed(CatalogKey{
		Provider: ProviderOpenAI, ModelName: "gpt-4o",
	}))
}

// A bare model name is probed against every provider on the key, so one
// provider allowing it is enough - and a block on that provider is not.
func TestIsModelKeyAllowed(t *testing.T) {
	compiled, err := CompileProviderConfigs(VKWeighted, []ProviderConfig{
		{Provider: ProviderOpenAI, AllowedModels: []string{"gpt-4o"}, Weight: 0.5},
		{Provider: ProviderAzure, AllowedModels: []string{"*"}, BlockedModels: []string{"o1"}, Weight: 0.5},
	})
	require.NoError(t, err)
	vk := &VirtualKey{ProviderConfigs: compiled}

	assert.True(t, vk.IsModelKeyAllowed(CatalogKey{ModelName: "gpt-4o"}))
	assert.True(t, vk.IsModelKeyAllowed(CatalogKey{ModelName: "claude"}), "azure allows everything")
	assert.False(t, vk.IsModelKeyAllowed(CatalogKey{ModelName: "o1"}),
		"azure blocks it and openai does not list it")

	assert.True(t, vk.IsModelKeyAllowed(CatalogKey{Provider: ProviderOpenAI, ModelName: "gpt-4o"}))
	assert.False(t, vk.IsModelKeyAllowed(CatalogKey{Provider: ProviderOpenAI, ModelName: "claude"}),
		"an explicit provider is not widened by the other configs")
	assert.False(t, vk.IsModelKeyAllowed(CatalogKey{Provider: ProviderCohere, ModelName: "gpt-4o"}))
}
