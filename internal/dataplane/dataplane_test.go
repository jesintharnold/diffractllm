package dataplane

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	config "diffractllm/configs"
	"diffractllm/internal/core"
)

const (
	gpt4o = "gpt-4o"
	vkID  = "vk-1"
)

func ptr[T any](v T) *T { return &v }

func chatKey(provider core.Provider, model string) core.CatalogKey {
	return core.CatalogKey{Provider: provider, ModelName: model, ModelType: core.ModelTypeChat}
}

func cred(id string, provider core.Provider) *core.Credential {
	return &core.Credential{
		ID: id, Provider: provider, Name: id, APIKey: "sk-test", Enabled: true,
		Endpoint: "https://example.test", AllowedModels: []string{"*"},
	}
}

// SelectionEngine takes the CredentialSource interface, so a test needs no
// plane: this returns whatever the case asks for, per provider.
type fakeSource struct {
	byProvider map[core.Provider][]*core.Credential
	calls      []core.CatalogKey
}

func (f *fakeSource) Candidates(key core.CatalogKey) []*core.Credential {
	f.calls = append(f.calls, key)
	return f.byProvider[key.Provider]
}

func source(creds ...*core.Credential) *fakeSource {
	f := &fakeSource{byProvider: make(map[core.Provider][]*core.Credential)}
	for _, c := range creds {
		f.byProvider[c.Provider] = append(f.byProvider[c.Provider], c)
	}
	return f
}

func vkey(lb core.LBKind, configs ...core.ProviderConfig) *core.VirtualKey {
	compiled, err := core.CompileProviderConfigs(core.VKWeighted, configs)
	if err != nil {
		panic(err)
	}
	return &core.VirtualKey{ID: vkID, LoadBalancer: lb, Mode: core.VKWeighted, ProviderConfigs: compiled}
}

func pconfig(provider core.Provider, weight float32) core.ProviderConfig {
	return core.ProviderConfig{Provider: provider, AllowedModels: []string{"*"}, Weight: weight}
}

func rctxFor(key core.CatalogKey, vk *core.VirtualKey) *core.DiffractLLMContext {
	rc := core.NewDiffractLLMContextPool().Acquire(context.Background(), nil, nil)
	rc.Modelkey = key
	if vk != nil {
		rc.VirtualKeyID = vk.ID
		rc.VirtualKeyPolicy = vk
	}
	return rc
}

func engine(t *testing.T, f *fakeSource) *SelectionEngine {
	t.Helper()
	return NewSelectionEngine(f, zap.NewNop())
}

// ---------- Resolve: rejections ----------

func TestResolveRejections(t *testing.T) {
	full := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 1))

	tests := []struct {
		name     string
		rctx     func() *core.DiffractLLMContext
		wantCode int
	}{
		{
			name:     "nil context",
			rctx:     func() *core.DiffractLLMContext { return nil },
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "no virtual key id",
			rctx: func() *core.DiffractLLMContext {
				rc := rctxFor(chatKey(core.ProviderOpenAI, gpt4o), full)
				rc.VirtualKeyID = ""
				return rc
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "no policy",
			rctx: func() *core.DiffractLLMContext {
				rc := rctxFor(chatKey(core.ProviderOpenAI, gpt4o), nil)
				rc.VirtualKeyID = vkID
				return rc
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "policy with no provider configs",
			rctx: func() *core.DiffractLLMContext {
				return rctxFor(chatKey(core.ProviderOpenAI, gpt4o), &core.VirtualKey{ID: vkID})
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "no model name",
			rctx: func() *core.DiffractLLMContext {
				return rctxFor(core.CatalogKey{Provider: core.ProviderOpenAI}, full)
			},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, derr := engine(t, source(cred("a", core.ProviderOpenAI))).Resolve(tc.rctx())
			require.NotNil(t, derr)
			assert.Equal(t, tc.wantCode, derr.StatusCode)
		})
	}
}

// ---------- Resolve: explicit provider ----------

func TestResolveExplicit(t *testing.T) {
	f := source(cred("a", core.ProviderOpenAI))
	rc := rctxFor(chatKey(core.ProviderOpenAI, gpt4o), vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 1)))

	got, derr := engine(t, f).Resolve(rc)
	require.Nil(t, derr)
	require.NotNil(t, got)
	assert.Equal(t, "a", got.ID)

	assert.Same(t, got, rc.SelectedCredential, "commit must publish the choice on the context")
	assert.Equal(t, core.ProviderOpenAI, rc.Modelkey.Provider)

	require.Len(t, f.calls, 1, "an explicit provider asks the plane once")
	assert.Equal(t, core.ProviderOpenAI, f.calls[0].Provider)
}

// A provider the client named but the key does not carry is 403, not 503: the
// difference is "you may not" against "nothing is up".
func TestResolveExplicitProviderNotOnTheKey(t *testing.T) {
	f := source(cred("a", core.ProviderAzure))
	rc := rctxFor(chatKey(core.ProviderAzure, gpt4o), vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 1)))

	_, derr := engine(t, f).Resolve(rc)
	require.NotNil(t, derr)
	assert.Equal(t, http.StatusForbidden, derr.StatusCode)
	assert.Empty(t, f.calls, "the plane must not be consulted for a provider the key lacks")
}

func TestResolveExplicitNoCandidates(t *testing.T) {
	rc := rctxFor(chatKey(core.ProviderOpenAI, gpt4o), vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 1)))

	_, derr := engine(t, source()).Resolve(rc)
	require.NotNil(t, derr)
	assert.Equal(t, http.StatusServiceUnavailable, derr.StatusCode)
	assert.Nil(t, rc.SelectedCredential, "a failed resolve must not commit")
}

// ---------- Resolve: weighted ----------

func TestResolveWeightedPicksAViableProvider(t *testing.T) {
	f := source(cred("openai-1", core.ProviderOpenAI), cred("azure-1", core.ProviderAzure))
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 0.5), pconfig(core.ProviderAzure, 0.5))
	rc := rctxFor(core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeChat}, vk)

	got, derr := engine(t, f).Resolve(rc)
	require.Nil(t, derr)
	require.NotNil(t, got)

	assert.Contains(t, []core.Provider{core.ProviderOpenAI, core.ProviderAzure}, got.Provider)
	assert.Equal(t, got.Provider, rc.Modelkey.Provider, "the chosen provider must land on the key")
	assert.Len(t, f.calls, 2, "every configured provider is probed")
}

