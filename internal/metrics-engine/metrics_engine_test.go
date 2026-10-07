package metricsengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestStore(t *testing.T) *DuckDBStore {
	t.Helper()
	store := NewDuckDBStore(zap.NewNop(), context.Background())
	require.NoError(t, store.Init(filepath.Join(t.TempDir(), "metrics.duckdb"), context.Background()))
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newTestEngine(store OLAPStore, capacity int) *MetricsEngine {
	return NewMetricsEngine(store, &config.MetricsEngineConfig{
		BufferCapacity: capacity,
		FlushInterval:  time.Hour,
		QueryTimeout:   5 * time.Second,
		MaxPage:        200,
		MaxWindow:      90 * 24 * time.Hour,
	}, zap.NewNop())
}

// okContext is a request that reached the provider and succeeded.
func okContext() *core.DiffractLLMContext {
	req := httptest.NewRequest("POST", "/openai/v1/chat/completions", nil)
	rc := core.NewDiffractLLMContextPool().Acquire(context.Background(), req, httptest.NewRecorder())
	rc.RequestID = "client-req-1"
	rc.StartedAt = time.Now().Add(-120 * time.Millisecond)
	rc.RequestKind = core.ChatRequest
	rc.ClientID = "acme"
	rc.VirtualKeyID = "vk-1"
	rc.BudgetRef = "budget-1"
	rc.RequestedModel = "openai/gpt-4o"
	rc.Modelkey = core.CatalogKey{Provider: core.Provider("openai"), ModelName: "gpt-4o"}
	rc.SelectedCredential = &core.Credential{ID: "cred-1"}
	rc.VirtualKeyPolicy = &core.VirtualKey{Mode: core.VKWeighted}
	rc.UpstreamStatus = 200
	rc.TTFB = 80 * time.Millisecond
	rc.ResponseStatus = 200
	rc.RequestCompleted = true
	rc.Usage = &core.Usage{InputTokens: 100, OutputTokens: 40, TotalTokens: 140, CachedInputTokens: 20}
	rc.Cost = 0.0015
	return rc
}

// rejectedContext is a request refused at auth: no routing, no usage, no upstream.
func rejectedContext() *core.DiffractLLMContext {
	req := httptest.NewRequest("POST", "/openai/v1/embeddings", nil)
	rc := core.NewDiffractLLMContextPool().Acquire(context.Background(), req, httptest.NewRecorder())
	rc.StartedAt = time.Now().Add(-2 * time.Millisecond)
	rc.RequestKind = core.EmbeddingRequest
	rc.ResponseStatus = 401
	rc.Error = core.NewAuthFailed("bad key")
	return rc
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
	return n
}

func TestAddEventThenFlushWritesTheRow(t *testing.T) {
	store := newTestStore(t)
	m := newTestEngine(store, 10)

	m.AddEvent(okContext())
	require.Equal(t, 1, m.buffer.Len())

	detail, err := m.flush(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, detail["written"])
	assert.Equal(t, 0, m.buffer.Len())

	var (
		kind, requestKind, endpoint, result, model, provider, credential, mode, tier string
		requestID, vkey, budget                                                      string
		httpStatus, upstreamStatus                                                   int
		in, out, total, cached, cost, ttfb, totalUS                                  int64
		usageDetail                                                                  string
	)
	err = store.db.QueryRow(`
		SELECT kind, request_kind, request_endpoint, outcome_result, llm_model, llm_provider,
		       routing_credential_id, routing_mode, usage_tier,
		       request_id, governance_virtual_key_id, governance_budget_id,
		       outcome_http_status, outcome_upstream_http_status,
		       usage_input_tokens, usage_output_tokens, usage_total_tokens, usage_cached_input_tokens,
		       cost_nano_usd, timing_ttfb_us, timing_total_us, usage_detail::VARCHAR
		FROM events`).Scan(
		&kind, &requestKind, &endpoint, &result, &model, &provider, &credential, &mode, &tier,
		&requestID, &vkey, &budget, &httpStatus, &upstreamStatus,
		&in, &out, &total, &cached, &cost, &ttfb, &totalUS, &usageDetail)
	require.NoError(t, err)

	assert.Equal(t, "llm", kind)
	assert.Equal(t, "chat", requestKind)
	assert.Equal(t, "/openai/v1/chat/completions", endpoint)
	assert.Equal(t, "ok", result)
	assert.Equal(t, "gpt-4o", model)
	assert.Equal(t, "openai", provider)
	assert.Equal(t, "cred-1", credential)
	assert.Equal(t, core.VKWeighted.String(), mode)
	assert.Equal(t, core.TierStandard.String(), tier)
	assert.Equal(t, "client-req-1", requestID)
	assert.Equal(t, "vk-1", vkey)
	assert.Equal(t, "budget-1", budget)
	assert.Equal(t, 200, httpStatus)
	assert.Equal(t, 200, upstreamStatus)
	assert.Equal(t, []int64{100, 40, 140, 20}, []int64{in, out, total, cached})
	assert.Equal(t, core.ToNanoUSD(0.0015), cost)
	assert.Equal(t, int64(80_000), ttfb)
	assert.Greater(t, totalUS, int64(0))

	var usage core.Usage
	require.NoError(t, json.Unmarshal([]byte(usageDetail), &usage))
	assert.Equal(t, int64(140), usage.TotalTokens)
}

func TestRejectedRequestStoresNullsNotZeros(t *testing.T) {
	store := newTestStore(t)
	m := newTestEngine(store, 10)

	m.AddEvent(rejectedContext())
	_, err := m.flush(context.Background())
	require.NoError(t, err)

	var (
		result, requestKind              string
		httpStatus                       int
		credential, mode, tier, errCode  sql.NullString
		in, ttfb, upstream               sql.NullInt64
		usageDetail, stages, attemptList sql.NullString
	)
	err = store.db.QueryRow(`
		SELECT outcome_result, request_kind, outcome_http_status,
		       routing_credential_id, routing_mode, usage_tier, outcome_error_code,
		       usage_input_tokens, timing_ttfb_us, outcome_upstream_http_status,
		       usage_detail::VARCHAR, timing_stages::VARCHAR, routing_attempt_list::VARCHAR
		FROM events`).Scan(&result, &requestKind, &httpStatus,
		&credential, &mode, &tier, &errCode, &in, &ttfb, &upstream,
		&usageDetail, &stages, &attemptList)
	require.NoError(t, err)

	assert.Equal(t, "rejected", result)
	assert.Equal(t, "embedding", requestKind)
	assert.Equal(t, 401, httpStatus)
	assert.True(t, errCode.Valid, "the error code is stored")
	for name, v := range map[string]bool{
		"routing_credential_id": credential.Valid, "routing_mode": mode.Valid, "usage_tier": tier.Valid,
		"usage_input_tokens": in.Valid, "timing_ttfb_us": ttfb.Valid, "outcome_upstream_http_status": upstream.Valid,
		"usage_detail": usageDetail.Valid, "timing_stages": stages.Valid, "routing_attempt_list": attemptList.Valid,
	} {
		assert.False(t, v, "%s must be NULL for a rejected request", name)
	}
}

func TestAddEventSkipsContextsWithNothingToRecord(t *testing.T) {
	m := newTestEngine(nil, 10)

	m.AddEvent(nil)
	rc := okContext()
	rc.StartedAt = time.Time{}
	m.AddEvent(rc)

	assert.Equal(t, 0, m.buffer.Len())
	var nilEngine *MetricsEngine
	assert.NotPanics(t, func() { nilEngine.AddEvent(okContext()) })
}

func TestFullBufferDropsAndCounts(t *testing.T) {
	m := newTestEngine(nil, 2)

	for range 3 {
		m.AddEvent(okContext())
	}

	assert.Equal(t, 2, m.buffer.Len())
	assert.Equal(t, int64(1), m.buffer.DroppedCount())
}

func TestHookRecordsAndNeverFails(t *testing.T) {
	m := newTestEngine(nil, 10)
	hook := NewEventHook(m)

	assert.Equal(t, "metrics_event", hook.Name())
	assert.Nil(t, hook.Execute(okContext()))
	assert.Nil(t, hook.Execute(rejectedContext()))
	assert.Equal(t, 2, m.buffer.Len())
}

func TestRegisterHooksAddsAPostCallHook(t *testing.T) {
	engine := core.NewHookEngine(zap.NewNop())
	require.NoError(t, RegisterHooks(engine, newTestEngine(nil, 10)))
	assert.Equal(t, 1, engine.Registered())
}

func TestPayloadRowIsWrittenWithItsEvent(t *testing.T) {
	store := newTestStore(t)
	m := newTestEngine(store, 10)

	e := acquireEvent()
	e.fill(okContext())
	e.Payload = &Payload{
		ClientRequest:  json.RawMessage(`{"model":"gpt-4o"}`),
		ClientResponse: json.RawMessage(`{"id":"chatcmpl-1"}`),
	}
	e.PayloadState = PayloadReady
	require.True(t, m.buffer.Append(e))

	_, err := m.flush(context.Background())
	require.NoError(t, err)

	var clientReq, clientResp string
	var providerReq sql.NullString
	require.NoError(t, store.db.QueryRow(`
		SELECT client_request::VARCHAR, client_response::VARCHAR, provider_request::VARCHAR
		FROM event_payloads`).Scan(&clientReq, &clientResp, &providerReq))
	assert.JSONEq(t, `{"model":"gpt-4o"}`, clientReq)
	assert.JSONEq(t, `{"id":"chatcmpl-1"}`, clientResp)
	assert.False(t, providerReq.Valid, "a body that wasn't kept is NULL")
}

// The whole batch is one transaction: a failure on the last row — after the first
// 2,048-row chunk has already been appended — leaves both tables empty.
func TestFailedBatchLeavesNothingBehind(t *testing.T) {
	store := newTestStore(t)

	events := make([]*Event, 0, 3001)
	for range 3000 {
		e := acquireEvent()
		e.fill(okContext())
		events = append(events, e)
	}
	dup := acquireEvent()
	dup.fill(okContext())
	dup.ID = events[0].ID // primary-key clash on the very last row
	events = append(events, dup)

	err := store.Write(context.Background(), events)
	require.Error(t, err)
	assert.Equal(t, 0, countRows(t, store.db, "events"))
	assert.Equal(t, 0, countRows(t, store.db, "event_payloads"))
}

// failingStore fails the first `failures` writes, then records what it receives.
type failingStore struct {
	OLAPStore // reads are not exercised; calling one panics
	failures  int
	calls     int
	written   []string
}

func (s *failingStore) Write(_ context.Context, events []*Event) error {
	s.calls++
	if s.calls <= s.failures {
		return errors.New("disk hiccup")
	}
	for _, e := range events {
		s.written = append(s.written, e.ID)
	}
	return nil
}

func (s *failingStore) Close() error { return nil }

func TestFailedFlushIsRetriedWithTheNextBatch(t *testing.T) {
	store := &failingStore{failures: 1}
	m := newTestEngine(store, 10)

	m.AddEvent(okContext())
	_, err := m.flush(context.Background())
	require.Error(t, err)
	assert.Len(t, m.retry, 1, "the failed batch waits for the next flush")

	m.AddEvent(okContext())
	detail, err := m.flush(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, detail["written"])
	assert.Len(t, store.written, 2)
	assert.NotEqual(t, store.written[0], store.written[1], "no event written twice")
	assert.Empty(t, m.retry)
}

func TestBatchIsDroppedAfterThreeAttempts(t *testing.T) {
	store := &failingStore{failures: maxWriteAttempts}
	m := newTestEngine(store, 10)

	m.AddEvent(okContext())
	for range maxWriteAttempts {
		_, err := m.flush(context.Background())
		require.Error(t, err)
	}

	assert.Empty(t, m.retry)
	assert.Equal(t, int64(1), m.lost)
	assert.Equal(t, 0, m.attempts)
}

func TestReleasedEventIsZeroed(t *testing.T) {
	e := acquireEvent()
	e.fill(okContext())
	releaseEvent(e)

	assert.Equal(t, Event{}, *e)
}

func TestReopeningTheStoreDoesNotRerunMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.duckdb")

	first := NewDuckDBStore(zap.NewNop(), context.Background())
	require.NoError(t, first.Init(path, context.Background()))
	require.NoError(t, first.Close())

	second := NewDuckDBStore(zap.NewNop(), context.Background())
	require.NoError(t, second.Init(path, context.Background()))
	defer second.Close()

	assert.Equal(t, 2, countRows(t, second.db, "schema_migrations"))
}

