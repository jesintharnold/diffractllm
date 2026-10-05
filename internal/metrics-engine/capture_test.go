package metricsengine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStagesAreBackToBackAndSkipUnreachedSteps(t *testing.T) {
	var marks [core.NumStages]time.Duration
	marks[core.StageAuth] = 1 * time.Millisecond
	marks[core.StageReadBody] = 3 * time.Millisecond
	marks[core.StageParse] = 4 * time.Millisecond
	marks[core.StageAdmission] = 6 * time.Millisecond
	marks[core.StageRouting] = 7 * time.Millisecond
	marks[core.StageUpstream] = 400 * time.Millisecond
	// unary request: no first token

	got := stages(marks, 900*time.Millisecond)
	require.Len(t, got, 7)
	assert.Equal(t, Stage{Name: "auth", StartUS: 0, EndUS: 1000, DurationUS: 1000}, got[0])
	assert.Equal(t, Stage{Name: "upstream", StartUS: 7000, EndUS: 400000, DurationUS: 393000}, got[5])
	assert.Equal(t, Stage{Name: "response", StartUS: 400000, EndUS: 900000, DurationUS: 500000}, got[6])
	for i := 1; i < len(got); i++ {
		assert.Equal(t, got[i-1].EndUS, got[i].StartUS, "stage %s must start where %s ended", got[i].Name, got[i-1].Name)
	}
}

func TestRejectedAtAuthHasOnlyTheAuthStage(t *testing.T) {
	assert.Nil(t, stages([core.NumStages]time.Duration{}, time.Millisecond)) // auth failed: nothing marked

	var marks [core.NumStages]time.Duration
	marks[core.StageAuth] = time.Millisecond
	got := stages(marks, time.Millisecond)
	assert.Equal(t, []Stage{{Name: "auth", StartUS: 0, EndUS: 1000, DurationUS: 1000}}, got)
}

// A streamed request's pipeline and TTFT reach the request detail screen.
func TestPipelineAndTTFTReachTheDetail(t *testing.T) {
	store := newTestStore(t)
	m := newTestEngine(store, 10)

	rc := okContext()
	rc.StartedAt = time.Now().Add(-time.Second)
	rc.Marks[core.StageAuth] = 2 * time.Millisecond
	rc.Marks[core.StageRouting] = 5 * time.Millisecond
	rc.Marks[core.StageUpstream] = 300 * time.Millisecond
	rc.Marks[core.StageFirstToken] = 450 * time.Millisecond
	m.AddEvent(rc)
	_, err := m.flush(context.Background())
	require.NoError(t, err)

	var id string
	require.NoError(t, store.db.QueryRow(`SELECT id FROM events`).Scan(&id))
	d, err := store.GetRequestDetailByID(context.Background(), id)
	require.NoError(t, err)

	assert.Equal(t, 450.0, d.TTFTMS)
	require.Len(t, d.Pipeline, 5) // auth, routing, upstream, first_token, response
	assert.Equal(t, "first_token", d.Pipeline[3].Name)
	assert.Equal(t, int64(150000), d.Pipeline[3].DurationUS)
	assert.Equal(t, "response", d.Pipeline[4].Name)
}

func payloadEngine(t *testing.T, mode string, maxKB int) (*MetricsEngine, *DuckDBStore) {
	store := newTestStore(t)
	return NewMetricsEngine(store, &config.MetricsEngineConfig{
		BufferCapacity: 10, FlushInterval: time.Hour, QueryTimeout: 5 * time.Second,
		MaxPage: 200, MaxWindow: 90 * 24 * time.Hour, PayloadMode: mode, PayloadMaxKB: maxKB,
	}, zap.NewNop()), store
}

// record sends one context through the engine and returns its event id.
func record(t *testing.T, m *MetricsEngine, store *DuckDBStore, rc *core.DiffractLLMContext) string {
	t.Helper()
	m.AddEvent(rc)
	_, err := m.flush(context.Background())
	require.NoError(t, err)
	var id string
	require.NoError(t, store.db.QueryRow(`SELECT id FROM events ORDER BY started_at DESC LIMIT 1`).Scan(&id))
	return id
}

