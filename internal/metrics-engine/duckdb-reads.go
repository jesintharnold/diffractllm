package metricsengine

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"diffractllm/internal/core"

	"github.com/bytedance/sonic"
)

var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidQuery       = errors.New("invalid query")
	ErrAmbiguousRequestID = errors.New("request id matches multiple events")
)

const (
	isError     = `outcome_result NOT IN ('ok', 'client_abort')`
	isCancelled = `outcome_result = 'client_abort'`
)

type OverviewTiles struct {
	Requests     int64   `json:"requests"`
	Errors       int64   `json:"errors"`
	Cancelled    int64   `json:"cancelled"`
	ErrorRate    float64 `json:"error_rate"` // errors / requests, 0..1
	P95LatencyMS float64 `json:"p95_latency_ms"`
	P99LatencyMS float64 `json:"p99_latency_ms"`
	TotalTokens  int64   `json:"total_tokens"` // billed: input + output
	SpendUSD     float64 `json:"spend_usd"`
}

type TimeSummary struct {
	From          time.Time   `json:"from"`
	To            time.Time   `json:"to"`
	BucketSeconds float64     `json:"bucket_seconds"`
	Points        []TimePoint `json:"points"`
}

type TimePoint struct {
	T         time.Time       `json:"t"`
	Errors    int64           `json:"errors"`
	Cancelled int64           `json:"cancelled"`
	Providers []ProviderCount `json:"providers"`
}

type ProviderCount struct {
	Provider string `json:"provider"`
	Success  int64  `json:"success"`
}

type RequestLogPage struct {
	Total int64           `json:"total"`
	Rows  []RequestLogRow `json:"rows"`
}

type RequestLogRow struct {
	ID           string    `json:"id"`
	RequestID    string    `json:"request_id,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	RequestKind  string    `json:"request_kind"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	LatencyMS    float64   `json:"latency_ms"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	HTTPStatus   int       `json:"http_status"`
	Result       string    `json:"result"`
	ClientID     string    `json:"client_id"`
}

type RequestDetail struct {
	ID          string       `json:"id"`
	RequestID   string       `json:"request_id,omitempty"`
	StartedAt   time.Time    `json:"started_at"`
	RequestKind string       `json:"request_kind,omitempty"`
	Endpoint    string       `json:"endpoint"`
	Result      string       `json:"result"`
	HTTPStatus  int          `json:"http_status"`
	LatencyMS   float64      `json:"latency_ms"`
	TTFBMS      float64      `json:"ttfb_ms,omitempty"`
	TTFTMS      float64      `json:"ttft_ms,omitempty"`
	Error       *DetailError `json:"error,omitempty"`

	Model    DetailModel    `json:"model"`
	Tokens   DetailTokens   `json:"tokens"`
	Context  DetailContext  `json:"context"`
	Upstream DetailUpstream `json:"upstream"`
	Pipeline []Stage        `json:"pipeline,omitempty"`
	Cost     DetailCost     `json:"cost"`

	PayloadState string `json:"payload_state"` // none | ready | truncated: whether /payload has bodies
}

type DetailError struct {
	Code     string `json:"code,omitempty"`
	Category string `json:"category,omitempty"`
	Message  string `json:"message,omitempty"`
}

type DetailModel struct {
	Requested    string `json:"requested,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	Upstream     string `json:"upstream,omitempty"`
	Stream       bool   `json:"stream"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type DetailTokens struct {
	Input         int64 `json:"input,omitempty"`
	Output        int64 `json:"output,omitempty"`
	CachedInput   int64 `json:"cached_input,omitempty"`
	CacheCreation int64 `json:"cache_creation,omitempty"`
	Reasoning     int64 `json:"reasoning,omitempty"`
	Billed        int64 `json:"billed,omitempty"` // input + output
}

type DetailContext struct {
	VirtualKeyID string `json:"virtual_key_id,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	BudgetID     string `json:"budget_id,omitempty"`
	Credential   string `json:"credential,omitempty"`
	RoutingMode  string `json:"routing_mode,omitempty"`
}