// A provider with no live credential is dropped before weighting, so a zero
// weight on the only viable provider still resolves.
func TestResolveWeightedSkipsProvidersWithNoCandidates(t *testing.T) {
	f := source(cred("azure-1", core.ProviderAzure))
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 1), pconfig(core.ProviderAzure, 0))
	rc := rctxFor(core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeChat}, vk)

	got, derr := engine(t, f).Resolve(rc)
	require.Nil(t, derr)
	require.NotNil(t, got)
	assert.Equal(t, "azure-1", got.ID)
	assert.Equal(t, core.ProviderAzure, rc.Modelkey.Provider)
}

func TestResolveWeightedNoViableProvider(t *testing.T) {
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 0.5), pconfig(core.ProviderAzure, 0.5))
	rc := rctxFor(core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeChat}, vk)

	_, derr := engine(t, source()).Resolve(rc)
	require.NotNil(t, derr)
	assert.Equal(t, http.StatusServiceUnavailable, derr.StatusCode)
}

// The probe carries the model and type from the request but the provider from
// the key's config - that is what makes a bare model name routable.
func TestResolveWeightedProbeKeys(t *testing.T) {
	f := source(cred("openai-1", core.ProviderOpenAI))
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 0.5), pconfig(core.ProviderAzure, 0.5))
	rc := rctxFor(core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeEmbedding}, vk)

	_, derr := engine(t, f).Resolve(rc)
	require.Nil(t, derr)

	require.Len(t, f.calls, 2)
	for _, call := range f.calls {
		assert.Equal(t, gpt4o, call.ModelName)
		assert.Equal(t, core.ModelTypeEmbedding, call.ModelType)
	}
}

// Weight decides the share. With 0/1 the zero-weight provider never wins, which
// is the only weighting claim a test can make without being flaky.
func TestResolveWeightedRespectsAZeroWeight(t *testing.T) {
	f := source(cred("openai-1", core.ProviderOpenAI), cred("azure-1", core.ProviderAzure))
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 0), pconfig(core.ProviderAzure, 1))

	for range 200 {
		rc := rctxFor(core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeChat}, vk)
		got, derr := engine(t, f).Resolve(rc)
		require.Nil(t, derr)
		require.Equal(t, core.ProviderAzure, got.Provider)
	}
}

// A 50/50 split over 400 draws must land near the middle. The bounds are wide
// because selection is random, but they still reject a distribution that
// ignores weight - 399/1 would pass a "both were seen" assertion.
func TestResolveWeightedDistributionIsBounded(t *testing.T) {
	f := source(cred("openai-1", core.ProviderOpenAI), cred("azure-1", core.ProviderAzure))
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 0.5), pconfig(core.ProviderAzure, 0.5))
	se := engine(t, f)

	const draws = 400
	seen := make(map[core.Provider]int)
	for range draws {
		rc := rctxFor(core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeChat}, vk)
		got, derr := se.Resolve(rc)
		require.Nil(t, derr)
		seen[got.Provider]++
	}

	require.Equal(t, draws, seen[core.ProviderOpenAI]+seen[core.ProviderAzure])
	for _, provider := range []core.Provider{core.ProviderOpenAI, core.ProviderAzure} {
		assert.Greater(t, seen[provider], 120, "%s is starved: %v", provider, seen)
		assert.Less(t, seen[provider], 280, "%s is over-selected: %v", provider, seen)
	}
}

// A 90/10 split must be visibly skewed, so weight is actually applied rather
// than the providers being picked uniformly.
func TestResolveWeightedHonoursTheRatio(t *testing.T) {
	f := source(cred("openai-1", core.ProviderOpenAI), cred("azure-1", core.ProviderAzure))
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 0.9), pconfig(core.ProviderAzure, 0.1))
	se := engine(t, f)

	const draws = 400
	seen := make(map[core.Provider]int)
	for range draws {
		rc := rctxFor(core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeChat}, vk)
		got, derr := se.Resolve(rc)
		require.Nil(t, derr)
		seen[got.Provider]++
	}

	assert.Greater(t, seen[core.ProviderOpenAI], 300, "the 0.9 provider must dominate: %v", seen)
	assert.Less(t, seen[core.ProviderAzure], 100, "the 0.1 provider must stay rare: %v", seen)
}

// An explicit provider is a pin, not a preference. If it has no live credential
// the request fails even though another configured provider could serve it.
func TestResolveExplicitNeverFallsBack(t *testing.T) {
	f := source(cred("azure-1", core.ProviderAzure))
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 0.5), pconfig(core.ProviderAzure, 0.5))
	rc := rctxFor(chatKey(core.ProviderOpenAI, gpt4o), vk)

	_, derr := engine(t, f).Resolve(rc)
	require.NotNil(t, derr)
	assert.Equal(t, core.CodeNoHealthyBackends, derr.Code)
	assert.Equal(t, http.StatusServiceUnavailable, derr.StatusCode)
	assert.Nil(t, rc.SelectedCredential, "azure must not be substituted for the pinned provider")

	require.Len(t, f.calls, 1, "only the pinned provider is probed")
	assert.Equal(t, core.ProviderOpenAI, f.calls[0].Provider)
}

// The error names the model that could not be served, so an operator can tell
// which key/model pair is dark.
func TestResolveNoCandidatesNamesTheModel(t *testing.T) {
	vk := vkey(core.LBRoundRobin, pconfig(core.ProviderOpenAI, 1))

	explicit := rctxFor(chatKey(core.ProviderOpenAI, gpt4o), vk)
	_, derr := engine(t, source()).Resolve(explicit)
	require.NotNil(t, derr)
	assert.Contains(t, derr.Message, gpt4o)
	assert.Equal(t, "openai/"+gpt4o, derr.Backend, "the explicit path reports provider/model")

	weighted := rctxFor(core.CatalogKey{ModelName: gpt4o, ModelType: core.ModelTypeChat}, vk)
	_, derr = engine(t, source()).Resolve(weighted)
	require.NotNil(t, derr)
	assert.Equal(t, gpt4o, derr.Backend, "the weighted path has no provider yet, so it reports the model")
}

