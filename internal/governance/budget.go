package governance

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type Budget struct {
	BudgetID          string
	BudgetLimit       int64
	EnforceBudget     bool
	BudgetDuration    int64 // if 0 no limit , set as seconds
	TotalCost         atomic.Int64
	RequestCount      atomic.Int64
	PendingCost       atomic.Int64 // This stores the total budget amount which are not written to the DB yet
	PendingRequests   atomic.Int64 // This stores the request count which are not written to the DB yet
	NextBudgetResetAt time.Time
	resetLock         sync.Mutex
}

func (b *Budget) ResetWindow() {
	if b.BudgetDuration == 0 || b.NextBudgetResetAt.IsZero() {
		return
	}
	b.resetLock.Lock()
	defer b.resetLock.Unlock()
	if time.Now().Before(b.NextBudgetResetAt) {
		return
	}
	b.TotalCost.Store(0)
	b.RequestCount.Store(0)
	b.PendingCost.Store(0)
	b.PendingRequests.Store(0)
	b.NextBudgetResetAt = time.Now().Add(time.Duration(b.BudgetDuration) * time.Second) // since it is seconds here given as expiry - BudgetDuration * time.Seconds
}

func (b *Budget) IsOverBudget() bool {
	b.ResetWindow()
	if !b.EnforceBudget {
		return false
	}

	if b.BudgetLimit == 0 {
		return false
	}

	return b.TotalCost.Load() >= b.BudgetLimit
}

func (b *Budget) RecordUsage(spend int64) {
	b.ResetWindow()

	b.TotalCost.Add(spend)
	b.RequestCount.Add(1)

	b.PendingCost.Add(spend)
	b.PendingRequests.Add(1)
}

type BudgetCache struct {
	BudgetMap sync.Map
	logger    *zap.Logger
}

func (bc *BudgetCache) Get(budgetID string) (*Budget, bool) {
	b, ok := bc.BudgetMap.Load(budgetID)
	if !ok {
		return nil, false
	}
	return b.(*Budget), true
}

func (bc *BudgetCache) Set(budget *Budget) {
	bc.BudgetMap.Store(budget.BudgetID, budget)
}

func (bc *BudgetCache) Delete(budgetID string) error {
	_, ok := bc.BudgetMap.Load(budgetID)
	if !ok {
		return fmt.Errorf("budget %s not found in cache", budgetID)
	}
	bc.BudgetMap.Delete(budgetID)
	return nil
}
