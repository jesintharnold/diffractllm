package governance

import (
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
	Cost           int64 // Per request cost
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

func NewUsageBuffer(maxcapacity int, logger *zap.Logger) *UsageBuffer {
	if maxcapacity <= 0 {
		maxcapacity = DefaultUsageBufferCapacity
	}
	return &UsageBuffer{
		records:     make([]UsageRecord, 0, min(maxcapacity, 4096)),
		maxCapacity: maxcapacity,
		logger:      logger,
	}
}

func (ub *UsageBuffer) Append(r UsageRecord) {
	ub.lock.Lock()
	if len(ub.records) >= ub.maxCapacity {
		ub.lock.Unlock()
		dropped := ub.dropped.Add(1)
		ub.logger.Error("usage buffer at capacity â€” record dropped", zap.String("client_id", r.ClientID), zap.Int64("total_dropped", dropped), zap.Int("capacity", ub.maxCapacity))
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

func (ub *UsageBuffer) DroppedCount() int64 {
	return ub.dropped.Load()
}

