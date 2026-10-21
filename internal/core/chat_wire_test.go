package core

import (
	"testing"

	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSinglePlainTextContentMarshalsAsAString(t *testing.T) {
	text := "hello"
	b, err := sonic.Marshal(DiffractLLMChatMessage{Role: "assistant", Content: ChatContent{{Type: ContentText, Text: &text}}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"role":"assistant","content":"hello"}`, string(b))
}

func TestContentThatIsNotPlainTextStaysAnArray(t *testing.T) {
	a, b := "a", "b"
	two, err := sonic.Marshal(ChatContent{{Type: ContentText, Text: &a}, {Type: ContentText, Text: &b}})
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, string(two))

	cached, err := sonic.Marshal(ChatContent{{Type: ContentText, Text: &a, PromptCacheBreakpoint: &ChatPromptCacheBreakpoint{}}})
	require.NoError(t, err)
	assert.Equal(t, byte('['), cached[0], "a cache breakpoint must survive, so it stays an array")
}

func TestContentRoundTripsBothForms(t *testing.T) {
	for _, in := range []string{`"hi"`, `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`} {
		var c ChatContent
		require.NoError(t, sonic.Unmarshal([]byte(in), &c))
		out, err := sonic.Marshal(c)
		require.NoError(t, err)
		assert.JSONEq(t, in, string(out))
	}
}

func TestStopAcceptsAStringOrAnArray(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ChatStop
	}{
		{`{"stop":"\n"}`, ChatStop{"\n"}},
		{`{"stop":["a","b"]}`, ChatStop{"a", "b"}},
		{`{"stop":null}`, nil},
		{`{}`, nil},
	} {
		var p DiffractLLMChatParameters
		require.NoError(t, sonic.Unmarshal([]byte(tc.in), &p), tc.in)
		assert.Equal(t, tc.want, p.Stop, tc.in)
	}
	b, err := sonic.Marshal(DiffractLLMChatParameters{Stop: ChatStop{"\n"}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"stop":["\n"]}`, string(b), "always sent upstream as an array")
}
