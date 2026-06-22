package governance

import (
	"diffractllm/internal/dbstore"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Syncer struct {
	DBStore       *dbstore.Store
	budgetCache   *BudgetCache
	UsageBuffer   *UsageBuffer
	logger        *zap.Logger
	stopChan      chan struct{}
	stopOnce      sync.Once
	wg            sync.WaitGroup
	flushInterval time.Duration
}

func NewSyncer(store *dbstore.Store, bc *BudgetCache, ub *UsageBuffer, interval time.Duration, logger *zap.Logger) *Syncer {
	return &Syncer{
		DBStore:       store,
		budgetCache:   bc,
		UsageBuffer:   ub,
		flushInterval: interval,
		stopChan:      make(chan struct{}),
		logger:        logger,
	}
}

func (s *Syncer) Start() {
	s.wg.Add(1)
	go s.flushUsageLoop()
}

func (s *Syncer) Stop() {
	s.stopOnce.Do(func() { close(s.stopChan) })
	s.wg.Wait()
	s.flushUsageHistory()
}

func (s *Syncer) flushUsageLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.flushUsageHistory()
			s.flushBudgetDeltas()
			if dropped := s.UsageBuffer.DroppedCount(); dropped > 0 {
				s.logger.Error("usage records dropped due to buffer capacity", zap.Int64("cumulative_dropped", dropped))
			}

		case <-s.stopChan:
			return
		}
	}

}

func (s *Syncer) flushUsageHistory() {
	records := s.UsageBuffer.Drain()
	if len(records) == 0 {
		return
	}
	storeRecords := make([]dbstore.StoreUsageRecord, len(records))
	for index, record := range records {
		storeRecords[index] = dbstore.StoreUsageRecord{
			ID:             uuid.Must(uuid.NewV7()).String(),
			ClientID:       record.ClientID,
			BudgetID:       record.BudgetID,
			Backend:        record.Backend,
			ModelID:        record.ModelID,
			ModelName:      record.ModelName,
			InputTokens:    record.InputTokens,
			OutputTokens:   record.OutputTokens,
			TotalTokens:    record.InputTokens + record.OutputTokens,
			ResponseStatus: record.ResponseStatus,
			ResponseBytes:  record.ResponseBytes,
			Cost:           record.Cost,
			RequestAt:      record.RequestedAt,
			FlushedAt:      time.Now(),
		}
	}

	if err := s.DBStore.BulkInsertUsageHistory(storeRecords); err != nil {
		s.logger.Error("usage flush failed â€” re-enqueueing", zap.Int("count", len(records)), zap.Error(err))
		for _, r := range records {
			s.UsageBuffer.Append(r)
		}
		return
	}
	s.logger.Debug("usage flushed", zap.Int("count", len(records)))
}

func (s *Syncer) flushBudgetDeltas() {
	s.budgetCache.BudgetMap.Range(func(key, value any) bool {
		b := value.(*Budget)
		pendingCost := b.PendingCost.Swap(0)
		pendingReq := b.PendingRequests.Swap(0)

		var nextResetAt time.Time

		if b.BudgetDuration > 0 {
			b.resetLock.Lock()
			nextResetAt = b.NextBudgetResetAt
			b.resetLock.Unlock()
		}

		if pendingCost == 0 && pendingReq == 0 && nextResetAt.IsZero() {
			return true
		}

		if err := s.DBStore.FlushBudgetUsage(b.BudgetID, pendingCost, pendingReq, nextResetAt); err != nil {
			s.logger.Error("budget delta flush failed â€” restoring delta", zap.String("budget_id", b.BudgetID), zap.Error(err))
			b.PendingCost.Add(pendingCost)
			b.PendingRequests.Add(pendingReq)
		}
		return true
	})
}
