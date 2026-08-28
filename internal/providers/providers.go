package providers

import (
	"diffractllm/internal/core"
	"fmt"
)

type Provider interface {
	ProviderName() core.Provider
	ProviderHeaders(stream bool) map[string]string
	AuthInjection(cred *core.Credential, headers map[string]string) error
	ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError)
	ChatCompletionStream(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError)
}

type ProviderInstance struct {
	ProviderMap map[core.Provider]Provider
}

type ProviderRequest interface {
	IsStreaming() bool
}

func NewUnsupportedOperation(kind core.RequestKind, provider core.Provider) *core.DiffractLLMError {
	return core.NewInternalError("provider", fmt.Sprintf("%s does not support %s", provider, kind), nil)
}

func NewProviderInstance() *ProviderInstance {
	p := ProviderInstance{
		ProviderMap: make(map[core.Provider]Provider),
	}
	return &p
}

func (pi *ProviderInstance) Register(p Provider) {
	pi.ProviderMap[p.ProviderName()] = p
}

func (pi *ProviderInstance) Get(provider core.Provider) (Provider, error) {
	p, ok := pi.ProviderMap[provider]
	if !ok {
		return nil, core.NewInternalError("provider", fmt.Sprintf("provider %s is not registered", string(provider)), nil)
	}
	return p, nil
}
