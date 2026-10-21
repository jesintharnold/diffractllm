package metricsengine

import (
	"bytes"
	"context"
	"diffractllm/internal/observability/logging"
	"errors"
	"fmt"
	"slices"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/worker"

	"go.uber.org/zap"
)

const (
	jobEventsFlush    = "events_flush"
	jobEventsPrune    = "events_prune"
	maxWriteAttempts  = 3
	defaultPruneEvery = 24 * time.Hour
)

type MetricsEngine struct {
	store    OLAPStore
	buffer   *EventsBuffer
	workers  *worker.Group
	logger   *zap.Logger
	config   *config.MetricsEngineConfig
	retry    []*Event
	attempts int
	lost     int64
}

func NewMetricsEngine(store OLAPStore, cfg *config.MetricsEngineConfig) *MetricsEngine {
	log := logging.For("metrics")
	return &MetricsEngine{
		store:  store,
		buffer: NewEventsBuffer(cfg.BufferCapacity),
		logger: log,
		config: cfg,
	}
}

func (m *MetricsEngine) Start(ctx context.Context) error {
	m.workers = worker.NewGroup("metrics")
	pruneEvery := m.config.PruneInterval
	if pruneEvery <= 0 {
		pruneEvery = defaultPruneEvery
	}
	if err := m.workers.Add(
		&worker.Job{
			Name:      jobEventsFlush,
			Interval:  m.config.FlushInterval,
			RunAtStop: true,
			Run:       m.flush,
		},
		&worker.Job{Name: jobEventsPrune, Interval: pruneEvery, Run: m.prune},
	); err != nil {
		return err
	}
	if err := m.workers.Start(ctx); err != nil {
		return err
	}
	_ = m.workers.Trigger(jobEventsPrune) // catch up right after a start, not a day later
	return nil
}

func (m *MetricsEngine) prune(ctx context.Context) (worker.Detail, error) {
	if m.config.EventsDays <= 0 || m.config.PayloadsDays <= 0 {
		return worker.Detail{"skipped": "retention is not set"}, nil
	}
	now := time.Now().UTC()
	deleted, err := m.store.Prune(ctx, now.AddDate(0, 0, -m.config.EventsDays), now.AddDate(0, 0, -m.config.PayloadsDays))
	if err != nil {
		return nil, err
	}
	return worker.Detail{"events_deleted": deleted["total_events"], "payloads_deleted": deleted["total_payload_events"]}, nil
}

func (m *MetricsEngine) Shutdown(ctx context.Context) error {
	var err error
	if m.workers != nil {
		err = m.workers.Shutdown(ctx)
	}
	if cerr := m.store.Close(); err == nil {
		err = cerr
	}
	return err
}

func (m *MetricsEngine) Stats() []worker.JobStats {
	if m == nil || m.workers == nil {
		return nil
	}
	return m.workers.Stats()
}

func (m *MetricsEngine) AddEvent(rctx *core.DiffractLLMContext) {
	if m == nil || rctx == nil || rctx.StartedAt.IsZero() {
		return
	}
	e := acquireEvent()
	e.fill(rctx)
	m.capturePayload(e, rctx)
	n, ok := m.buffer.Append(e)
	if !ok {
		releaseEvent(e)
		return
	}
	if m.workers != nil && n >= m.buffer.maxCapacity/2 {
		_ = m.workers.Trigger(jobEventsFlush) // flush early instead of dropping
	}
}

func (m *MetricsEngine) flush(ctx context.Context) (worker.Detail, error) {
	events := append(m.retry, m.buffer.Drain()...)
	m.retry = nil

	detail := worker.Detail{"dropped": m.buffer.DroppedCount(), "lost": m.lost}
	if len(events) == 0 {
		detail["written"] = 0
		return detail, nil
	}

	if err := m.store.Write(ctx, events); err != nil {
		m.attempts++
		if m.attempts < maxWriteAttempts {
			m.retry = events
			detail["retrying"] = len(events)
			return detail, fmt.Errorf("write attempt %d of %d: %w", m.attempts, maxWriteAttempts, err)
		}
		m.attempts = 0
		m.lost += int64(len(events))
		detail["lost"] = m.lost
		releaseAll(events)
		return detail, fmt.Errorf("batch of %d dropped after %d attempts: %w", len(events), maxWriteAttempts, err)
	}

	m.attempts = 0
	detail["written"] = len(events)
	releaseAll(events)
	return detail, nil
}

func releaseAll(events []*Event) {
	for _, e := range events {
		releaseEvent(e)
	}
}

const minRange = time.Hour

func (m *MetricsEngine) checkRange(from, to time.Time) error {
	if to.Sub(from) < minRange {
		return fmt.Errorf("%w: range must be at least %s", ErrInvalidQuery, minRange)
	}
	if to.Sub(from) > m.config.MaxWindow {
		return fmt.Errorf("%w: range exceeds %s", ErrInvalidQuery, m.config.MaxWindow)
	}
	return nil
}

func bucketFor(r time.Duration) time.Duration {
	const day = 24 * time.Hour
	switch {
	case r >= 365*day:
		return 30 * day
	case r >= 90*day:
		return 7 * day
	case r >= 30*day:
		return 3 * day
	case r >= 7*day:
		return day
	case r >= 3*day:
		return 8 * time.Hour
	case r >= day:
		return time.Hour
	case r >= 2*time.Hour:
		return 10 * time.Minute
	default:
		return time.Minute
	}
}

func (m *MetricsEngine) GetOverviewTiles(ctx context.Context, from, to time.Time) (*OverviewTiles, error) {
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetOverviewTiles(ctx, from.UTC(), to.UTC())
}

