package openaiprovider

import "diffractllm/internal/core"

type OpenAIProvider struct {
}

func (op *OpenAIProvider) ProviderName() core.Provider {
	return core.ProviderOpenAI
}

func (op *OpenAIProvider) ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {
	return nil, nil
}

func (op *OpenAIProvider) ChatCompletionStream(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest, cred *core.Credential) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError) {
	return nil, nil
}

func (op *OpenAIProvider) providerHeaders(cred *core.Credential, stream bool) map[string]string {
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + cred.APIKey,
	}
	if stream {
		headers["Accept"] = "text/event-stream"
		headers["Cache-Control"] = "no-cache"
	}
	return headers
}
