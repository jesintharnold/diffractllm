package providers

import (
	"diffractllm/internal/core"
	openaiprovider "diffractllm/internal/providers/openai"
	"net/http"
)

type RouteDescriptor struct {
	SDK                core.Provider
	Method             string
	Path               string
	RequestKind        core.RequestKind
	NewRequest         func() any
	ToDiffract         func(req any, rctx *core.DiffractLLMContext) any
	FromDiffract       func(res any) any
	FromDiffractStream func(chunk any) (event string, payload any)
	FromDiffractError  func(err *core.DiffractLLMError) (event string, payload any)
	AddToRoute         bool
}

var OpenAIDescriptors = []RouteDescriptor{
	{
		SDK:         core.ProviderOpenAI,
		Method:      http.MethodPost,
		Path:        "/v1/chat/completions",
		RequestKind: core.ChatRequest,
		NewRequest:  func() any { return &openaiprovider.OpenAIChatCompletionRequest{} },
		ToDiffract: func(req any, rctx *core.DiffractLLMContext) any {
			return req.(*openaiprovider.OpenAIChatCompletionRequest).ToDMChatCompletionRequest(rctx)
		},
		FromDiffract: func(res any) any {
			diffres := res.(*core.DiffractLLMChatCompletionResponse)
			return openaiprovider.ToOpenAIChatCompletionResponse(diffres)
		},
		FromDiffractStream: func(chunk any) (event string, payload any) {
			diffresChunk := chunk.(*core.DiffractLLMChatCompletionStreamResponse)
			return "", openaiprovider.ToOpenAIChatCompletionStreamResponse(diffresChunk)
		},
		FromDiffractError: func(err *core.DiffractLLMError) (event string, payload any) {
			return "", openaiprovider.ToOpenAIError(err)
		},
		AddToRoute: true,
	},
}