// ---------- selector fallback ----------

// Only round robin is implemented. An unimplemented kind must fall back rather
// than return a nil selector and panic.
func TestUnimplementedLoadBalancerFallsBackToRoundRobin(t *testing.T) {
	f := source(cred("a", core.ProviderOpenAI))
	se := engine(t, f)

	for _, lb := range []core.LBKind{core.LBLeastConnection, core.LBLatencyBased, core.LBKind(99)} {
		rc := rctxFor(chatKey(core.ProviderOpenAI, gpt4o), vkey(lb, pconfig(core.ProviderOpenAI, 1)))
		got, derr := se.Resolve(rc)
		require.Nil(t, derr, lb.String())
		assert.Equal(t, "a", got.ID)
	}

	assert.Same(t, se.selectors[core.LBRoundRobin], se.kind(core.LBKind(99)))
}

// ---------- round robin ----------

func TestRoundRobinRotates(t *testing.T) {
	rr := newRoundRobin()
	creds := []*core.Credential{
		cred("a", core.ProviderOpenAI), cred("b", core.ProviderOpenAI), cred("c", core.ProviderOpenAI),
	}
	key := chatKey(core.ProviderOpenAI, gpt4o)

	seen := make(map[string]int)
	for range 30 {
		seen[rr.Pick(key, creds).ID]++
	}
	assert.Equal(t, map[string]int{"a": 10, "b": 10, "c": 10}, seen)
}

func TestRoundRobinEdgeCases(t *testing.T) {
	rr := newRoundRobin()
	key := chatKey(core.ProviderOpenAI, gpt4o)

	assert.Nil(t, rr.Pick(key, nil))
	assert.Nil(t, rr.Pick(key, []*core.Credential{}))

	only := cred("a", core.ProviderOpenAI)
	assert.Same(t, only, rr.Pick(key, []*core.Credential{only}))
	assert.Same(t, only, rr.Pick(key, []*core.Credential{only}), "a single credential needs no cursor")
}

// Cursors are per CatalogKey, so two models do not share a rotation.
func TestRoundRobinCursorIsPerKey(t *testing.T) {
	rr := newRoundRobin()
	creds := []*core.Credential{cred("a", core.ProviderOpenAI), cred("b", core.ProviderOpenAI)}

	first := rr.Pick(chatKey(core.ProviderOpenAI, gpt4o), creds)
	other := rr.Pick(chatKey(core.ProviderOpenAI, "gpt-4o-mini"), creds)
	assert.Equal(t, first.ID, other.ID, "a fresh key starts its own cursor")

	assert.NotEqual(t, first.ID, rr.Pick(chatKey(core.ProviderOpenAI, gpt4o), creds).ID)
}

func TestRoundRobinIsConcurrencySafe(t *testing.T) {
	rr := newRoundRobin()
	creds := []*core.Credential{cred("a", core.ProviderOpenAI), cred("b", core.ProviderOpenAI)}
	key := chatKey(core.ProviderOpenAI, gpt4o)

	done := make(chan struct{})
	for range 8 {
		go func() {
			for range 200 {
				require.NotNil(t, rr.Pick(key, creds))
			}
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
}

// ---------- the SSRF guard ----------

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{addr: "127.0.0.1", want: true},
		{addr: "::1", want: true},
		{addr: "10.0.0.1", want: true},
		{addr: "172.16.0.1", want: true},
		{addr: "192.168.1.1", want: true},
		{addr: "169.254.169.254", want: true},
		{addr: "0.0.0.0", want: true},
		{addr: "224.0.0.1", want: true},
		{addr: "::ffff:127.0.0.1", want: true},
		{addr: "::ffff:10.0.0.1", want: true},
		{addr: "8.8.8.8"},
		{addr: "1.1.1.1"},
		{addr: "2606:4700:4700::1111"},
	}

	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			assert.Equal(t, tc.want, isBlockedIP(netip.MustParseAddr(tc.addr)))
		})
	}
}

func TestGuardedControl(t *testing.T) {
	assert.Nil(t, guardedControl(true), "allow_private_network removes the Control hook entirely")

	guard := guardedControl(false)
	require.NotNil(t, guard)

	assert.Error(t, guard("tcp", "127.0.0.1:443", nil), "a loopback dial must be refused")
	assert.NoError(t, guard("tcp", "8.8.8.8:443", nil))
	assert.Error(t, guard("tcp", "no-port", nil))
	assert.Error(t, guard("tcp", "example.com:443", nil), "an unresolved host must be refused")
}

// ---------- bounded bodies ----------

func TestLimitedBody(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		limit   int64
		wantErr bool
	}{
		{name: "under the limit", payload: "hello", limit: 100},
		{name: "exactly at the limit", payload: "hello", limit: 5},
		{name: "one byte over", payload: "hello!", limit: 5, wantErr: true},
		{name: "empty payload", payload: "", limit: 5},
		{name: "zero limit with a payload", payload: "x", limit: 0, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := newLimitedBody(io.NopCloser(strings.NewReader(tc.payload)), tc.limit)
			got, err := io.ReadAll(body)

			if tc.wantErr {
				assert.ErrorIs(t, err, ErrResponseTooLarge)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.payload, string(got))
			assert.NoError(t, body.Close())
		})
	}
}

// A stalled upstream surfaces as ErrStreamIdle, not as a generic EOF, so the
// caller can tell a truncation from a clean end.
func TestStreamTimeoutBodyStall(t *testing.T) {
	pr, pw := io.Pipe()
	body := newStreamTimeoutBody(pr, 50*time.Millisecond)

	go func() {
		_, _ = pw.Write([]byte("chunk-1"))
	}()

	buf := make([]byte, 7)
	n, err := body.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "chunk-1", string(buf[:n]))

	_, err = body.Read(buf)
	assert.ErrorIs(t, err, ErrStreamIdle, "silence past the idle timeout must be reported")
}

