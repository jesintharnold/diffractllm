package openaiprovider

import (
	"diffractllm/internal/core"

	"github.com/bytedance/sonic"
)

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

func (r *OpenAIChatCompletionRequest) ToDMChatCompletionRequest(rctx *core.DiffractLLMContext) *core.DiffractLLMChatCompletionRequest {
	params := r.DiffractLLMChatParameters
	return &core.DiffractLLMChatCompletionRequest{
		Model:      r.Model,
		Messages:   r.Messages,
		Parameters: &params,
		Raw:        rctx.BodyBytes,
	}
}

func ToOpenAIChatCompletionRequest(req *core.DiffractLLMChatCompletionRequest, model string) *OpenAIChatCompletionRequest {
	if model == "" {
		model = req.Model
	}
	out := &OpenAIChatCompletionRequest{Model: model, Messages: req.Messages}
	if req.Parameters != nil {
		out.DiffractLLMChatParameters = *req.Parameters
	}
	return out
}

// Non-stream

func (r *OpenAIChatCompletionResponse) ToDMChatCompletionResponse() *core.DiffractLLMChatCompletionResponse {
	out := r.DiffractLLMChatCompletionResponse
	out.Usage = r.Usage.ToDMChatCompletionUsage()
	return &out
}

func ToOpenAIChatCompletionResponse(res *core.DiffractLLMChatCompletionResponse) *OpenAIChatCompletionResponse {
	if res == nil {
		return nil
	}
	out := OpenAIChatCompletionResponse{DiffractLLMChatCompletionResponse: *res}
	out.Usage = ToOpenAIChatCompletionUsage(res.Usage)
	return &out
}

// Streaming response chunk

func (c *OpenAIChatCompletionStreamResponse) ToDMChatCompletionStreamResponse() *core.DiffractLLMChatCompletionStreamResponse {
	out := c.DiffractLLMChatCompletionStreamResponse
	out.Usage = c.Usage.ToDMChatCompletionUsage()
	out.Type = c.streamEventType()
	return &out
}

const objectChatCompletionChunk = "chat.completion.chunk"

func (c *OpenAIChatCompletionStreamResponse) streamEventType() core.ChatStreamEventType {
	switch {
	case c.Object != objectChatCompletionChunk:
		return core.StreamEventError
	case len(c.Choices) == 0 && c.Usage != nil:
		return core.StreamEventComplete
	default:
		return core.StreamEventDelta
	}
}

func ToOpenAIChatCompletionStreamResponse(c *core.DiffractLLMChatCompletionStreamResponse) *OpenAIChatCompletionStreamResponse {
	if c == nil {
		return nil
	}
	out := &OpenAIChatCompletionStreamResponse{DiffractLLMChatCompletionStreamResponse: *c}
	out.Usage = ToOpenAIChatCompletionUsage(c.Usage)
	return out
}

func BuildChatCompletionPayload(req *core.DiffractLLMChatCompletionRequest, model string, stream bool) ([]byte, *core.DiffractLLMError) {
	if req == nil {
		return nil, core.NewInvalidRequestBody("chat request is required", nil)
	}

	wire := ToOpenAIChatCompletionRequest(req, model)
	if stream {
		wire.Stream = &stream
		if wire.StreamOptions == nil {
			wire.StreamOptions = &core.ChatStreamOptions{}
		}
		t := true
		wire.StreamOptions.IncludeUsage = &t
	}

	body, err := sonic.Marshal(wire)
	if err != nil {
		return nil, core.NewInternalError("openai", "marshalling chat request", err)
	}
	return body, nil
}
