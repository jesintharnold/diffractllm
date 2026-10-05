package metricsengine

import (
	"bytes"
	"context"
	"fmt"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/worker"

	"go.uber.org/zap"
)

const (
	jobEventsFlush   = "events_flush"
	maxWriteAttempts = 3
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

func NewMetricsEngine(store OLAPStore, cfg *config.MetricsEngineConfig, logger *zap.Logger) *MetricsEngine {
	log := logger.With(zap.String("component", "metrics_engine"))
	return &MetricsEngine{
		store:  store,
		buffer: NewEventsBuffer(cfg.BufferCapacity),
		logger: log,
		config: cfg,
	}
}

func (m *MetricsEngine) Start(ctx context.Context) error {
	m.workers = worker.NewGroup("metrics", m.logger)
	if err := m.workers.Add(&worker.Job{
		Name:      jobEventsFlush,
		Interval:  m.config.FlushInterval,
		RunAtStop: true,
		Run:       m.flush,
	}); err != nil {
		return err
	}
	return m.workers.Start(ctx)
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
	if !m.buffer.Append(e) {
		releaseEvent(e)
		return
	}
	if m.workers != nil && m.buffer.Len() >= m.buffer.maxCapacity/2 {
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
	case r > 31*day:
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

func (m *MetricsEngine) GetRequestSummaryByTime(ctx context.Context, from, to time.Time) (*TimeSummary, error) {
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetRequestSummaryByTime(ctx, from.UTC(), to.UTC(), bucketFor(to.Sub(from)))
}

func (m *MetricsEngine) GetRequestLogsByTime(ctx context.Context, from, to time.Time, offset, limit int) (*RequestLogPage, error) {
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	if offset < 0 || limit < 1 || limit > m.config.MaxPage {
		return nil, fmt.Errorf("%w: need offset >= 0 and 1 <= limit <= %d", ErrInvalidQuery, m.config.MaxPage)
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetRequestLogsByTime(ctx, from.UTC(), to.UTC(), offset, limit)
}

func (m *MetricsEngine) GetRequestDetailByID(ctx context.Context, id string) (*RequestDetail, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidQuery)
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetRequestDetailByID(ctx, id)
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

func (m *MetricsEngine) GetBudgetSpendByTime(ctx context.Context, budgetID string, from, to time.Time) (*BudgetSpend, error) {
	if budgetID == "" {
		return nil, fmt.Errorf("%w: budget id is required", ErrInvalidQuery)
	}
	if err := m.checkRange(from, to); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()
	return m.store.GetBudgetSpendByTime(ctx, budgetID, from.UTC(), to.UTC(), bucketFor(to.Sub(from)))
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