type DetailUpstream struct {
	HTTPStatus int               `json:"http_status,omitempty"`
	Attempts   int               `json:"attempts,omitempty"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

type DetailCost struct {
	TotalUSD float64         `json:"total_usd"`
	Lines    []core.CostLine `json:"lines,omitempty"`
}

func usd(nano int64) float64 { return float64(nano) / 1e9 }
func ms(us int64) float64    { return float64(us) / 1e3 }

func (ds *DuckDBStore) GetOverviewTiles(ctx context.Context, from, to time.Time) (*OverviewTiles, error) {
	q := `SELECT count(*),
		count(*) FILTER (` + isError + `),
		count(*) FILTER (` + isCancelled + `),
		quantile_cont(timing_total_us, 0.95) FILTER (routing_credential_id IS NOT NULL),
		quantile_cont(timing_total_us, 0.99) FILTER (routing_credential_id IS NOT NULL),
		coalesce(sum(coalesce(usage_input_tokens, 0) + coalesce(usage_output_tokens, 0)), 0)::BIGINT,
		coalesce(sum(cost_nano_usd), 0)::BIGINT
	FROM events WHERE started_at >= $1 AND started_at < $2`

	var t OverviewTiles
	var p95, p99 sql.NullFloat64
	var nano int64
	if err := ds.db.QueryRowContext(ctx, q, from, to).Scan(
		&t.Requests, &t.Errors, &t.Cancelled, &p95, &p99, &t.TotalTokens, &nano); err != nil {
		return nil, fmt.Errorf("overview tiles: %w", err)
	}
	if t.Requests > 0 {
		t.ErrorRate = float64(t.Errors) / float64(t.Requests)
	}
	t.P95LatencyMS = p95.Float64 / 1e3
	t.P99LatencyMS = p99.Float64 / 1e3
	t.SpendUSD = usd(nano)
	return &t, nil
}

func (ds *DuckDBStore) GetRequestSummaryByTime(ctx context.Context, from, to time.Time, bucket time.Duration) (*TimeSummary, error) {
	q := `SELECT (epoch_us(started_at) - $1) // $2 AS idx, coalesce(llm_provider, ''),
		count(*) FILTER (outcome_result = 'ok'),
		count(*) FILTER (` + isError + `),
		count(*) FILTER (` + isCancelled + `)
	FROM events WHERE started_at >= $3 AND started_at < $4
	GROUP BY ALL ORDER BY idx, 2`

	start, n := bucketStart(from, to, bucket)
	s := &TimeSummary{From: from, To: to, BucketSeconds: bucket.Seconds(), Points: make([]TimePoint, n)}
	for i := range s.Points {
		s.Points[i] = TimePoint{T: start.Add(time.Duration(i) * bucket), Providers: []ProviderCount{}}
	}

	rows, err := ds.db.QueryContext(ctx, q, start.UnixMicro(), bucket.Microseconds(), from, to)
	if err != nil {
		return nil, fmt.Errorf("request summary: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var idx, ok, errs, cancelled int64
		var provider string
		if err := rows.Scan(&idx, &provider, &ok, &errs, &cancelled); err != nil {
			return nil, fmt.Errorf("request summary: %w", err)
		}
		p := &s.Points[idx]
		p.Errors += errs
		p.Cancelled += cancelled
		if ok > 0 && provider != "" {
			p.Providers = append(p.Providers, ProviderCount{Provider: provider, Success: ok})
		}
	}
	return s, rows.Err()
}

func (ds *DuckDBStore) GetRequestLogsByTime(ctx context.Context, from, to time.Time, offset, limit int) (*RequestLogPage, error) {
	q := `SELECT id, coalesce(request_id, ''), started_at, coalesce(request_kind, ''), coalesce(llm_provider, ''),
		coalesce(llm_model, llm_requested_model, ''), timing_total_us, coalesce(usage_input_tokens, 0), coalesce(usage_output_tokens, 0), cost_nano_usd,
		outcome_http_status, outcome_result, coalesce(governance_client_id, ''), count(*) OVER ()
	FROM events WHERE started_at >= $1 AND started_at < $2
	ORDER BY started_at DESC, id DESC LIMIT $3 OFFSET $4`

	rows, err := ds.db.QueryContext(ctx, q, from, to, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("request logs: %w", err)
	}
	defer rows.Close()

	page := &RequestLogPage{Rows: make([]RequestLogRow, 0, limit)}
	for rows.Next() {
		var r RequestLogRow
		var latencyUS, nano int64
		if err := rows.Scan(&r.ID, &r.RequestID, &r.StartedAt, &r.RequestKind, &r.Provider, &r.Model, &latencyUS, &r.InputTokens,
			&r.OutputTokens, &nano, &r.HTTPStatus, &r.Result, &r.ClientID, &page.Total); err != nil {
			return nil, fmt.Errorf("request logs: %w", err)
		}
		r.StartedAt = r.StartedAt.UTC()
		r.LatencyMS = ms(latencyUS)
		r.CostUSD = usd(nano)
		page.Rows = append(page.Rows, r)
	}
	return page, rows.Err()
}

// GetRequestDetailByRequestID resolves a gateway request ID only when it identifies
// one event. Client-supplied X-Request-ID values are not guaranteed to be unique.
func (ds *DuckDBStore) GetRequestDetailByRequestID(ctx context.Context, requestID string) (*RequestDetail, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id FROM events WHERE request_id = $1
		ORDER BY started_at DESC, id DESC LIMIT 2`, requestID)
	if err != nil {
		return nil, fmt.Errorf("request detail by request id %s: %w", requestID, err)
	}
	defer rows.Close()

	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("request detail by request id %s: %w", requestID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("request detail by request id %s: %w", requestID, err)
	}
	if len(ids) == 0 {
		return nil, ErrNotFound
	}
	if len(ids) > 1 {
		return nil, ErrAmbiguousRequestID
	}
	return ds.GetRequestDetailByID(ctx, ids[0])
}

