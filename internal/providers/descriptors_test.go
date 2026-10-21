package providers

import (
	"encoding/json"
	"testing"

	"diffractllm/internal/core"

	"github.com/stretchr/testify/assert"
)

func TestOpenAIDescriptorPassesRawThrough(t *testing.T) {
	desc := OpenAIDescriptors[0]
	raw := json.RawMessage(`{"upstream":"bytes"}`)

	res := &core.DiffractLLMChatCompletionResponse{}
	res.Raw = raw
	assert.Equal(t, raw, desc.FromDiffract(res))

	chunk := &core.DiffractLLMChatCompletionStreamResponse{}
	chunk.Raw = raw
	_, payload := desc.FromDiffractStream(chunk)
	assert.Equal(t, raw, payload)
}

func TestOpenAIDescriptorConvertsWithoutRaw(t *testing.T) {
	desc := OpenAIDescriptors[0]

	_, isRaw := desc.FromDiffract(&core.DiffractLLMChatCompletionResponse{}).(json.RawMessage)
	assert.False(t, isRaw)

	_, payload := desc.FromDiffractStream(&core.DiffractLLMChatCompletionStreamResponse{})
	_, isRaw = payload.(json.RawMessage)
	assert.False(t, isRaw)
}