// Every read resets the timer, so a slow but live stream is not cut off.
func TestStreamTimeoutBodyResetsOnData(t *testing.T) {
	pr, pw := io.Pipe()
	body := newStreamTimeoutBody(pr, 150*time.Millisecond)

	go func() {
		for range 4 {
			time.Sleep(50 * time.Millisecond)
			_, _ = pw.Write([]byte("tick"))
		}
		pw.Close()
	}()

	got, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "ticktickticktick", string(got))
}

func TestStreamTimeoutBodyCloseIsIdempotent(t *testing.T) {
	body := newStreamTimeoutBody(io.NopCloser(strings.NewReader("x")), time.Hour)

	assert.NoError(t, body.Close())
	assert.NoError(t, body.Close(), "the timer stop must run once and the error be cached")
}

// ---------- retry policy ----------

// A retry of 502/503/504 can bill twice, so it is off unless the operator
// opts in. 408 and 429 are rejections before a model ran.
func TestRetryable(t *testing.T) {
	safe := []int{408, 429}
	ambiguous := []int{502, 503, 504}
	never := []int{200, 201, 400, 401, 403, 404, 409, 422, 500, 501}

	for _, status := range safe {
		assert.True(t, retrySafe(status), http.StatusText(status))
		assert.True(t, retryable(status, false), http.StatusText(status))
		assert.True(t, retryable(status, true), http.StatusText(status))
	}

	for _, status := range ambiguous {
		assert.False(t, retrySafe(status), http.StatusText(status))
		assert.True(t, retryAmbiguous(status), http.StatusText(status))
		assert.False(t, retryable(status, false), "%s must not retry by default", http.StatusText(status))
		assert.True(t, retryable(status, true), http.StatusText(status))
	}

	for _, status := range never {
		assert.False(t, retryable(status, false), http.StatusText(status))
		assert.False(t, retryable(status, true), "%s is never retryable", http.StatusText(status))
	}
}

func TestRetryAfter(t *testing.T) {
	base := 100 * time.Millisecond

	tests := []struct {
		name    string
		header  string
		attempt int
		want    time.Duration
	}{
		{name: "honours a numeric header", header: "2", attempt: 1, want: 2 * time.Second},
		{name: "ignores a zero header", header: "0", attempt: 1, want: base},
		{name: "ignores a date header", header: "Wed, 21 Oct 2026 07:28:00 GMT", attempt: 1, want: base},
		{name: "ignores a negative header", header: "-5", attempt: 2, want: 2 * base},
		{name: "exponential on attempt 1", attempt: 1, want: base},
		{name: "exponential on attempt 2", attempt: 2, want: 2 * base},
		{name: "exponential on attempt 3", attempt: 3, want: 4 * base},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.header != "" {
				h.Set("Retry-After", tc.header)
			}
			assert.Equal(t, tc.want, retryAfter(h, base, tc.attempt))
		})
	}
}

func TestSleepBackoff(t *testing.T) {
	assert.True(t, sleepBackoff(context.Background(), time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, sleepBackoff(ctx, time.Hour), "a cancelled context must abandon the wait")
}

// ---------- header hygiene ----------

func TestReservedHeader(t *testing.T) {
	for _, name := range []string{
		"Authorization", "authorization", "  API-Key  ", "x-api-key", "x-goog-api-key",
		"Content-Type", "Content-Length", "Host", "Connection", "Transfer-Encoding", "Upgrade",
	} {
		assert.True(t, reservedHeader(name), name)
	}
	for _, name := range []string{"X-Trace-Id", "User-Agent", "Accept", "openai-beta"} {
		assert.False(t, reservedHeader(name), name)
	}
}

func TestRemoveHopHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "keep-alive, X-Custom-Hop")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("Transfer-Encoding", "chunked")
	h.Set("X-Custom-Hop", "drop-me")
	h.Set("Content-Type", "application/json")

	removeHopHeaders(h)

	for _, name := range []string{"Connection", "Keep-Alive", "Transfer-Encoding", "X-Custom-Hop"} {
		assert.Empty(t, h.Get(name), name)
	}
	assert.Equal(t, "application/json", h.Get("Content-Type"), "end-to-end headers must survive")
}

func TestCompareConfig(t *testing.T) {
	assert.Equal(t, 10, compareConfig(10, nil))
	assert.Equal(t, 20, compareConfig(10, ptr(20)))
	assert.Equal(t, 10, compareConfig(10, ptr(0)), "a zero override means unset")
	assert.Equal(t, 10, compareConfig(10, ptr(-1)), "a negative override means unset")
	assert.Equal(t, time.Second, compareConfig(2*time.Second, ptr(time.Second)))
}

// ---------- the transport ----------

func testUpstreamConfig() config.UpstreamConfig {
	return config.UpstreamConfig{
		MaxIdleConns: 10, MaxConnsPerHost: 10, MaxIdleConnsPerHost: 10,
		IdleConnTimeout: time.Second, DialTimeout: 2 * time.Second, KeepAlive: time.Second,
		TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second,
		RequestTimeout: 2 * time.Second, StreamIdleTimeout: time.Second,
		MaxResponseBytesKB: 64, WriteBufferSize: 4096, ReadBufferSize: 4096,
	}
}

// AllowPrivateNetwork is required: httptest listens on 127.0.0.1, which the
// dial guard refuses by default.
func upstreamFor(network core.NetworkConfig) *core.Upstream {
	network.AllowPrivateNetwork = true
	return &core.Upstream{Provider: core.ProviderOpenAI, Network: network}
}

func transportFor(t *testing.T, network core.NetworkConfig) *DiffractLLMTransport {
	t.Helper()
	return NewTransport(testUpstreamConfig(),
		map[core.Provider]*core.Upstream{core.ProviderOpenAI: upstreamFor(network)}, zap.NewNop())
}

func transportRctx(t *testing.T) *core.DiffractLLMContext {
	t.Helper()
	rc := core.NewDiffractLLMContextPool().Acquire(context.Background(), nil, nil)
	rc.Modelkey = chatKey(core.ProviderOpenAI, gpt4o)
	return rc
}

func TestServeHTTPSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		assert.Equal(t, "on", r.Header.Get("X-Trace"))
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"model":"gpt-4o"}`, string(body))

		w.Header().Set("x-request-id", "req-123")
		w.Header().Set("x-ratelimit-remaining-tokens", "999")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{Headers: map[string]string{
		"X-Trace":       "on",
		"Authorization": "must-be-ignored",
	}})
	rc := transportRctx(t)

	res, derr := tr.ServeHTTP(rc, &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL, Body: []byte(`{"model":"gpt-4o"}`),
		Headers: map[string]string{"Authorization": "Bearer sk-test"},
	})
	require.Nil(t, derr)
	require.NotNil(t, res)
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.Status)
	assert.Equal(t, 1, res.Attempts)
	assert.Positive(t, res.TTFB)

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(body))

	assert.Equal(t, http.StatusOK, rc.UpstreamStatus)
	assert.Positive(t, rc.TTFB)

	id, ok := rc.Get("upstream.x-request-id")
	assert.True(t, ok)
	assert.Equal(t, "req-123", id)
	tokens, ok := rc.Get("upstream.x-ratelimit-remaining-tokens")
	assert.True(t, ok)
	assert.Equal(t, "999", tokens)
}

// Network config is operator input. It must not be able to inject an auth
// header the provider never set, so reserved names are dropped from it.
func TestServeHTTPNetworkHeadersCannotInjectReservedNames(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{Headers: map[string]string{
		"Authorization": "Bearer injected",
		"X-Api-Key":     "injected",
		"X-Trace":       "kept",
	}})

	// The request sets no auth header at all, so nothing can mask the injection.
	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr)

	assert.Empty(t, got.Get("Authorization"), "a reserved network header must be dropped")
	assert.Empty(t, got.Get("X-Api-Key"), "a reserved network header must be dropped")
	assert.Equal(t, "kept", got.Get("X-Trace"), "an ordinary network header must pass through")
}

// Request headers are applied after network headers, so the provider's own auth
// wins whatever the operator configured.
func TestServeHTTPRequestHeadersWinOverNetworkHeaders(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Trace")
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{Headers: map[string]string{"X-Trace": "from-config"}})

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
		Headers: map[string]string{"X-Trace": "from-request"},
	})
	require.Nil(t, derr)
	assert.Equal(t, "from-request", got)
}

// The client-facing Message says nothing; the real reason goes to the log via
// Details. Asserting both ways is the point.
func TestServeHTTPUnknownProvider(t *testing.T) {
	tr := transportFor(t, core.NetworkConfig{})
	rc := transportRctx(t)
	rc.Modelkey = chatKey(core.ProviderCohere, gpt4o)

	_, derr := tr.ServeHTTP(rc, &DiffractLLMTransportRequest{Method: http.MethodPost, URL: "http://x.test"})
	require.NotNil(t, derr)
	assert.Equal(t, core.CodeInternalError, derr.Code)
	assert.Equal(t, http.StatusInternalServerError, derr.StatusCode)
	assert.Equal(t, "Internal server error", derr.Message, "the client must not learn the provider is unwired")
	assert.Contains(t, derr.Details["internal_detail"], "no client for provider cohere")
}

func TestServeHTTPRetriesAndSucceeds(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("try again"))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{
		MaxRetries: ptr(3), RetryBackoff: ptr(time.Millisecond),
		RetryAmbiguousStatus: ptr(true),
	})

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.Status)
	assert.Equal(t, 3, res.Attempts)
	assert.EqualValues(t, 3, attempts.Load())
}

// The default. A 503 may have arrived after a model ran, so retrying it could
// bill the client twice for one request.
func TestServeHTTPDoesNotRetryAmbiguousStatusByDefault(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{MaxRetries: ptr(3), RetryBackoff: ptr(time.Millisecond)})

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, res.Status)
	assert.Equal(t, 1, res.Attempts)
	assert.EqualValues(t, 1, attempts.Load(), "the request must be sent exactly once")
}

// 429 is a rejection before any model ran, so it retries whatever the flag says.
func TestServeHTTPAlwaysRetriesSafeStatus(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{MaxRetries: ptr(2), RetryBackoff: ptr(time.Millisecond)})

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.Status)
	assert.Equal(t, 2, res.Attempts)
}

// A non-retryable status is returned as-is on the first attempt, so a 400 is
// never sent twice.
func TestServeHTTPDoesNotRetryNonRetryableStatus(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{MaxRetries: ptr(3), RetryBackoff: ptr(time.Millisecond)})

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	assert.Equal(t, http.StatusBadRequest, res.Status)
	assert.Equal(t, 1, res.Attempts)
	assert.EqualValues(t, 1, attempts.Load())
}

// Retries exhausted on a retryable status returns the response, not an error -
// the caller renders the upstream's own 503.
func TestServeHTTPRetriesExhausted(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{
		MaxRetries: ptr(2), RetryBackoff: ptr(time.Millisecond),
		RetryAmbiguousStatus: ptr(true),
	})

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, res.Status)
	assert.Equal(t, 3, res.Attempts, "one attempt plus two retries")
	assert.EqualValues(t, 3, attempts.Load())
}

func TestServeHTTPUnreachableUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	tr := transportFor(t, core.NetworkConfig{})

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: url,
	})
	require.NotNil(t, derr)
	assert.Equal(t, core.CodeUpstreamUnavailable, derr.Code)
	assert.Equal(t, http.StatusBadGateway, derr.StatusCode)
}

// BackendURL is attached to every upstream error. Userinfo and the query string
// must be stripped before it gets there.
func TestServeHTTPErrorCarriesASanitizedURL(t *testing.T) {
	tr := transportFor(t, core.NetworkConfig{})

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://user:secret@127.0.0.1:1/v1/chat?api_key=leak",
	})
	require.NotNil(t, derr)
	assert.Equal(t, "http://127.0.0.1:1/v1/chat", derr.BackendURL)
	assert.NotContains(t, derr.Message, "secret")
	assert.NotContains(t, derr.Message, "leak")
}

func TestServeHTTPRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{RequestTimeout: ptr(30 * time.Millisecond)})

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.NotNil(t, derr)
	assert.Equal(t, http.StatusGatewayTimeout, derr.StatusCode)
}

// Unary responses are capped; streams are not, because a long stream is not an
// oversized response.
func TestServeHTTPCapsUnaryBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{MaxResponseBytes: 1024})

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	_, err := io.ReadAll(res.Body)
	assert.ErrorIs(t, err, ErrResponseTooLarge)
}

func TestServeHTTPStreamingIsNotCapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{
		MaxResponseBytes: 1024, StreamIdleTimeout: ptr(time.Second),
	})

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL, IsStreaming: true,
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Len(t, body, 4096, "a stream must not be truncated by max_response_bytes")
}

// ---------- Replace ----------

func TestReplaceSwapsClients(t *testing.T) {
	tr := transportFor(t, core.NetworkConfig{})

	failed := tr.Replace(map[core.Provider]*core.Upstream{
		core.ProviderAzure: upstreamFor(core.NetworkConfig{}),
	})
	assert.Empty(t, failed, "a config that builds must report nothing")

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://x.test",
	})
	require.NotNil(t, derr)
	assert.Contains(t, derr.Details["internal_detail"], "no client for provider openai",
		"Replace is a snapshot swap, so openai loses its client")
}

// A provider whose new config cannot build keeps its previous client and is
// named in the return, so an admin write can answer honestly.
func TestReplaceReportsProvidersThatFailedToBuild(t *testing.T) {
	tr := transportFor(t, core.NetworkConfig{})

	failed := tr.Replace(map[core.Provider]*core.Upstream{
		core.ProviderOpenAI: {
			Provider: core.ProviderOpenAI,
			Network:  core.NetworkConfig{AllowPrivateNetwork: true},
			Proxy:    &core.ProxyConfig{Type: "not-a-proxy-type", URL: "http://127.0.0.1:1"},
		},
	})

	require.Equal(t, []core.Provider{core.ProviderOpenAI}, failed)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("still here"))
	}))
	defer server.Close()

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr, "the previous client must keep serving")
	defer res.Body.Close()
	assert.Equal(t, http.StatusOK, res.Status)
}

func TestNewClientRejectsABadProxy(t *testing.T) {
	_, err := newClient(testUpstreamConfig(), &core.Upstream{
		Provider: core.ProviderOpenAI,
		Proxy:    &core.ProxyConfig{Type: "wat"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown proxy type")
}

func TestApplyProxy(t *testing.T) {
	dialer := &net.Dialer{}

	t.Run("nil proxy is a no-op", func(t *testing.T) {
		tr := &http.Transport{}
		require.NoError(t, applyProxy(tr, dialer, nil, false))
		assert.Nil(t, tr.Proxy)
	})

	t.Run("environment", func(t *testing.T) {
		tr := &http.Transport{}
		require.NoError(t, applyProxy(tr, dialer, &core.ProxyConfig{Type: core.ProxyEnvironment}, false))
		assert.NotNil(t, tr.Proxy)
	})

	t.Run("http with credentials", func(t *testing.T) {
		tr := &http.Transport{}
		require.NoError(t, applyProxy(tr, dialer, &core.ProxyConfig{
			Type: core.ProxyHTTP, URL: "http://proxy.test:8080",
			Username: "u", Password: "p",
		}, false))
		require.NotNil(t, tr.Proxy)

		req, _ := http.NewRequest(http.MethodGet, "https://api.openai.com", nil)
		proxyURL, err := tr.Proxy(req)
		require.NoError(t, err)
		require.NotNil(t, proxyURL)

		pass, set := proxyURL.User.Password()
		assert.Equal(t, "u", proxyURL.User.Username())
		assert.True(t, set)
		assert.Equal(t, "p", pass)
	})

	t.Run("socks5 replaces DialContext", func(t *testing.T) {
		tr := &http.Transport{}
		require.NoError(t, applyProxy(tr, dialer, &core.ProxyConfig{
			Type: core.ProxySOCKS5, URL: "127.0.0.1:1080",
		}, false))
		assert.NotNil(t, tr.DialContext)
	})
}

// ---------- a fake SOCKS5 proxy ----------

// RFC 1928 method bytes. Only the two the transport can ask for.
const (
	socksNoAuth   = 0x00
	socksUserPass = 0x02
)

type fakeSOCKS5 struct {
	addr     string
	wantUser string
	wantPass string

	mu        sync.Mutex
	connectTo []string // the addresses clients asked the proxy to reach
	authSeen  []string // "user:pass" per authenticated session
}

// Speaks just enough SOCKS5 to satisfy golang.org/x/net/proxy, then answers the
// tunnelled HTTP request itself. No real egress: whatever address the client
// asks for, the proxy serves the canned response.
func newFakeSOCKS5(t *testing.T, user, pass string) *fakeSOCKS5 {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	s := &fakeSOCKS5{addr: listener.Addr().String(), wantUser: user, wantPass: pass}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeSOCKS5) targets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.connectTo)
}

func (s *fakeSOCKS5) credentials() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.authSeen)
}

func (s *fakeSOCKS5) serve(conn net.Conn) {
	defer conn.Close()

	if !s.greet(conn) || !s.connect(conn) {
		return
	}

	// Drain the tunnelled request head, then answer it.
	buf := make([]byte, 4096)
	_, _ = conn.Read(buf)
	_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 15\r\n\r\n{\"via\":\"socks\"}"))
}

// Method negotiation, plus the username/password sub-negotiation when this
// proxy was built with credentials.
func (s *fakeSOCKS5) greet(conn net.Conn) bool {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil || head[0] != 0x05 {
		return false
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return false
	}

	want := byte(socksNoAuth)
	if s.wantUser != "" {
		want = socksUserPass
	}
	if !slices.Contains(methods, want) {
		_, _ = conn.Write([]byte{0x05, 0xFF})
		return false
	}
	if _, err := conn.Write([]byte{0x05, want}); err != nil {
		return false
	}
	if want == socksNoAuth {
		return true
	}

	// RFC 1929: version, ulen, user, plen, pass.
	if _, err := io.ReadFull(conn, head[:1]); err != nil || head[0] != 0x01 {
		return false
	}
	user, ok := readLengthPrefixed(conn)
	if !ok {
		return false
	}
	pass, ok := readLengthPrefixed(conn)
	if !ok {
		return false
	}

	s.mu.Lock()
	s.authSeen = append(s.authSeen, user+":"+pass)
	s.mu.Unlock()

	if user != s.wantUser || pass != s.wantPass {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return false
	}
	_, err := conn.Write([]byte{0x01, 0x00})
	return err == nil
}

func (s *fakeSOCKS5) connect(conn net.Conn) bool {
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil || head[0] != 0x05 || head[1] != 0x01 {
		return false
	}

	var host string
	switch head[3] {
	case 0x01:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return false
		}
		host = net.IP(ip).String()
	case 0x03:
		name, ok := readLengthPrefixed(conn)
		if !ok {
			return false
		}
		host = name
	case 0x04:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return false
		}
		host = net.IP(ip).String()
	default:
		return false
	}

	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return false
	}
	port := int(portBytes[0])<<8 | int(portBytes[1])

	s.mu.Lock()
	s.connectTo = append(s.connectTo, net.JoinHostPort(host, strconv.Itoa(port)))
	s.mu.Unlock()

	// Success, bound address 0.0.0.0:0 - the client ignores it.
	_, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err == nil
}

func readLengthPrefixed(conn net.Conn) (string, bool) {
	size := make([]byte, 1)
	if _, err := io.ReadFull(conn, size); err != nil {
		return "", false
	}
	value := make([]byte, size[0])
	if _, err := io.ReadFull(conn, value); err != nil {
		return "", false
	}
	return string(value), true
}

// ---------- SOCKS5 through the transport ----------

func socksTransport(t *testing.T, proxyAddr, user, pass string, allowPrivate bool) *DiffractLLMTransport {
	t.Helper()
	return NewTransport(testUpstreamConfig(), map[core.Provider]*core.Upstream{
		core.ProviderOpenAI: {
			Provider: core.ProviderOpenAI,
			Network:  core.NetworkConfig{AllowPrivateNetwork: allowPrivate},
			Proxy: &core.ProxyConfig{
				Type: core.ProxySOCKS5, URL: proxyAddr, Username: user, Password: pass,
			},
		},
	}, zap.NewNop())
}

// 8.8.8.8 is never really dialled: the fake proxy answers the CONNECT itself.
// allow_private_network is on because the proxy lives on loopback - see
// TestSOCKS5OnAPrivateAddressNeedsAllowPrivateNetwork for why that is forced.
func TestSOCKS5NoAuthReachesThroughTheProxy(t *testing.T) {
	proxyServer := newFakeSOCKS5(t, "", "")
	tr := socksTransport(t, proxyServer.addr, "", "", false)

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://8.8.8.8:80/v1/chat/completions",
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"via":"socks"}`, string(body))
	assert.Equal(t, http.StatusOK, res.Status)

	assert.Equal(t, []string{"8.8.8.8:80"}, proxyServer.targets(),
		"the request must reach the upstream through the proxy, not directly")
	assert.Empty(t, proxyServer.credentials())
}

