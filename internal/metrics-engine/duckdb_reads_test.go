package metricsengine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"diffractllm/internal/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// seedEvent is a written-to-disk event at t0+at with the given outcome.
func seedEvent(id string, at time.Duration, result Result, status int, latency time.Duration, provider string) *Event {
	e := &Event{
		SchemaVersion:   SchemaVersion,
		ID:              id,
		Kind:            KindLLM,
		RequestKind:     core.ChatRequest,
		RequestEndpoint: "/openai/v1/chat/completions",
		Governance:      Governance{ClientID: "acme"},
		Outcome:         Outcome{Result: result, HTTPStatus: status},
		Timing:          Timing{Start: t0.Add(at), TotalUS: latency.Microseconds()},
		PayloadState:    PayloadNone,
	}
	if provider != "" {
		e.CostNanoUSD = 1_500_000 // $0.0015
		e.Routing = &Routing{CredentialID: "cred-1", CredentialName: "openai-prod"}
		e.LLM = &LLM{RequestedModel: "gpt-4o", Provider: provider, Model: "gpt-4o",
			Usage: &core.Usage{InputTokens: 1284, OutputTokens: 902, CachedInputTokens: 512}}
	}
	return e
}

func seededEngine(t *testing.T) (*MetricsEngine, *DuckDBStore) {
	store := newTestStore(t)
	require.NoError(t, store.Write(context.Background(), []*Event{
		seedEvent("e1", 1*time.Minute, ResultOK, 200, 100*time.Millisecond, "openai"),
		seedEvent("e2", 2*time.Minute, ResultOK, 200, 200*time.Millisecond, "openai"),
		seedEvent("e3", 3*time.Minute, ResultOK, 200, 300*time.Millisecond, "azure"),
		seedEvent("e4", 31*time.Minute, ResultUpstreamError, 502, 400*time.Millisecond, "openai"),
		seedEvent("e5", 32*time.Minute, ResultRejected, 401, time.Millisecond, ""),
		seedEvent("e6", 33*time.Minute, ResultClientAbort, 200, 50*time.Millisecond, "azure"),
		seedEvent("old", -time.Hour, ResultOK, 200, time.Second, "openai"), // outside the range
	}))
	return newTestEngine(store, 10), store
}

func TestOverviewTiles(t *testing.T) {
	m, _ := seededEngine(t)
	tiles, err := m.GetOverviewTiles(context.Background(), t0, t0.Add(time.Hour))
	require.NoError(t, err)

	assert.Equal(t, int64(6), tiles.Requests)
	assert.Equal(t, int64(2), tiles.Errors) // upstream 502 + rejected 401
	assert.Equal(t, int64(1), tiles.Cancelled)
	assert.InDelta(t, 2.0/6, tiles.ErrorRate, 1e-9)
	assert.Equal(t, int64(5*2186), tiles.TotalTokens) // billed = in + out, five requests with usage
	assert.InDelta(t, 5*0.0015, tiles.SpendUSD, 1e-12)
	// The 1ms rejection never reached a provider, so it is not in the latency.
	assert.Greater(t, tiles.P95LatencyMS, 300.0)
	assert.LessOrEqual(t, tiles.P99LatencyMS, 400.0)
}

func TestBucketForFollowsBifrost(t *testing.T) {
	day := 24 * time.Hour
	for r, want := range map[time.Duration]time.Duration{
		time.Hour: time.Minute, 6 * time.Hour: 10 * time.Minute, day: time.Hour,
		3 * day: 8 * time.Hour, 7 * day: day, 29 * day: day, 30 * day: 3 * day, 90 * day: 7 * day,
	} {
		assert.Equal(t, want, bucketFor(r), "range %s", r)
	}
}

func TestRequestSummaryPicksTheBucketFromTheRange(t *testing.T) {
	m, _ := seededEngine(t)
	s, err := m.GetRequestSummaryByTime(context.Background(), t0, t0.Add(2*time.Hour))
	require.NoError(t, err)

	assert.Equal(t, 600.0, s.BucketSeconds) // 2h -> 10 minute bars
	require.Len(t, s.Points, 12)
	assert.Equal(t, t0, s.Points[0].T)
	assert.Equal(t, []ProviderCount{{"azure", 1}, {"openai", 2}}, s.Points[0].Providers)
	assert.Zero(t, s.Points[0].Errors)

	assert.Equal(t, t0.Add(30*time.Minute), s.Points[3].T)
	assert.Equal(t, int64(2), s.Points[3].Errors)
	assert.Equal(t, int64(1), s.Points[3].Cancelled)
	assert.Empty(t, s.Points[3].Providers)

	assert.NotNil(t, s.Points[1].Providers) // [] in JSON, not null
	assert.Empty(t, s.Points[1].Providers)
}

