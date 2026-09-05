package providers

import (
	"diffractllm/internal/core"
	"fmt"
)

type Provider interface {
	ProviderName() core.Provider
	ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError)
	ChatCompletionStream(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError)
}

// ProviderInstance is written once at boot and read-only afterwards, so the
// map needs no lock - but only if nothing outside can reach it.
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

func (pi *ProviderInstance) Get(provider core.Provider) (Provider, *core.DiffractLLMError) {
	p, ok := pi.providers[provider]
	if !ok {
		return nil, core.NewInternalError("provider", fmt.Sprintf("provider %s is not registered", string(provider)), nil)
	}
	return p, nil
}
