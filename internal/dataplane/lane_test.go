package dataplane

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"diffractllm/internal/core"
	"diffractllm/internal/observability/logging"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNilLaneLetsEverythingThrough(t *testing.T) {
	var ln *lane
	require.NoError(t, ln.acquire())
	ln.release()
	assert.Zero(t, ln.concurrency())
	assert.Zero(t, ln.active())
	assert.Nil(t, newLane(0), "max_concurrency 0 means off")
}

func TestLaneRefusesAtOnceWhenFull(t *testing.T) {
	ln := newLane(2)
	require.NoError(t, ln.acquire())
	require.NoError(t, ln.acquire())
	assert.Equal(t, 2, ln.active())
	assert.ErrorIs(t, ln.acquire(), errLaneFull)

	ln.release()
	assert.NoError(t, ln.acquire(), "a freed place can be taken again")
}

func TestLaneNeverExceedsItsLimitUnderContention(t *testing.T) {
	const limit = 8
	ln := newLane(limit)
	var peak, current atomic.Int64
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				if ln.acquire() != nil {
					continue
				}
				if n := current.Add(1); n > peak.Load() {
					peak.Store(n)
				}
				current.Add(-1)
				ln.release()
			}
		}()
	}
	wg.Wait()
	assert.LessOrEqual(t, peak.Load(), int64(limit))
	assert.Zero(t, ln.active(), "every acquire was released")
}

func TestLaneLimitIsCappedAtMaxConnsPerHost(t *testing.T) {
	cfg := testUpstreamConfig()
	cfg.MaxConnsPerHost, cfg.MaxConcurrency = 2, 4
	assert.Equal(t, 2, laneLimit(cfg, core.NetworkConfig{}, core.ProviderOpenAI, logging.For("test")))
}

func TestFullLaneReturnsProviderOverloadedAndCloseFreesIt(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)

	cfg := testUpstreamConfig()
	cfg.MaxConcurrency = 1
	tr := NewTransport(cfg, map[core.Provider]*core.Upstream{core.ProviderOpenAI: upstreamFor(core.NetworkConfig{})})
	req := &DiffractLLMTransportRequest{Method: http.MethodPost, URL: server.URL, IsStreaming: true}

	first, derr := tr.ServeHTTP(transportRctx(t), req)
	require.Nil(t, derr)

	_, derr = tr.ServeHTTP(transportRctx(t), req)
	require.NotNil(t, derr, "the lane is full while the first stream is open")
	assert.Equal(t, core.CodeProviderOverloaded, derr.Code)
	assert.Equal(t, http.StatusServiceUnavailable, derr.StatusCode)
	assert.Equal(t, 1, derr.RetryAfter)

	first.Body.Close()
	second, derr := tr.ServeHTTP(transportRctx(t), req)
	require.Nil(t, derr, "closing the stream frees its place")
	second.Body.Close()
}

func TestClosingABodyTwiceReleasesOnce(t *testing.T) {
	ln := newLane(2)
	require.NoError(t, ln.acquire())
	require.NoError(t, ln.acquire())
	b := newLimitedBody(io.NopCloser(strings.NewReader("x")), 64, nil, ln)
	require.NoError(t, b.Close())
	require.NoError(t, b.Close())
	assert.Equal(t, 1, ln.active(), "the second Close must not release again")
}

func TestReplaceKeepsUnchangedProviders(t *testing.T) {
	cfg := testUpstreamConfig()
	cfg.MaxConcurrency = 4
	openai := upstreamFor(core.NetworkConfig{})
	azure := &core.Upstream{Provider: core.ProviderAzure, Network: core.NetworkConfig{AllowPrivateNetwork: true}}
	tr := NewTransport(cfg, map[core.Provider]*core.Upstream{core.ProviderOpenAI: openai, core.ProviderAzure: azure})
	before := *tr.providers.Load()

	retries := 2
	changed := *azure
	changed.Network.MaxRetries = &retries
	require.Empty(t, tr.Replace(map[core.Provider]*core.Upstream{
		core.ProviderOpenAI: upstreamFor(core.NetworkConfig{}),
		core.ProviderAzure:  &changed,
	}))
	after := *tr.providers.Load()

	assert.Same(t, before[core.ProviderOpenAI], after[core.ProviderOpenAI], "OpenAI keeps its client, idle pool and lane")
	assert.NotSame(t, before[core.ProviderAzure], after[core.ProviderAzure], "Azure is rebuilt")
	assert.Same(t, before[core.ProviderAzure].TrafficControl, after[core.ProviderAzure].TrafficControl, "and keeps its lane")
}

// Lowering the limit while calls run keeps them counted: no overshoot after a config edit.
func TestReplaceChangesTheLimitWithoutResettingTheCount(t *testing.T) {
	cfg := testUpstreamConfig()
	cfg.MaxConcurrency = 4
	tr := NewTransport(cfg, map[core.Provider]*core.Upstream{core.ProviderOpenAI: upstreamFor(core.NetworkConfig{})})
	ln := (*tr.providers.Load())[core.ProviderOpenAI].TrafficControl
	for range 3 {
		require.NoError(t, ln.acquire())
	}

	two := 2
	require.Empty(t, tr.Replace(map[core.Provider]*core.Upstream{
		core.ProviderOpenAI: upstreamFor(core.NetworkConfig{MaxConcurrency: &two}),
	}))
	now := (*tr.providers.Load())[core.ProviderOpenAI].TrafficControl

	assert.Same(t, ln, now)
	assert.Equal(t, 2, now.concurrency())
	assert.Equal(t, 3, now.active(), "the three running calls are still counted")
	assert.ErrorIs(t, now.acquire(), errLaneFull)
	now.release()
	now.release()
	assert.NoError(t, now.acquire(), "below the new limit again")
}