// Bars sit on clock minutes even when the range starts mid-minute; the first one is partial.
func TestRequestSummaryAlignsBarsToTheClock(t *testing.T) {
	m, _ := seededEngine(t)
	from := t0.Add(90 * time.Second) // 10:01:30
	s, err := m.GetRequestSummaryByTime(context.Background(), from, from.Add(time.Hour))
	require.NoError(t, err)

	assert.Equal(t, 60.0, s.BucketSeconds)
	require.Len(t, s.Points, 61)
	assert.Equal(t, t0.Add(time.Minute), s.Points[0].T)                    // 10:01
	assert.Empty(t, s.Points[0].Providers)                                 // e1 at 10:01:00 is before from
	assert.Equal(t, []ProviderCount{{"openai", 1}}, s.Points[1].Providers) // e2 at 10:02
	assert.Equal(t, from.Add(time.Hour), s.To)
}

func TestRequestLogsPageNewestFirst(t *testing.T) {
	m, _ := seededEngine(t)
	page, err := m.GetRequestLogsByTime(context.Background(), t0, t0.Add(time.Hour), 0, 2)
	require.NoError(t, err)

	assert.Equal(t, int64(6), page.Total)
	require.Len(t, page.Rows, 2)
	assert.Equal(t, "e6", page.Rows[0].ID)
	assert.Equal(t, "client_abort", page.Rows[0].Result)
	assert.Equal(t, "azure", page.Rows[0].Provider)
	assert.Equal(t, "e5", page.Rows[1].ID)
	assert.Equal(t, 401, page.Rows[1].HTTPStatus)
	assert.Empty(t, page.Rows[1].Provider, "rejected before routing: no provider")
	assert.Equal(t, time.UTC, page.Rows[0].StartedAt.Location())

	page, err = m.GetRequestLogsByTime(context.Background(), t0, t0.Add(time.Hour), 4, 2)
	require.NoError(t, err)
	require.Len(t, page.Rows, 2)
	r := page.Rows[1]
	assert.Equal(t, "e1", r.ID)
	assert.Equal(t, "openai", r.Provider)
	assert.Equal(t, "gpt-4o", r.Model)
	assert.Equal(t, 100.0, r.LatencyMS)
	assert.Equal(t, int64(1284), r.InputTokens)
	assert.Equal(t, int64(902), r.OutputTokens)
	assert.InDelta(t, 0.0015, r.CostUSD, 1e-12)
	assert.Equal(t, "acme", r.ClientID)
}

