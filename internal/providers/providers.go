package providers

import (
	"diffractllm/internal/core"
	"fmt"
	"sort"
)

type Provider interface {
	ProviderName() core.Provider
	ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError)
	ChatCompletionStream(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError)
}

type ProviderInstance struct {
	providers map[core.Provider]Provider
}

func NewUnsupportedOperation(kind core.RequestKind, provider core.Provider) *core.DiffractLLMError {
	return core.NewInternalError("provider", fmt.Sprintf("%s does not support %s", provider, kind), nil)
}

func NewProviderInstance() *ProviderInstance {
	p := ProviderInstance{
		providers: make(map[core.Provider]Provider),
	}
	return &p
}

func (pi *ProviderInstance) Register(p Provider) {
	pi.providers[p.ProviderName()] = p
}

func (pi *ProviderInstance) Has(provider core.Provider) bool {
	_, ok := pi.providers[provider]
	return ok
}

func (pi *ProviderInstance) Providers() []core.Provider {
	out := make([]core.Provider, 0, len(pi.providers))
	for provider := range pi.providers {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (pi *ProviderInstance) Len() int { return len(pi.providers) }

func (pi *ProviderInstance) Get(provider core.Provider) (Provider, *core.DiffractLLMError) {
	p, ok := pi.providers[provider]
	if !ok {
		return nil, core.NewInternalError("provider", fmt.Sprintf("provider %s is not registered", string(provider)), nil)
	}
	return p, nil
}
