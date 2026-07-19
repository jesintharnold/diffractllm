package governance

import (
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Governance struct {
	Store        *dbstore.Store
	KeyCache     *VirtualkeyCache
	BudgetCache  *BudgetCache
	UsageBuffer  *UsageBuffer
	PricingCache *PricingCache
	logger       *zap.Logger
}

func NewGovernance(store *dbstore.Store, logger *zap.Logger) (*Governance, error) {
	keyCache := &VirtualkeyCache{logger: logger}
	budgetCache := &BudgetCache{logger: logger}
	usageBuffer := NewUsageBuffer(0, logger)
	priceCache := NewPricingCache(logger)
	g := &Governance{
		Store:        store,
		KeyCache:     keyCache,
		BudgetCache:  budgetCache,
		UsageBuffer:  usageBuffer,
		PricingCache: priceCache,
		logger:       logger,
	}
	return g, nil
}

func (g *Governance) InitGovernance() error {

	// budgets, err := g.Store.ListBudgets()
	// if err != nil {
	// 	return fmt.Errorf("load budgets: %w", err)
	// }

	return g.SyncVirtualKey()
}

func (g *Governance) SyncCustomPrice() error {
	start := time.Now()
	g.logger.Debug("custom pricing sync started")

	customprice, err := g.Store.ListCustomPricing()
	if err != nil {
		return fmt.Errorf("sync custom pricing: %w", err)
	}
	tempCustomprice := make([]*core.CustomPricing, 0, len(customprice))
	for i := range customprice {
		tempCustomprice = append(tempCustomprice, customprice[i].ToCore())
	}
	g.PricingCache.LoadCustomPricing(tempCustomprice)

	g.logger.Debug("custom pricing sync finished", zap.Int("rows_loaded", len(tempCustomprice)), zap.Duration("took", time.Since(start)))
	return nil
}

func (g *Governance) SyncBasePrice() error {
	start := time.Now()
	g.logger.Debug("base pricing sync started")

	baseprice, err := g.Store.ListBasePricing()
	if err != nil {
		return fmt.Errorf("sync base pricing: %w", err)
	}
	tempBaseprice := make([]*core.BasePricing, 0, len(baseprice))
	for i := range baseprice {
		tempBaseprice = append(tempBaseprice, baseprice[i].ToCore())
	}
	g.PricingCache.LoadBasePricing(tempBaseprice)

	g.logger.Debug("base pricing sync finished", zap.Int("rows_loaded", len(tempBaseprice)), zap.Duration("took", time.Since(start)))
	return nil
}

func (g *Governance) SyncVirtualKey() error {
	start := time.Now()
	g.logger.Debug("virtual key sync started")
	vkeydetail, err := g.Store.ListVirtualKeys()
	if err != nil {
		return fmt.Errorf("sync virtual keys: %w", err)
	}

	tempVkey := make([]*core.VirtualKey, 0, len(vkeydetail))
	for i := range vkeydetail {
		tempVkey = append(tempVkey, vkeydetail[i].ToCore())
	}
	g.KeyCache.LoadVirtualKeys(tempVkey)

	g.logger.Debug("virtual key sync finished",
		zap.Int("rows_loaded", len(tempVkey)),
		zap.Duration("took", time.Since(start)),
	)
	return nil
}

func (g *Governance) SyncBudget() error {
	start := time.Now()
	g.logger.Debug("budget sync started")

	db_budget, err := g.Store.ListBudgets()
	if err != nil {
		return fmt.Errorf("sync budget : %w", err)
	}
	tempBudget := make([]*core.Budget, 0, len(db_budget))
	for i := range db_budget {
		tempBudget = append(tempBudget, db_budget[i].ToCore())
	}
	g.BudgetCache.LoadBudgets(tempBudget)

	g.logger.Debug("budget sync finished", zap.Int("rows_loaded", len(db_budget)), zap.Duration("took", time.Since(start)))
	return nil
}

// Responsible for hisory drain to the DB
func (g *Governance) flushUsageHistory() {
	records := g.UsageBuffer.Drain()
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
	if err := g.Store.BulkInsertUsageHistory(storeRecords); err != nil {
		g.logger.Error("usage flush failed â€” re-enqueueing", zap.Int("count", len(records)), zap.Error(err))
		for _, r := range records {
			g.UsageBuffer.Append(r)
		}
		return
	}
	g.logger.Debug("usage flushed", zap.Int("count", len(records)))
}

// Responsible for Budget windows for all
func (g *Governance) flushBudgetUsage() {
	g.BudgetCache.BudgetMap.Range(func(key, value any) bool {
		b := value.(*Budget)
		pendingCost := b.PendingCost.Swap(0)
		pendingReq := b.PendingRequests.Swap(0)
		cfg := b.Config.Load()

		if pendingCost > 0 || pendingReq > 0 {
			err := g.Store.FlushBudgetUsage(cfg.ID, pendingCost, pendingReq)

			if err != nil {
				g.logger.Error("Failed to flush budget usage", zap.Error(err), zap.String("budget_id", cfg.ID))
				b.PendingCost.Add(pendingCost)
				b.PendingRequests.Add(pendingReq)
			} else {
				b.TotalCost.Add(pendingCost)
				b.RequestCount.Add(pendingReq)
			}
		}
		return true
	})
}

func (g *Governance) trackBudgetWindow() {
	now := time.Now()
	g.BudgetCache.BudgetMap.Range(func(key, value any) bool {
		b := value.(*Budget)
		cfg := b.Config.Load()
		if cfg == nil || cfg.BudgetParseDuration <= 0 {
			return true
		}

		if now.Sub(cfg.LastBudgetRefreshAt) < cfg.BudgetParseDuration {
			return true
		}
		newCfg := *cfg
		newCfg.LastBudgetRefreshAt = now
		newCfg.TotalSpend = 0
		newCfg.RequestCount = 0
		b.Config.Store(&newCfg)
		b.TotalCost.Store(0)
		b.RequestCount.Store(0)

		if err := g.Store.ResetBudgetWindow(cfg.ID, now); err != nil {
			g.logger.Error("budget window reset DB write failed, restoring counters",
				zap.String("budget_id", cfg.ID), zap.Error(err))
		}
		return true
	})

}
