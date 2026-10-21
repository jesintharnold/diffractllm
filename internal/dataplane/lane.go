package dataplane

import (
	"errors"
	"sync/atomic"

	config "diffractllm/configs"
	"diffractllm/internal/core"

	"go.uber.org/zap"
)

var errLaneFull = errors.New("lane full: provider at max_concurrency")

type lane struct {
	concurrencyLimit atomic.Int64
	inUse            atomic.Int64
}

func newLane(limit int) *lane {
	if limit <= 0 {
		return nil
	}
	l := &lane{}
	l.concurrencyLimit.Store(int64(limit))
	return l
}

func laneLimit(d config.UpstreamConfig, n core.NetworkConfig, p core.Provider, logger *zap.Logger) int {
	limit := compareConfig(d.MaxConcurrency, n.MaxConcurrency)
	if conns := compareConfig(d.MaxConnsPerHost, n.MaxConnsPerHost); conns > 0 && limit > conns {
		logger.Warn("max_concurrency capped at max_conns_per_host",
			zap.String("provider", string(p)), zap.Int("max_concurrency", limit), zap.Int("max_conns_per_host", conns))
		limit = conns
	}
	return limit
}

func laneFor(limit int, prev *lane) *lane {
	if limit <= 0 {
		return nil
	}
	if prev == nil {
		return newLane(limit)
	}
	prev.setConcurrency(limit)
	return prev
}

func (l *lane) acquire() error {
	if l == nil {
		return nil
	}
	for {
		n := l.inUse.Load()
		if n >= l.concurrencyLimit.Load() { 
			return errLaneFull
		}
		if l.inUse.CompareAndSwap(n, n+1) {
			return nil
		}
	}
}

func (l *lane) release() {
	if l != nil {
		l.inUse.Add(-1)
	}
}

func (l *lane) setConcurrency(limit int) { l.concurrencyLimit.Store(int64(limit)) }

func (l *lane) concurrency() int {
	if l == nil {
		return 0
	}
	return int(l.concurrencyLimit.Load())
}

func (l *lane) active() int {
	if l == nil {
		return 0
	}
	return int(l.inUse.Load())
}
