package governance

import (
	"diffractllm/internal/core"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type Budget struct {
	Config atomic.Pointer[core.Budget]

	// Everything spent this window, flushed or not. One counter, so a reader
	// cannot catch spend mid-move between two fields.
	WindowCost atomic.Int64
	WindowReqs atomic.Int64

	// What the database last accepted. Written by the settle goroutine only.
	LastFlushed atomic.Int64
	LastReqs    atomic.Int64
}

func (b *Budget) CheckBudgetUsage() bool {
	bc := b.Config.Load()
	if bc == nil || !bc.Enforce || bc.BudgetLimit == 0 {
		return true
	}
	// A stale window is not a free window. TrackBudgetWindow does the reset;
	// until it lands we stay strict rather than letting spend through.
	return b.WindowCost.Load() < bc.BudgetLimit
}

func (b *Budget) CheckBudgetWindow() bool {
	bc := b.Config.Load()
	if bc == nil || bc.BudgetParseDuration <= 0 {
		return false
	}
	return time.Since(bc.LastBudgetRefreshAt) >= bc.BudgetParseDuration
}

func (b *Budget) RecordUsage(spend int64) {
	b.WindowCost.Add(spend)
	b.WindowReqs.Add(1)
}

func budgetResetTarget(cfg *core.Budget, now time.Time) *time.Time {
	if cfg == nil || cfg.BudgetParseDuration <= 0 {
		return nil
	}
	elapsed := now.Sub(cfg.LastBudgetRefreshAt)
	if elapsed < cfg.BudgetParseDuration {
		return nil
	}

	// A gap of many windows collapses to one reset at the current boundary
	// rather than replaying one reset per window that went by.
	windows := int64(elapsed / cfg.BudgetParseDuration)
	target := cfg.LastBudgetRefreshAt.Add(time.Duration(windows) * cfg.BudgetParseDuration)
	return &target
}





type BudgetCache struct {
	BudgetMap sync.Map
	logger    *zap.Logger
}

func (bc *BudgetCache) LookupBudget(budgetID string) (*Budget, bool) {
	b, ok := bc.BudgetMap.Load(budgetID)
	if !ok {
		return nil, false
	}
	return b.(*Budget), true
}

func (bc *BudgetCache) LoadBudgets(budgetData []*core.Budget) {
	tempactiveIDs := make(map[string]struct{}, len(budgetData))
	for _, dbBudget := range budgetData {
		tempactiveIDs[dbBudget.ID] = struct{}{}
		bc.UpsertBudget(dbBudget)
	}

	bc.BudgetMap.Range(func(key, value any) bool {
		budgetID := key.(string)
		if _, exists := tempactiveIDs[budgetID]; !exists {
			bc.BudgetMap.Delete(budgetID)
			if bc.logger != nil {
				bc.logger.Info("Removed deleted budget from cache", zap.String("budget_id", budgetID))
			}
		}
		return true
	})

	bc.logger.Debug("Budget cache hot-swapped", zap.Int("keys", len(budgetData)))
}

func (bc *BudgetCache) UpsertBudget(budget *core.Budget) {
	if budget == nil {
		return
	}

	seeded := &Budget{}
	seeded.Config.Store(budget)
	seeded.WindowCost.Store(budget.TotalSpend)
	seeded.WindowReqs.Store(budget.RequestCount)
	seeded.LastFlushed.Store(budget.TotalSpend)
	seeded.LastReqs.Store(budget.RequestCount)
	if actual, loaded := bc.BudgetMap.LoadOrStore(budget.ID, seeded); loaded {
		actual.(*Budget).Config.Store(budget)
	}
}

func (bc *BudgetCache) DeleteBudget(budgetID string) {
	bc.BudgetMap.Delete(budgetID)
	if bc.logger != nil {
		bc.logger.Info("Removed deleted budget from cache", zap.String("budget_id", budgetID))
	}
}