func TestRequestDetailWithCostBreakdown(t *testing.T) {
	store := newTestStore(t)
	in, out, cached := 2.5e-6, 10e-6, 1.25e-6
	e := seedEvent("d1", 0, ResultOK, 200, 900*time.Millisecond, "openai")
	e.LLM.UpstreamModel = "gpt-4o-2024-08-06"
	e.LLM.Pricing = &core.Pricing{InputCostPerToken: &in, OutputCostPerToken: &out, CacheReadInputTokenCost: &cached}
	e.Outcome.UpstreamHTTPStatus = 200
	require.NoError(t, store.Write(context.Background(), []*Event{e}))

	d, err := newTestEngine(store, 10).GetRequestDetailByID(context.Background(), "d1")
	require.NoError(t, err)

	assert.Equal(t, "gpt-4o-2024-08-06", d.Model.Upstream)
	assert.Equal(t, "openai-prod", d.Context.Credential)
	assert.Equal(t, int64(2186), d.Tokens.Billed)
	assert.Equal(t, int64(512), d.Tokens.CachedInput)
	assert.Equal(t, 200, d.Upstream.HTTPStatus)
	assert.Nil(t, d.Error)
	require.Len(t, d.Cost.Lines, 3)
	assert.Equal(t, core.CostLine{Item: "input", Quantity: 772, Unit: "token", UnitCost: in, CostUSD: 772 * in}, d.Cost.Lines[0])

	_, err = newTestEngine(store, 10).GetRequestDetailByID(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPayloadByIDOmitsBodiesNotKept(t *testing.T) {
	store := newTestStore(t)
	e := seedEvent("p1", 0, ResultOK, 200, time.Millisecond, "openai")
	e.Payload = &Payload{ClientRequest: json.RawMessage(`{"model":"gpt-4o"}`)}
	require.NoError(t, store.Write(context.Background(), []*Event{e}))
	m := newTestEngine(store, 10)

	p, err := m.GetPayloadByID(context.Background(), "p1")
	require.NoError(t, err)
	body, err := json.Marshal(p)
	require.NoError(t, err)
	assert.JSONEq(t, `{"client_request":{"model":"gpt-4o"}}`, string(body))

	_, err = m.GetPayloadByID(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestReadsRejectBadQueries(t *testing.T) {
	m, _ := seededEngine(t)
	ctx := context.Background()

	_, err := m.GetOverviewTiles(ctx, t0, t0)
	assert.ErrorIs(t, err, ErrInvalidQuery)
	_, err = m.GetOverviewTiles(ctx, t0, t0.Add(91*24*time.Hour))
	assert.ErrorIs(t, err, ErrInvalidQuery)
	_, err = m.GetRequestSummaryByTime(ctx, t0, t0.Add(59*time.Minute)) // under the 1h minimum
	assert.ErrorIs(t, err, ErrInvalidQuery)
	_, err = m.GetRequestLogsByTime(ctx, t0, t0.Add(time.Hour), 0, 201)
	assert.ErrorIs(t, err, ErrInvalidQuery)
}

// governed is a seeded event charged to a key and budget.
func governed(id string, at time.Duration, vk, budget string, nano int64, result Result) *Event {
	e := seedEvent(id, at, result, 200, 100*time.Millisecond, "openai")
	e.Governance = Governance{ClientID: "client-" + vk, VirtualKeyID: vk, BudgetID: budget}
	e.CostNanoUSD = nano
	return e
}

func governedEngine(t *testing.T) *MetricsEngine {
	store := newTestStore(t)
	require.NoError(t, store.Write(context.Background(), []*Event{
		governed("a1", 1*time.Minute, "vk-a", "b1", 1_000_000, ResultOK),
		governed("a2", 65*time.Minute, "vk-a", "b1", 2_000_000, ResultUpstreamError),
		governed("b1", 2*time.Minute, "vk-b", "b2", 5_000_000, ResultOK),
		seedEvent("anon", 3*time.Minute, ResultRejected, 401, time.Millisecond, ""), // no key: skipped
	}))
	return newTestEngine(store, 10)
}

func TestTopVirtualKeysRankBySpend(t *testing.T) {
	m := governedEngine(t)
	keys, err := m.GetTopVirtualKeys(context.Background(), t0, t0.Add(2*time.Hour), 10)
	require.NoError(t, err)

	require.Len(t, keys, 2)
	assert.Equal(t, "vk-b", keys[0].VirtualKeyID) // $0.005 beats $0.003
	assert.Equal(t, VirtualKeyUsage{
		VirtualKeyID: "vk-a", ClientID: "client-vk-a", Requests: 2, Errors: 1,
		Tokens: 2 * 2186, SpendUSD: 0.003, LastUsed: t0.Add(65 * time.Minute),
	}, keys[1])

	keys, err = m.GetTopVirtualKeys(context.Background(), t0, t0.Add(2*time.Hour), 1)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "vk-b", keys[0].VirtualKeyID)
}

// One row per provider, biggest spend first; the rejection with no provider is left out.
func TestProviderUsage(t *testing.T) {
	m, _ := seededEngine(t)
	usage, err := m.GetProviderUsage(context.Background(), t0, t0.Add(time.Hour))
	require.NoError(t, err)

	require.Len(t, usage, 2)
	assert.Equal(t, "openai", usage[0].Provider)
	assert.Equal(t, int64(3), usage[0].Requests)
	assert.Equal(t, int64(3*2186), usage[0].Tokens)
	assert.InDelta(t, 3*0.0015, usage[0].SpendUSD, 1e-12)
	assert.Equal(t, "azure", usage[1].Provider)
	assert.Equal(t, int64(2), usage[1].Requests)
}

func TestBudgetSpendByTime(t *testing.T) {
	m := governedEngine(t)
	s, err := m.GetBudgetSpendByTime(context.Background(), "b1", t0, t0.Add(2*time.Hour))
	require.NoError(t, err)

	assert.Equal(t, "b1", s.BudgetID)
	assert.Equal(t, 600.0, s.BucketSeconds) // 2h -> 10 minute bars
	require.Len(t, s.Points, 12)
	assert.Equal(t, SpendPoint{T: t0, SpendUSD: 0.001}, s.Points[0])
	assert.Equal(t, SpendPoint{T: t0.Add(60 * time.Minute), SpendUSD: 0.002}, s.Points[6])
	assert.Zero(t, s.Points[1].SpendUSD) // b2's spend at 10:02 is not here

	s, err = m.GetBudgetSpendByTime(context.Background(), "nope", t0, t0.Add(2*time.Hour))
	require.NoError(t, err)
	require.Len(t, s.Points, 12)
	for _, p := range s.Points {
		assert.Zero(t, p.SpendUSD)
	}
}

func TestTopAndBudgetRejectBadQueries(t *testing.T) {
	m := governedEngine(t)
	ctx := context.Background()

	_, err := m.GetTopVirtualKeys(ctx, t0, t0.Add(time.Hour), 0)
	assert.ErrorIs(t, err, ErrInvalidQuery)
	_, err = m.GetTopVirtualKeys(ctx, t0, t0.Add(time.Hour), 51)
	assert.ErrorIs(t, err, ErrInvalidQuery)
	_, err = m.GetBudgetSpendByTime(ctx, "", t0, t0.Add(time.Hour))
	assert.ErrorIs(t, err, ErrInvalidQuery)
	_, err = m.GetBudgetSpendByTime(ctx, "b1", t0, t0.Add(30*time.Minute))
	assert.ErrorIs(t, err, ErrInvalidQuery)
}
