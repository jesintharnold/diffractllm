package azureprovider

import (
	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"
	"diffractllm/internal/providers"
	openaiprovider "diffractllm/internal/providers/openai"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

var _ providers.Provider = (*AzureProvider)(nil)

type AzureProvider struct {
	Transport       *dataplane.DiffractLLMTransport
	CredentialCache sync.Map
	Logger          *zap.Logger
}

func New(transport *dataplane.DiffractLLMTransport, logger *zap.Logger) *AzureProvider {
	return &AzureProvider{Transport: transport, Logger: logger}
}

func (op *AzureProvider) ProviderName() core.Provider {
	return core.ProviderAzure
}

func (ap *AzureProvider) ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {
	alias, derr := resolveAlias(req, cred)
	if derr != nil {
		return nil, derr
	}

	switch protocol := alias.Protocol(req.Model); protocol {
	case core.ProtocolOpenAI:
		cfg, derr := ap.openaichatConfig(req, cred, alias, false)
		if derr != nil {
			return nil, derr
		}
		return openaiprovider.HandleChatCompletion(rctx, ap.Transport, cfg)

	default:
		return nil, unsupportedProtocol(protocol)
	}
}

func (ap *AzureProvider) ChatCompletionStream(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError) {
	alias, derr := resolveAlias(req, cred)
	if derr != nil {
		return nil, derr
	}

	switch protocol := alias.Protocol(req.Model); protocol {
	case core.ProtocolOpenAI:
		cfg, derr := ap.openaichatConfig(req, cred, alias, true)
		if derr != nil {
			return nil, derr
		}
		return openaiprovider.HandleChatCompletionStream(rctx, ap.Transport, cfg)

	default:
		return nil, unsupportedProtocol(protocol)
	}
}

func resolveAlias(req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (*core.Alias, *core.DiffractLLMError) {
	if req == nil {
		return nil, core.NewInvalidRequestBody("chat request is required", nil)
	}
	if cred == nil || cred.Settings.Azure == nil {
		return nil, core.NewInternalError("azure-provider", "azure credential settings are required", nil)
	}
	return cred.CheckModelAlias(req.Model), nil
}

func unsupportedProtocol(protocol core.EndpointProtocol) *core.DiffractLLMError {
	return core.NewInternalError("azure-provider", fmt.Sprintf("protocol %q is not supported yet", protocol), nil)
}
