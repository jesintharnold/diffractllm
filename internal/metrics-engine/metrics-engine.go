package metricsengine

import (
	"context"
	"fmt"

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
	store   OLAPStore
	buffer  *EventsBuffer
	workers *worker.Group
	logger  *zap.Logger
	config  *config.MetricsEngineConfig
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