func (ds *DuckDBStore) GetRequestDetailByID(ctx context.Context, id string) (*RequestDetail, error) {
	q := `SELECT id, coalesce(request_id, ''), started_at, coalesce(request_kind, ''), request_endpoint,
		outcome_result, outcome_http_status, timing_total_us, coalesce(timing_ttfb_us, 0), coalesce(timing_ttft_us, 0),
		coalesce(outcome_error_code, ''), coalesce(outcome_error_category, ''), coalesce(outcome_error_message, ''),
		coalesce(llm_requested_model, ''), coalesce(llm_provider, ''), coalesce(llm_model, ''),
		coalesce(llm_upstream_model, ''), coalesce(llm_stream, false), coalesce(llm_finish_reason, ''),
		coalesce(usage_input_tokens, 0), coalesce(usage_output_tokens, 0), coalesce(usage_cached_input_tokens, 0),
		coalesce(usage_cache_creation_tokens, 0), coalesce(usage_reasoning_tokens, 0),
		coalesce(governance_virtual_key_id, ''), coalesce(governance_client_id, ''), coalesce(governance_budget_id, ''),
		coalesce(routing_credential_name, ''), coalesce(routing_mode, ''),
		coalesce(outcome_upstream_http_status, 0), coalesce(routing_attempts, 0),
		cost_nano_usd, coalesce(timing_stages::VARCHAR, ''), coalesce(llm_pricing::VARCHAR, ''),
		coalesce(usage_detail::VARCHAR, ''), coalesce(headers::VARCHAR, ''), payload_state
	FROM events WHERE id = $1`

	var d RequestDetail
	var e DetailError
	var latencyUS, ttfbUS, ttftUS, nano int64
	var stages, pricing, usage, headers string
	err := ds.db.QueryRowContext(ctx, q, id).Scan(
		&d.ID, &d.RequestID, &d.StartedAt, &d.RequestKind, &d.Endpoint,
		&d.Result, &d.HTTPStatus, &latencyUS, &ttfbUS, &ttftUS,
		&e.Code, &e.Category, &e.Message,
		&d.Model.Requested, &d.Model.Provider, &d.Model.Model,
		&d.Model.Upstream, &d.Model.Stream, &d.Model.FinishReason,
		&d.Tokens.Input, &d.Tokens.Output, &d.Tokens.CachedInput,
		&d.Tokens.CacheCreation, &d.Tokens.Reasoning,
		&d.Context.VirtualKeyID, &d.Context.ClientID, &d.Context.BudgetID,
		&d.Context.Credential, &d.Context.RoutingMode,
		&d.Upstream.HTTPStatus, &d.Upstream.Attempts,
		&nano, &stages, &pricing, &usage, &headers, &d.PayloadState)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("request detail %s: %w", id, err)
	}

	d.StartedAt = d.StartedAt.UTC()
	d.LatencyMS = ms(latencyUS)
	d.TTFBMS = ms(ttfbUS)
	d.TTFTMS = ms(ttftUS)
	d.Tokens.Billed = d.Tokens.Input + d.Tokens.Output
	d.Cost.TotalUSD = usd(nano)
	if e != (DetailError{}) {
		d.Error = &e
	}
	if headers != "" {
		if err := sonic.UnmarshalString(headers, &d.Upstream.Headers); err != nil {
			return nil, fmt.Errorf("request detail %s headers: %w", id, err)
		}
		d.Upstream.RequestID = cmp.Or(d.Upstream.Headers["x-request-id"], d.Upstream.Headers["apim-request-id"])
	}
	if stages != "" {
		if err := sonic.UnmarshalString(stages, &d.Pipeline); err != nil {
			return nil, fmt.Errorf("request detail %s stages: %w", id, err)
		}
	}
	if pricing != "" && usage != "" {
		var p core.Pricing
		var u core.Usage
		if err := sonic.UnmarshalString(pricing, &p); err != nil {
			return nil, fmt.Errorf("request detail %s pricing: %w", id, err)
		}
		if err := sonic.UnmarshalString(usage, &u); err != nil {
			return nil, fmt.Errorf("request detail %s usage: %w", id, err)
		}
		d.Cost.Lines = core.CostBreakdown(p, u)
	}
	return &d, nil
}

