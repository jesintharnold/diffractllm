package openaiprovider

import (
	"context"
	"testing"

	"diffractllm/internal/core"

	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toIR(t *testing.T, body string) *core.DiffractLLMChatCompletionRequest {
	t.Helper()
	var r OpenAIChatCompletionRequest
	require.NoError(t, sonic.Unmarshal([]byte(body), &r))
	return r.ToDMChatCompletionRequest(core.NewDiffractLLMContextPool().Acquire(context.Background(), nil, nil))
}

func TestMaxTokensReachesTheUpstream(t *testing.T) {
	ir := toIR(t, `{"model":"gpt-4o","messages":[],"max_tokens":10}`)
	require.NotNil(t, ir.Parameters.MaxCompletionTokens)
	assert.Equal(t, 10, *ir.Parameters.MaxCompletionTokens)

	body, derr := BuildChatCompletionPayload(ir, "gpt-4o", false)
	require.Nil(t, derr)
	var sent map[string]any
	require.NoError(t, sonic.Unmarshal(body, &sent))
	assert.EqualValues(t, 10, sent["max_completion_tokens"])
	assert.NotContains(t, sent, "max_tokens", "OpenAI's reasoning models reject max_tokens")
}

func TestMaxCompletionTokensWinsOverMaxTokens(t *testing.T) {
	ir := toIR(t, `{"model":"gpt-4o","messages":[],"max_tokens":10,"max_completion_tokens":20}`)
	assert.Equal(t, 20, *ir.Parameters.MaxCompletionTokens)
}
