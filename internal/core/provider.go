package core

import (
	"sort"
	"strings"
)

type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderAzure     Provider = "azure"
	ProviderCohere    Provider = "cohere"
	ProviderOllama    Provider = "ollama"
	ProviderCustom    Provider = "custom"
)

var supportedProviders = map[Provider]struct{}{
	ProviderOpenAI:    {},
	ProviderAnthropic: {},
	ProviderAzure:     {},
	ProviderCohere:    {},
	ProviderOllama:    {},
	ProviderCustom:    {},
}

func IsKnownProvider(name string) bool {
	_, ok := supportedProviders[Provider(name)]
	return ok
}

func SupportedProviders() []Provider {
	out := make([]Provider, 0, len(supportedProviders))
	for provider := range supportedProviders {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func ParseModelString(model string, sdkProvider Provider) (Provider, string) {
	model = strings.TrimSpace(model)
	if provider, name, found := strings.Cut(model, "/"); found && provider != "" && name != "" {
		if IsKnownProvider(provider) {
			return Provider(provider), name
		}
	}
	return sdkProvider, model
}
