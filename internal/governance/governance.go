package governance

import (
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type Governance struct {
	Store       *dbstore.Store
	KeyCache    *VirtualkeyCache
	BudgetCache *BudgetCache
	UsageBuffer *UsageBuffer
	Syncer      *Syncer
}

func NewGovernance(store *dbstore.Store, logger *zap.Logger) (*Governance, error) {
	keyCache := &VirtualkeyCache{logger: logger}
	budgetCache := &BudgetCache{logger: logger}
	usageBuffer := NewUsageBuffer(0, logger)
	syncer := NewSyncer(store, budgetCache, usageBuffer, 30*time.Second, logger)
	g := &Governance{
		Store:       store,
		KeyCache:    keyCache,
		BudgetCache: budgetCache,
		UsageBuffer: usageBuffer,
		Syncer:      syncer,
	}
	return g, nil
}

func (g *Governance) InitGovernance() error {

	budgets, err := g.Store.ListBudgets()
	if err != nil {
		return fmt.Errorf("load budgets: %w", err)
	}

	for _, b := range budgets {
		budget := &Budget{
			BudgetID:          b.ID,
			BudgetLimit:       b.BudgetLimit,
			EnforceBudget:     b.Enforce,
			BudgetDuration:    b.BudgetDuration,
			NextBudgetResetAt: b.BudgetRefreshAt,
		}

		budget.TotalCost.Store(b.TotalSpend)
		budget.RequestCount.Store(b.RequestCount)
		g.BudgetCache.Set(budget)
	}

	vkeyDetail, err := g.Store.ListVirtualKeys()
	if err != nil {
		return fmt.Errorf("load virtual keys: %w", err)
	}

	vkeymap := make(VirtualKeyMap, len(vkeyDetail))
	for _, key := range vkeyDetail {
		vkeymap[key.KeyHash] = &VirtualKey{
			Key:           key.APIKey,
			ClientID:      key.ClientID,
			BudgetID:      key.BudgetID,
			Mode:          core.ParseVKMode(key.Mode),
			AllowedModels: key.ToModelKeySet(),
			ModelPools:    key.ToPoolNameSet(),
			IsActive:      key.IsActive,
			ExpiresAt:     key.ExpiresAt,
		}
	}
	g.KeyCache.Swap(vkeymap)
	return nil
}
