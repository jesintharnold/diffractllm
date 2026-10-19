package governance

import (
	"diffractllm/internal/observability/logging"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Usage Buffer struct for doing the batching architecture

type UsageRecord struct {
	ClientID       string
	BudgetID       string
	Backend        string
	ModelID        string
	ModelName      string
	InputTokens    int64
	OutputTokens   int64
	ResponseBytes  int
	ResponseStatus int
	Cost           int64 // Per request cost, nano-USD via core.ToNanoUSD
	RequestedAt    time.Time
}

const DefaultUsageBufferCapacity = 100_000

type UsageBuffer struct {
	lock        sync.Mutex
	records     []UsageRecord
	maxCapacity int
	logger      *zap.Logger
	dropped     atomic.Int64
}

func NewUsageBuffer(maxcapacity int) *UsageBuffer {
	if maxcapacity <= 0 {
		maxcapacity = DefaultUsageBufferCapacity
	}
	return &UsageBuffer{
		records:     make([]UsageRecord, 0, min(maxcapacity, 4096)),
		maxCapacity: maxcapacity,
		logger:      logging.For("governance"),
	}
}

func (ub *UsageBuffer) Append(r UsageRecord) {
	ub.lock.Lock()
	if len(ub.records) >= ub.maxCapacity {
		ub.lock.Unlock()
		// First drop and every 1000th; records_dropped in the job detail has the exact count.
		if dropped := ub.dropped.Add(1); dropped == 1 || dropped%1000 == 0 {
			ub.logger.Error("usage buffer full, records dropped", zap.Int64("total_dropped", dropped), zap.Int("capacity", ub.maxCapacity))
		}
		return
	}
	ub.records = append(ub.records, r)
	ub.lock.Unlock()
}

func (ub *UsageBuffer) Drain() []UsageRecord {
	ub.lock.Lock()
	out := ub.records
	ub.records = make([]UsageRecord, 0, cap(out))
	ub.lock.Unlock()
	return out
}

func (ub *UsageBuffer) Len() int {
	ub.lock.Lock()
	defer ub.lock.Unlock()
	return len(ub.records)
}

func (ub *UsageBuffer) DroppedCount() int64 {
	return ub.dropped.Load()
}