// Many request goroutines append while the flush job drains: nothing is lost or counted twice.
func TestConcurrentAppendAndDrain(t *testing.T) {
	m := newTestEngine(nil, 100_000)
	const writers, perWriter = 50, 200

	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWriter {
				m.AddEvent(okContext())
			}
		}()
	}

	drained := 0
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		batch := m.buffer.Drain()
		drained += len(batch)
		releaseAll(batch)
	}
	drained += len(m.buffer.Drain())

	assert.Equal(t, writers*perWriter, drained)
	assert.Equal(t, int64(0), m.buffer.DroppedCount())
}

// Prune removes rows older than the cutoff from both tables and keeps the rest.
func TestPruneDeletesOnlyOldRows(t *testing.T) {
	store := newTestStore(t)

	event := func(start time.Time) *Event {
		e := acquireEvent()
		e.fill(okContext())
		e.Timing.Start = start
		e.Payload = &Payload{ClientRequest: json.RawMessage(`{"model":"gpt-4o"}`)}
		return e
	}
	now := time.Now().UTC()
	require.NoError(t, store.Write(context.Background(), []*Event{
		event(now.Add(-48 * time.Hour)),
		event(now.Add(-47 * time.Hour)),
		event(now),
	}))

	result, err := store.Prune(context.Background(), now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(2), result["total_events"])
	assert.Equal(t, int64(2), result["total_payload_events"])
	assert.Equal(t, 1, countRows(t, store.db, "events"))
	assert.Equal(t, 1, countRows(t, store.db, "event_payloads"))
}