func withBodies(rc *core.DiffractLLMContext, req, res string) *core.DiffractLLMContext {
	rc.BodyBytes = []byte(req)
	rc.ResponseBody = []byte(res)
	return rc
}

func TestPayloadModeNoneKeepsNothing(t *testing.T) {
	m, store := payloadEngine(t, config.PayloadModeNone, 64)
	id := record(t, m, store, withBodies(okContext(), `{"model":"gpt-4o"}`, `{"id":"r1"}`))

	_, err := m.GetPayloadByID(context.Background(), id)
	assert.ErrorIs(t, err, ErrNotFound)
	d, err := m.GetRequestDetailByID(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "none", d.PayloadState)
}

func TestPayloadModeErrorsOnlySkipsSuccesses(t *testing.T) {
	m, store := payloadEngine(t, config.PayloadModeErrorsOnly, 64)

	okID := record(t, m, store, withBodies(okContext(), `{"model":"gpt-4o"}`, `{"id":"r1"}`))
	_, err := m.GetPayloadByID(context.Background(), okID)
	assert.ErrorIs(t, err, ErrNotFound)

	rc := withBodies(rejectedContext(), "", `{"error":{"message":"invalid key"}}`)
	rejID := record(t, m, store, rc)
	p, err := m.GetPayloadByID(context.Background(), rejID)
	require.NoError(t, err)
	assert.Nil(t, p.ClientRequest) // rejected before the body was read
	assert.JSONEq(t, `{"error":{"message":"invalid key"}}`, string(p.ClientResponse))
}

func TestPayloadModeFullKeepsBothBodies(t *testing.T) {
	m, store := payloadEngine(t, config.PayloadModeFull, 64)
	id := record(t, m, store, withBodies(okContext(), `{"model":"gpt-4o"}`, `{"id":"r1"}`))

	p, err := m.GetPayloadByID(context.Background(), id)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"gpt-4o"}`, string(p.ClientRequest))
	assert.JSONEq(t, `{"id":"r1"}`, string(p.ClientResponse))

	d, err := m.GetRequestDetailByID(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "ready", d.PayloadState)
}

// A body over the cap is clipped and kept as a JSON string; the batch still writes.
func TestOversizedBodyIsClippedNotDropped(t *testing.T) {
	m, store := payloadEngine(t, config.PayloadModeFull, 1)
	big := `{"messages":"` + strings.Repeat("x", 4096) + `"}`
	id := record(t, m, store, withBodies(okContext(), big, `{"id":"r1"}`))

	p, err := m.GetPayloadByID(context.Background(), id)
	require.NoError(t, err)
	var clipped string
	require.NoError(t, json.Unmarshal(p.ClientRequest, &clipped))
	assert.Len(t, clipped, 1024)
	assert.True(t, strings.HasPrefix(clipped, `{"messages":"xxx`))

	d, err := m.GetRequestDetailByID(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "truncated", d.PayloadState)
}

// A client that sent something other than JSON must not fail the whole batch.
func TestMalformedBodyIsStoredAsAString(t *testing.T) {
	m, store := payloadEngine(t, config.PayloadModeFull, 64)
	id := record(t, m, store, withBodies(rejectedContext(), `{"model": oops`, `{"error":{}}`))

	p, err := m.GetPayloadByID(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, `"{\"model\": oops"`, string(p.ClientRequest))
}

// The captured copy must not share memory with the request buffer the pool reuses.
func TestCapturedBodyIsACopy(t *testing.T) {
	m, _ := payloadEngine(t, config.PayloadModeFull, 64)
	rc := withBodies(okContext(), `{"a":1}`, `{"b":2}`)
	e := acquireEvent()
	e.fill(rc)
	m.capturePayload(e, rc)

	rc.BodyBytes[2] = 'Z'
	assert.Equal(t, `{"a":1}`, string(e.Payload.ClientRequest))
}
