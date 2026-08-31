package openaiprovider

import "diffractllm/internal/core"

type OpenAIChatCompletionRequest struct {
	Model     string                        `json:"model"`
	Messages  []core.DiffractLLMChatMessage `json:"messages"`
	MaxTokens *int                          `json:"max_tokens,omitempty"`
	core.DiffractLLMChatParameters
}

func (r *OpenAIChatCompletionRequest) IsStreaming() bool {
	return r.Stream != nil && *r.Stream
}

type openAIChatPromptTokensDetails struct {
	CachedTokens     int64 `json:"cached_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	AudioTokens      int64 `json:"audio_tokens,omitempty"`
	ImageTokens      int64 `json:"image_tokens,omitempty"`
	TextTokens       int64 `json:"text_tokens,omitempty"`
}

type openAIChatCompletionTokensDetails struct {
	ReasoningTokens          int64 `json:"reasoning_tokens,omitempty"`
	AudioTokens              int64 `json:"audio_tokens,omitempty"`
	TextTokens               int64 `json:"text_tokens,omitempty"`
	AcceptedPredictionTokens int64 `json:"accepted_prediction_tokens,omitempty"`
	RejectedPredictionTokens int64 `json:"rejected_prediction_tokens,omitempty"`
}

type OpenAIChatCompletionUsage struct {
	PromptTokens            int64                              `json:"prompt_tokens"`
	CompletionTokens        int64                              `json:"completion_tokens"`
	TotalTokens             int64                              `json:"total_tokens"`
	PromptTokensDetails     *openAIChatPromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openAIChatCompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type OpenAIChatCompletionResponse struct {
	core.DiffractLLMChatCompletionResponse
	Usage *OpenAIChatCompletionUsage `json:"usage,omitempty"`
}

type OpenAIChatCompletionStreamResponse struct {
	core.DiffractLLMChatCompletionStreamResponse
	Usage *OpenAIChatCompletionUsage `json:"usage,omitempty"`
}