func (m *MetricsEngine) GetRequestSummaryByTime(ctx context.Context, from, to time.Time, view BucketView) (*TimeSummary, error) {
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	bucket, zone, err := resolveBucket(view, from, to)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetRequestSummaryByTime(ctx, from.UTC(), to.UTC(), bucket, zone)
}

func (m *MetricsEngine) GetRequestLogsByTime(ctx context.Context, from, to time.Time, offset, limit int, filter RequestFilter) (*RequestLogPage, error) {
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	if offset < 0 || limit < 1 || limit > m.config.MaxPage {
		return nil, fmt.Errorf("%w: need offset >= 0 and 1 <= limit <= %d", ErrInvalidQuery, m.config.MaxPage)
	}
	switch filter.Status {
	case "", StatusSuccess, StatusError, StatusCancelled:
	default:
		return nil, fmt.Errorf("%w: status must be %s, %s or %s", ErrInvalidQuery, StatusSuccess, StatusError, StatusCancelled)
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetRequestLogsByTime(ctx, from.UTC(), to.UTC(), offset, limit, filter)
}

func (m *MetricsEngine) GetRequestFilterOptions(ctx context.Context, from, to time.Time) (*RequestFilterOptions, error) {
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetRequestFilterOptions(ctx, from.UTC(), to.UTC())
}

func (m *MetricsEngine) GetRequestDetailByID(ctx context.Context, id string) (*RequestDetail, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidQuery)
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	detail, err := m.store.GetRequestDetailByID(ctx, id)
	if !errors.Is(err, ErrNotFound) {
		return detail, err
	}
	return m.store.GetRequestDetailByRequestID(ctx, id)
}

func (m *MetricsEngine) GetPayloadByID(ctx context.Context, id string) (*Payload, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidQuery)
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetPayloadByID(ctx, id)
}

const maxTopLimit = 50

func (m *MetricsEngine) GetProviderUsage(ctx context.Context, from, to time.Time) ([]ProviderUsage, error) {
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetProviderUsage(ctx, from.UTC(), to.UTC())
}

func (m *MetricsEngine) GetCredentialUsage(ctx context.Context, provider string) ([]CredentialUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetCredentialUsage(ctx, provider, time.Now().UTC().Add(-24*time.Hour))
}

// Last use and 24h request count for every virtual key, for the Virtual keys screens.
func (m *MetricsEngine) GetVirtualKeyLastUse(ctx context.Context) ([]VirtualKeyLastUse, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetVirtualKeyLastUse(ctx, time.Now().UTC().Add(-24*time.Hour))
}

func (m *MetricsEngine) GetTopVirtualKeys(ctx context.Context, from, to time.Time, limit int) ([]VirtualKeyUsage, error) {
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxTopLimit {
		return nil, fmt.Errorf("%w: need 1 <= limit <= %d", ErrInvalidQuery, maxTopLimit)
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetTopVirtualKeys(ctx, from.UTC(), to.UTC(), limit)
}

type BucketView struct {
	Bucket time.Duration  // 0 picks from the range; otherwise one of drillBuckets
	Zone   *time.Location // nil is UTC; buckets start on this zone's clock boundaries
}

var drillBuckets = []time.Duration{
	time.Minute, 10 * time.Minute, time.Hour, 8 * time.Hour,
	24 * time.Hour, 72 * time.Hour, 168 * time.Hour, 720 * time.Hour,
}

const maxBuckets = 1000

func resolveBucket(view BucketView, from, to time.Time) (time.Duration, *time.Location, error) {
	bucket := view.Bucket
	if bucket == 0 {
		bucket = bucketFor(to.Sub(from))
	} else if !slices.Contains(drillBuckets, bucket) {
		return 0, nil, fmt.Errorf("%w: bucket must be 1m, 10m, 1h, 8h, 1d, 3d, 7d or 30d", ErrInvalidQuery)
	}
	if to.Sub(from)/bucket > maxBuckets {
		return 0, nil, fmt.Errorf("%w: that range needs more than %d buckets of %s", ErrInvalidQuery, maxBuckets, bucket)
	}
	zone := view.Zone
	if zone == nil {
		zone = time.UTC
	}
	return bucket, zone, nil
}

func (m *MetricsEngine) GetBudgetSpendByTime(ctx context.Context, budgetID string, from, to time.Time, view BucketView) (*BudgetSpend, error) {
	if budgetID == "" {
		return nil, fmt.Errorf("%w: budget id is required", ErrInvalidQuery)
	}
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	bucket, zone, err := resolveBucket(view, from, to)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetBudgetSpendByTime(ctx, budgetID, from.UTC(), to.UTC(), bucket, zone)
}

func (m *MetricsEngine) capturePayload(e *Event, rctx *core.DiffractLLMContext) {
	switch m.config.PayloadMode {
	case config.PayloadModeFull:
	case config.PayloadModeErrorsOnly:
		if e.Outcome.Result == ResultOK || e.Outcome.Result == ResultClientAbort {
			return
		}
	default:
		return
	}

	limit := m.config.PayloadMaxKB << 10
	req, reqCut := clip(rctx.BodyBytes, limit)
	res, resCut := clip(rctx.ResponseBody, limit)
	if req == nil && res == nil {
		return
	}
	e.Payload = &Payload{ClientRequest: req, ClientResponse: res}
	e.PayloadState = PayloadReady
	if reqCut || resCut {
		e.PayloadState = PayloadTruncated
	}
}

func clip(b []byte, limit int) ([]byte, bool) {
	if len(b) == 0 {
		return nil, false
	}
	if len(b) > limit {
		return bytes.Clone(b[:limit]), true
	}
	return bytes.Clone(b), false
}