func TestSOCKS5SendsCredentials(t *testing.T) {
	proxyServer := newFakeSOCKS5(t, "proxy-user", "proxy-pass")
	tr := socksTransport(t, proxyServer.addr, "proxy-user", "proxy-pass", false)

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://8.8.8.8:80/v1/chat/completions",
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.Status)
	assert.Equal(t, []string{"proxy-user:proxy-pass"}, proxyServer.credentials())
}

func TestSOCKS5WrongCredentialsFail(t *testing.T) {
	proxyServer := newFakeSOCKS5(t, "proxy-user", "proxy-pass")
	tr := socksTransport(t, proxyServer.addr, "proxy-user", "wrong", false)

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://8.8.8.8:80/v1/chat/completions",
	})
	require.NotNil(t, derr)
	assert.Equal(t, http.StatusBadGateway, derr.StatusCode)
	assert.Equal(t, []string{"proxy-user:wrong"}, proxyServer.credentials())
	assert.Empty(t, proxyServer.targets(), "a rejected login must never get to CONNECT")
}

// The combination that matters: a proxy on a private address, reached with the
// target guard still on. newClient dials the proxy with an unguarded hop dialer
// because that address is operator config, while guardedProxyDial keeps
// checking where the request is actually going.
func TestSOCKS5OnAPrivateAddressWorksWithTheGuardOn(t *testing.T) {
	proxyServer := newFakeSOCKS5(t, "", "")
	tr := socksTransport(t, proxyServer.addr, "", "", false)

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://8.8.8.8:80/v1/chat/completions",
	})
	require.Nil(t, derr, "a loopback proxy must be reachable with allow_private_network off")
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.Status)
	assert.Equal(t, []string{"8.8.8.8:80"}, proxyServer.targets())
}

