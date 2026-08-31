package openaiprovider

import (
	"errors"
	"fmt"
	"strings"

	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"

	"go.uber.org/zap"
)

var openAIPaths = map[core.RequestKind]string{
	core.ChatRequest: "/v1/chat/completions",
}

func PathFor(kind core.RequestKind) (string, bool) {
	p, ok := openAIPaths[kind]
	return p, ok
}

func ProviderHeaders(stream bool) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if stream {
		headers["Accept"] = "text/event-stream"
		headers["Cache-Control"] = "no-cache"
	}
	return headers
}

type OpenAIProvider struct {
	Transport *dataplane.DiffractLLMTransport
	Logger    *zap.Logger
}

func New(transport *dataplane.DiffractLLMTransport, logger *zap.Logger) *OpenAIProvider {
	return &OpenAIProvider{Transport: transport, Logger: logger}
}

func (op *OpenAIProvider) ProviderName() core.Provider {
	return core.ProviderOpenAI
}

func (op *OpenAIProvider) ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {
	cfg, derr := op.chatConfig(req, cred, false)
	if derr != nil {
		return nil, derr
	}

	return HandleChatCompletion(rctx, op.Transport, cfg)
}

func (op *OpenAIProvider) ChatCompletionStream(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError) {
	cfg, derr := op.chatConfig(req, cred, true)
	if derr != nil {
		return nil, derr
	}
	return HandleChatCompletionStream(rctx, op.Transport, cfg)
}

func (op *OpenAIProvider) chatConfig(req *core.DiffractLLMChatCompletionRequest, cred *core.Credential, stream bool) (*ChatCompletionConfig, *core.DiffractLLMError) {
	if req == nil {
		return nil, core.NewInvalidRequestBody("chat request is required", nil)
	}
	if cred == nil {
		return nil, core.NewInternalError("openai", "credential is required", nil)
	}

	model := cred.CheckModelAlias(req.Model)

	url, err := op.endpoint(cred, core.ChatRequest)
	if err != nil {
		return nil, core.NewInternalError("openai", "building url", err)
	}

	headers := ProviderHeaders(stream)
	if err := op.AuthInjection(cred, headers); err != nil {
		return nil, core.NewUpstreamAuth("openai", core.SanitizeBackendURL(url), err.Error())
	}

	return &ChatCompletionConfig{
		Provider: core.ProviderOpenAI,
		URL:      url,
		Model:    model.ModelID,
		Request:  req,
		Headers:  headers,
	}, nil
}

func (op *OpenAIProvider) endpoint(cred *core.Credential, kind core.RequestKind) (string, error) {
	if cred.Endpoint == "" {
		return "", errors.New("endpoint is required")
	}
	path, ok := PathFor(kind)
	if !ok {
		return "", fmt.Errorf("unsupported request kind %s", kind)
	}
	return strings.TrimRight(cred.Endpoint, "/") + path, nil
}

func (op *OpenAIProvider) AuthInjection(cred *core.Credential, headers map[string]string) error {
	if cred.APIKey == "" {
		return errors.New("missing api key")
	}
	headers["Authorization"] = "Bearer " + cred.APIKey
	return nil
}