// The rate a request was billed at is stored with it, so cost stays auditable after a price change.
func TestPricingSnapshotIsStored(t *testing.T) {
	store := newTestStore(t)
	m := newTestEngine(store, 10)

	rate := 0.0000025
	rc := okContext()
	rc.Pricing = &core.Pricing{InputCostPerToken: &rate}
	m.AddEvent(rc)
	_, err := m.flush(context.Background())
	require.NoError(t, err)

	var pricing string
	require.NoError(t, store.db.QueryRow(`SELECT llm_pricing::VARCHAR FROM events`).Scan(&pricing))
	assert.JSONEq(t, `{"input_cost_per_token":0.0000025}`, pricing)
}

// Retries and the provider's request id reach the request detail screen.
func TestUpstreamAttemptsAndHeadersReachTheDetail(t *testing.T) {
	store := newTestStore(t)
	m := newTestEngine(store, 10)

	rc := okContext()
	rc.UpstreamAttempts = 3
	rc.UpstreamStatus = 200
	rc.Overwrite("upstream.apim-request-id", "azure-req-9")
	rc.Overwrite("upstream.x-ratelimit-remaining-tokens", "1200")
	m.AddEvent(rc)
	_, err := m.flush(context.Background())
	require.NoError(t, err)

	var id string
	require.NoError(t, store.db.QueryRow(`SELECT id FROM events`).Scan(&id))
	d, err := store.GetRequestDetailByID(context.Background(), id)
	require.NoError(t, err)

	assert.Equal(t, 3, d.Upstream.Attempts)
	assert.Equal(t, 200, d.Upstream.HTTPStatus)
	assert.Equal(t, "azure-req-9", d.Upstream.RequestID)
	assert.Equal(t, map[string]string{
		"apim-request-id":              "azure-req-9",
		"x-ratelimit-remaining-tokens": "1200",
	}, d.Upstream.Headers)
}
