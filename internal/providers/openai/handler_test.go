package openaiprovider

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

const doneStream = "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

func TestStreamReusesTheConnectionAfterDone(t *testing.T) {
	var conns atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, doneStream)
	}))
	server.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	tr := testTransport(t)
	for range 10 {
		ch, derr := HandleChatCompletionStream(testRctx(core.ProviderOpenAI), tr, chatConfig(server.URL))
		require.Nil(t, derr)
		for range ch {
		}
		// The drain runs after the channel closes; give it a moment to return the conn.
		time.Sleep(20 * time.Millisecond)
	}
	assert.EqualValues(t, 1, conns.Load(), "every stream after the first reuses the connection")
}

// An upstream that sends [DONE] and keeps the connection open must not hold the client.
func TestStreamEndsAtDoneEvenIfUpstreamHoldsTheConnection(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, doneStream)
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)

	ch, derr := HandleChatCompletionStream(testRctx(core.ProviderOpenAI), testTransport(t), chatConfig(server.URL))
	require.Nil(t, derr)

	start := time.Now()
	var chunks int
	for range ch {
		chunks++
	}
	assert.Equal(t, 1, chunks)
	assert.Less(t, time.Since(start), 500*time.Millisecond, "the client is not kept waiting on the drain")
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

// The client hangs up while the non-stream body is arriving: a cancel, not a 502 from the provider.
func TestNonStreamClientCancelDuringBodyIsACancel(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"c1",`)
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	rc := core.NewDiffractLLMContextPool().Acquire(ctx, nil, nil)
	rc.Modelkey = core.CatalogKey{Provider: core.ProviderOpenAI, ModelName: "gpt-4o"}
	rc.SDKProvider = core.ProviderOpenAI
	time.AfterFunc(50*time.Millisecond, cancel)

	_, derr := HandleChatCompletion(rc, testTransport(t), chatConfig(server.URL))
	require.NotNil(t, derr)
	assert.Equal(t, core.CodeClientCancelled, derr.Code)
	assert.Equal(t, core.StatusClientClosedRequest, derr.StatusCode)
}