func (ds *DuckDBStore) GetPayloadByID(ctx context.Context, id string) (*Payload, error) {
	q := `SELECT client_request::VARCHAR, normalized_request::VARCHAR, provider_request::VARCHAR,
		provider_response::VARCHAR, normalized_response::VARCHAR, client_response::VARCHAR
	FROM event_payloads WHERE id = $1`

	var b [6]sql.NullString
	err := ds.db.QueryRowContext(ctx, q, id).Scan(&b[0], &b[1], &b[2], &b[3], &b[4], &b[5])
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("payload %s: %w", id, err)
	}
	raw := func(s sql.NullString) json.RawMessage {
		if !s.Valid {
			return nil
		}
		return json.RawMessage(s.String)
	}
	return &Payload{
		ClientRequest:      raw(b[0]),
		NormalizedRequest:  raw(b[1]),
		ProviderRequest:    raw(b[2]),
		ProviderResponse:   raw(b[3]),
		NormalizedResponse: raw(b[4]),
		ClientResponse:     raw(b[5]),
	}, nil
}

type VirtualKeyUsage struct {
	VirtualKeyID string    `json:"virtual_key_id"`
	ClientID     string    `json:"client_id"`
	Requests     int64     `json:"requests"`
	Errors       int64     `json:"errors"`
	Tokens       int64     `json:"tokens"` // billed: input + output
	SpendUSD     float64   `json:"spend_usd"`
	LastUsed     time.Time `json:"last_used"`
}

type BudgetSpend struct {
	BudgetID      string       `json:"budget_id"`
	From          time.Time    `json:"from"`
	To            time.Time    `json:"to"`
	BucketSeconds float64      `json:"bucket_seconds"`
	Points        []SpendPoint `json:"points"`
}

type SpendPoint struct {
	T        time.Time `json:"t"`
	SpendUSD float64   `json:"spend_usd"`
}

