package openaiprovider

import "diffractllm/internal/core"

func (r *OpenAIChatCompletionRequest) ToDMChatCompletionRequest(rctx *core.DiffractLLMContext) *core.DiffractLLMChatCompletionRequest {
	params := r.DiffractLLMChatParameters
	return &core.DiffractLLMChatCompletionRequest{
		Model:      r.Model,
		Messages:   r.Messages,
		Parameters: &params,
		Raw:        rctx.BodyBytes,
	}
}

func ToOpenAIChatCompletionRequest(req *core.DiffractLLMChatCompletionRequest) *OpenAIChatCompletionRequest {
	out := &OpenAIChatCompletionRequest{Model: req.Model, Messages: req.Messages}
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
type OpenAIChatCompletionStreamResponse struct {
	core.DiffractLLMChatCompletionStreamResponse
	Usage *OpenAIChatCompletionUsage `json:"usage,omitempty"`
}

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
