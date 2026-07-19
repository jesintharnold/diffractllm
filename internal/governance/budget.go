package governance

import (
	"diffractllm/internal/core"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type Budget struct {
	Config          atomic.Pointer[core.Budget]
	TotalCost       atomic.Int64
	RequestCount    atomic.Int64
	PendingCost     atomic.Int64 // This stores the total budget amount which are not written to the DB yet
	PendingRequests atomic.Int64 // This stores the request count which are not written to the DB yet
}

func (b *Budget) CheckBudgetUsage() bool {
	bc := b.Config.Load()
	if bc == nil || !bc.Enforce || bc.BudgetLimit == 0 {
		return true
	}
	if bc.BudgetParseDuration > 0 && time.Since(bc.LastBudgetRefreshAt) >= bc.BudgetParseDuration {
		return true
	}
	effectiveCost := b.TotalCost.Load() + b.PendingCost.Load()
	return effectiveCost < bc.BudgetLimit
}

func (b *Budget) CheckBudgetWindow() bool {
	bc := b.Config.Load()
	if bc == nil || bc.BudgetParseDuration <= 0 {
		return false
	}
	return time.Since(bc.LastBudgetRefreshAt) >= bc.BudgetParseDuration
}

func (b *Budget) RecordUsage(spend int64) {
	b.PendingCost.Add(spend)
	b.PendingRequests.Add(1)
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
		if existing, ok := bc.BudgetMap.Load(dbBudget.ID); ok {
			b := existing.(*Budget)
			b.Config.Store(dbBudget)
			b.RequestCount.Store(dbBudget.RequestCount)
			b.TotalCost.Store(dbBudget.TotalSpend)
		} else {
			newtempBudget := &Budget{}
			newtempBudget.Config.Store(dbBudget)
			newtempBudget.TotalCost.Store(dbBudget.TotalSpend)
			newtempBudget.RequestCount.Store(dbBudget.RequestCount)
			bc.BudgetMap.Store(dbBudget.ID, newtempBudget)
		}
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
	if existing, ok := bc.BudgetMap.Load(budget.ID); ok {
		b := existing.(*Budget)
		b.Config.Store(budget)
		b.RequestCount.Store(budget.RequestCount)
		b.TotalCost.Store(budget.TotalSpend)
	} else {
		newBudget := &Budget{}
		newBudget.Config.Store(budget)
		newBudget.TotalCost.Store(budget.TotalSpend)
		newBudget.RequestCount.Store(budget.RequestCount)
		actual, loaded := bc.BudgetMap.LoadOrStore(budget.ID, newBudget)
		if loaded {
			b := actual.(*Budget)
			b.Config.Store(budget)
			b.RequestCount.Store(budget.RequestCount)
			b.TotalCost.Store(budget.TotalSpend)
		}
	}
}

func (bc *BudgetCache) DeleteBudget(budgetID string) {
	bc.BudgetMap.Delete(budgetID)
	if bc.logger != nil {
		bc.logger.Info("Removed deleted budget from cache", zap.String("budget_id", budgetID))
	}
}