func bucketStart(from, to time.Time, bucket time.Duration) (time.Time, int) {
	b := bucket.Microseconds()
	start := time.UnixMicro(from.UnixMicro() / b * b).UTC()
	return start, int((to.Sub(start) + bucket - 1) / bucket)
}

func (ds *DuckDBStore) GetTopVirtualKeys(ctx context.Context, from, to time.Time, limit int) ([]VirtualKeyUsage, error) {
	q := `SELECT governance_virtual_key_id, coalesce(any_value(governance_client_id), ''),
		count(*), count(*) FILTER (` + isError + `),
		coalesce(sum(coalesce(usage_input_tokens, 0) + coalesce(usage_output_tokens, 0)), 0)::BIGINT,
		coalesce(sum(cost_nano_usd), 0)::BIGINT, max(started_at)
	FROM events
	WHERE governance_virtual_key_id IS NOT NULL AND started_at >= $1 AND started_at < $2
	GROUP BY governance_virtual_key_id
	ORDER BY 6 DESC, 3 DESC, 1 LIMIT $3`

	rows, err := ds.db.QueryContext(ctx, q, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("top virtual keys: %w", err)
	}
	defer rows.Close()

	out := make([]VirtualKeyUsage, 0, limit)
	for rows.Next() {
		var k VirtualKeyUsage
		var nano int64
		if err := rows.Scan(&k.VirtualKeyID, &k.ClientID, &k.Requests, &k.Errors, &k.Tokens, &nano, &k.LastUsed); err != nil {
			return nil, fmt.Errorf("top virtual keys: %w", err)
		}
		k.SpendUSD = usd(nano)
		k.LastUsed = k.LastUsed.UTC()
		out = append(out, k)
	}
	return out, rows.Err()
}

type ProviderUsage struct {
	Provider string  `json:"provider"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	SpendUSD float64 `json:"spend_usd"`
}

func (ds *DuckDBStore) GetProviderUsage(ctx context.Context, from, to time.Time) ([]ProviderUsage, error) {
	q := `SELECT llm_provider, count(*),
		coalesce(sum(coalesce(usage_input_tokens, 0) + coalesce(usage_output_tokens, 0)), 0)::BIGINT,
		coalesce(sum(cost_nano_usd), 0)::BIGINT
	FROM events
	WHERE coalesce(llm_provider, '') <> '' AND started_at >= $1 AND started_at < $2
	GROUP BY llm_provider ORDER BY 4 DESC, 1`

	rows, err := ds.db.QueryContext(ctx, q, from, to)
	if err != nil {
		return nil, fmt.Errorf("provider usage: %w", err)
	}
	defer rows.Close()

	out := []ProviderUsage{}
	for rows.Next() {
		var u ProviderUsage
		var nano int64
		if err := rows.Scan(&u.Provider, &u.Requests, &u.Tokens, &nano); err != nil {
			return nil, fmt.Errorf("provider usage: %w", err)
		}
		u.SpendUSD = usd(nano)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (ds *DuckDBStore) GetBudgetSpendByTime(ctx context.Context, budgetID string, from, to time.Time, bucket time.Duration) (*BudgetSpend, error) {
	q := `SELECT (epoch_us(started_at) - $1) // $2 AS idx, sum(cost_nano_usd)::BIGINT
	FROM events WHERE governance_budget_id = $3 AND started_at >= $4 AND started_at < $5
	GROUP BY idx`

	start, n := bucketStart(from, to, bucket)
	s := &BudgetSpend{BudgetID: budgetID, From: from, To: to, BucketSeconds: bucket.Seconds(), Points: make([]SpendPoint, n)}
	for i := range s.Points {
		s.Points[i].T = start.Add(time.Duration(i) * bucket)
	}

	rows, err := ds.db.QueryContext(ctx, q, start.UnixMicro(), bucket.Microseconds(), budgetID, from, to)
	if err != nil {
		return nil, fmt.Errorf("budget spend: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var idx, nano int64
		if err := rows.Scan(&idx, &nano); err != nil {
			return nil, fmt.Errorf("budget spend: %w", err)
		}
		s.Points[idx].SpendUSD = usd(nano)
	}
	return s, rows.Err()
}