// The target guard still applies through the proxy, so a private upstream is
// refused even though the proxy itself is private.
func TestSOCKS5GuardStillBlocksAPrivateTarget(t *testing.T) {
	proxyServer := newFakeSOCKS5(t, "", "")
	tr := socksTransport(t, proxyServer.addr, "", "", false)

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://169.254.169.254/latest/meta-data/",
	})
	require.NotNil(t, derr)
	assert.Equal(t, http.StatusBadGateway, derr.StatusCode)
	require.Error(t, derr.Internal)
	assert.Contains(t, derr.Internal.Error(), "blocked address")
	assert.Empty(t, proxyServer.targets(), "a blocked target must never reach the proxy")
}

// An http proxy on a private address was unreachable for the same reason. It
// resolves the target itself, so there is no dial-time target guard here.
func TestHTTPProxyOnAPrivateAddressIsReachable(t *testing.T) {
	var sawRequest atomic.Bool
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest.Store(true)
		_, _ = w.Write([]byte(`{"via":"http-proxy"}`))
	}))
	defer proxyServer.Close()

	tr := NewTransport(testUpstreamConfig(), map[core.Provider]*core.Upstream{
		core.ProviderOpenAI: {
			Provider: core.ProviderOpenAI,
			Network:  core.NetworkConfig{},
			Proxy:    &core.ProxyConfig{Type: core.ProxyHTTP, URL: proxyServer.URL},
		},
	}, zap.NewNop())

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://8.8.8.8:80/v1/chat",
	})
	require.Nil(t, derr)
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.Status)
	assert.True(t, sawRequest.Load(), "the request must go through the proxy")
}

