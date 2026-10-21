package openaiprovider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const upstreamBody = `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4},"not_in_the_ir":true}`

func testTransport(t *testing.T) *dataplane.DiffractLLMTransport {
	t.Helper()
	return dataplane.NewTransport(config.UpstreamConfig{
		MaxIdleConns: 10, MaxConnsPerHost: 10, MaxIdleConnsPerHost: 10,
		IdleConnTimeout: time.Second, DialTimeout: time.Second, KeepAlive: time.Second,
		TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: time.Second,
		RequestTimeout: 2 * time.Second, StreamIdleTimeout: time.Second,
		MaxResponseBytesKB: 64, WriteBufferSize: 4096, ReadBufferSize: 4096,
	}, map[core.Provider]*core.Upstream{
		core.ProviderOpenAI: {Provider: core.ProviderOpenAI, Network: core.NetworkConfig{AllowPrivateNetwork: true}},
	})
}

func testRctx(sdk core.Provider) *core.DiffractLLMContext {
	rc := core.NewDiffractLLMContextPool().Acquire(context.Background(), nil, nil)
	rc.Modelkey = core.CatalogKey{Provider: core.ProviderOpenAI, ModelName: "gpt-4o"}
	rc.SDKProvider = sdk
	return rc
}

func chatConfig(url string) *ChatCompletionConfig {
	return &ChatCompletionConfig{
		Provider: core.ProviderOpenAI, URL: url, Model: "gpt-4o",
		Request: &core.DiffractLLMChatCompletionRequest{Model: "gpt-4o"},
	}
}

func TestNonStreamKeepsUpstreamBytesForAnOpenAIClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, upstreamBody)
	}))
	defer server.Close()

	res, derr := HandleChatCompletion(testRctx(core.ProviderOpenAI), testTransport(t), chatConfig(server.URL))
	require.Nil(t, derr)
	assert.Equal(t, upstreamBody, string(res.Raw), "an OpenAI client gets the upstream bytes")
	require.NotNil(t, res.Usage, "the IR is still parsed for accounting")
	assert.EqualValues(t, 3, res.Usage.InputTokens)
}

func TestNonStreamDropsUpstreamBytesForAnotherClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, upstreamBody)
	}))
	defer server.Close()

	res, derr := HandleChatCompletion(testRctx("anthropic"), testTransport(t), chatConfig(server.URL))
	require.Nil(t, derr)
	assert.Empty(t, res.Raw, "another SDK's client must be answered from the IR")
}

func TestStreamChunksKeepUpstreamBytes(t *testing.T) {
	const chunk = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hi"}}],"not_in_the_ir":true}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "data: "+chunk+"\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	ch, derr := HandleChatCompletionStream(testRctx(core.ProviderOpenAI), testTransport(t), chatConfig(server.URL))
	require.Nil(t, derr)
	var got []string
	for c := range ch {
		got = append(got, string(c.Raw))
	}
	assert.Equal(t, []string{chunk}, got)
}
