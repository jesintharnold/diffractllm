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

func (u *OpenAIChatCompletionUsage) ToDMChatCompletionUsage() *core.Usage {
	if u == nil {
		return nil
	}
	out := &core.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
	if d := u.PromptTokensDetails; d != nil {
		out.CachedInputTokens = d.CachedTokens
		out.InputAudioTokens = d.AudioTokens
		out.InputImageTokens = d.ImageTokens
		out.CacheCreationTokens = d.CacheWriteTokens
	}
	if d := u.CompletionTokensDetails; d != nil {
		out.ReasoningTokens = d.ReasoningTokens
		out.OutputAudioTokens = d.AudioTokens
		out.AcceptedPredictionTokens = d.AcceptedPredictionTokens
		out.RejectedPredictionTokens = d.RejectedPredictionTokens
	}
	out.TotalTokens = u.TotalTokens
	return out
}

func ToOpenAIChatCompletionUsage(u *core.Usage) *OpenAIChatCompletionUsage {
	if u == nil {
		return nil
	}
	out := &OpenAIChatCompletionUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = u.InputTokens + u.OutputTokens
	}

	if u.CachedInputTokens > 0 || u.CacheCreationTokens > 0 ||
		u.InputAudioTokens > 0 || u.InputImageTokens > 0 {
		out.PromptTokensDetails = &openAIChatPromptTokensDetails{
			CachedTokens:     u.CachedInputTokens,
			CacheWriteTokens: u.CacheCreationTokens,
			AudioTokens:      u.InputAudioTokens,
			ImageTokens:      u.InputImageTokens,
		}
	}
	if u.ReasoningTokens > 0 || u.OutputAudioTokens > 0 ||
		u.AcceptedPredictionTokens > 0 || u.RejectedPredictionTokens > 0 {
		out.CompletionTokensDetails = &openAIChatCompletionTokensDetails{
			ReasoningTokens:          u.ReasoningTokens,
			AudioTokens:              u.OutputAudioTokens,
			AcceptedPredictionTokens: u.AcceptedPredictionTokens,
			RejectedPredictionTokens: u.RejectedPredictionTokens,
		}
	}
	return out
}

type OpenAIChatCompletionResponse struct {
	core.DiffractLLMChatCompletionResponse
	Usage *OpenAIChatCompletionUsage `json:"usage,omitempty"`
}