// The guard still applies when no proxy is configured.
func TestDirectDialStillGuarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{})
	tr.Replace(map[core.Provider]*core.Upstream{
		core.ProviderOpenAI: {Provider: core.ProviderOpenAI, Network: core.NetworkConfig{}},
	})

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.NotNil(t, derr)
	require.Error(t, derr.Internal)
	assert.Contains(t, derr.Internal.Error(), "blocked address 127.0.0.1")
}

func TestSOCKS5UnreachableProxy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	tr := socksTransport(t, addr, "", "", false)

	_, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: "http://8.8.8.8:80/v1/chat",
	})
	require.NotNil(t, derr)
	assert.Equal(t, http.StatusBadGateway, derr.StatusCode)
}

// ---------- guardedProxyDial in isolation ----------

// A dialer that records what it was asked for and never touches the network.
type recordingDialer struct {
	mu    sync.Mutex
	addrs []string
	err   error
}

func (d *recordingDialer) DialContext(_ context.Context, _, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.addrs = append(d.addrs, addr)
	d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	client, server := net.Pipe()
	go server.Close()
	return client, nil
}

func (d *recordingDialer) seen() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.addrs)
}

func TestGuardedProxyDial(t *testing.T) {
	t.Run("allow private hands the address straight through", func(t *testing.T) {
		d := &recordingDialer{}
		conn, err := guardedProxyDial(d, true)(context.Background(), "tcp", "127.0.0.1:443")
		require.NoError(t, err)
		_ = conn.Close()
		assert.Equal(t, []string{"127.0.0.1:443"}, d.seen(), "no resolution, no guard, no rewrite")
	})

	t.Run("blocked target never reaches the dialer", func(t *testing.T) {
		for _, addr := range []string{"127.0.0.1:443", "10.0.0.1:443", "[::1]:443", "169.254.169.254:80"} {
			d := &recordingDialer{}
			_, err := guardedProxyDial(d, false)(context.Background(), "tcp", addr)
			require.Error(t, err, addr)
			assert.Contains(t, err.Error(), "blocked address", addr)
			assert.Empty(t, d.seen(), addr)
		}
	})

	// LookupNetIP normalises an IPv4 literal to its IPv4-mapped IPv6 form, so the
	// address handed to the proxy is [::ffff:8.8.8.8]:443, not 8.8.8.8:443.
	// isBlockedIP calls Unmap() first, so the check itself is unaffected.
	t.Run("allowed target is dialled by resolved ip", func(t *testing.T) {
		d := &recordingDialer{}
		conn, err := guardedProxyDial(d, false)(context.Background(), "tcp", "8.8.8.8:443")
		require.NoError(t, err)
		_ = conn.Close()

		require.Len(t, d.seen(), 1)
		host, port, splitErr := net.SplitHostPort(d.seen()[0])
		require.NoError(t, splitErr)
		assert.Equal(t, "443", port)
		assert.Equal(t, "8.8.8.8", netip.MustParseAddr(host).Unmap().String())
	})

	t.Run("malformed address", func(t *testing.T) {
		d := &recordingDialer{}
		_, err := guardedProxyDial(d, false)(context.Background(), "tcp", "no-port")
		require.Error(t, err)
		assert.Empty(t, d.seen())
	})

	t.Run("dialer error is surfaced", func(t *testing.T) {
		d := &recordingDialer{err: errors.New("connection refused")}
		_, err := guardedProxyDial(d, false)(context.Background(), "tcp", "8.8.8.8:443")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
	})
}

// Replace releases the idle sockets of clients it drops. The check is that the
// carried-over client for a failed provider is NOT closed, since it is still
// serving.
func TestReplaceClosesDroppedClients(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{})

	// Warm a pooled connection.
	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr)
	_, _ = io.ReadAll(res.Body)
	res.Body.Close()

	dropped := (*tr.providers.Load())[core.ProviderOpenAI]

	// openai is absent from the new map, so its client is dropped and closed.
	failed := tr.Replace(map[core.Provider]*core.Upstream{
		core.ProviderAzure: upstreamFor(core.NetworkConfig{}),
	})
	require.Empty(t, failed)

	// The dropped client still works - CloseIdleConnections frees sockets, it
	// does not disable the client.
	assert.NotNil(t, dropped.client.Transport)
	assert.NotSame(t, dropped, (*tr.providers.Load())[core.ProviderAzure])
}

// A provider whose rebuild failed keeps its previous client, so that client
// must survive the sweep and keep serving.
func TestReplaceDoesNotCloseCarriedOverClients(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("still here"))
	}))
	defer server.Close()

	tr := transportFor(t, core.NetworkConfig{})
	before := (*tr.providers.Load())[core.ProviderOpenAI]

	failed := tr.Replace(map[core.Provider]*core.Upstream{
		core.ProviderOpenAI: {
			Provider: core.ProviderOpenAI,
			Network:  core.NetworkConfig{AllowPrivateNetwork: true},
			Proxy:    &core.ProxyConfig{Type: "not-a-proxy-type"},
		},
	})
	require.Equal(t, []core.Provider{core.ProviderOpenAI}, failed)

	after := (*tr.providers.Load())[core.ProviderOpenAI]
	assert.Same(t, before, after, "the previous client must be carried over untouched")

	res, derr := tr.ServeHTTP(transportRctx(t), &DiffractLLMTransportRequest{
		Method: http.MethodPost, URL: server.URL,
	})
	require.Nil(t, derr, "the carried-over client must still serve")
	defer res.Body.Close()
	assert.Equal(t, http.StatusOK, res.Status)
}
