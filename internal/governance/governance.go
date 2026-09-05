package governance

import (
	"context"
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"diffractllm/internal/worker"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type jobsIntervalconfig struct {
	vkeySyncInterval        time.Duration
	budgetSyncInterval      time.Duration
	usageFlushInterval      time.Duration
	budgetFlushRollInterval time.Duration
}

type Governance struct {
	Store       *dbstore.Store
	KeyCache    *VirtualkeyCache
	BudgetCache *BudgetCache
	UsageBuffer *UsageBuffer
	logger      *zap.Logger
	workers     *worker.Group
	config      jobsIntervalconfig
}

func NewGovernance(store *dbstore.Store, logger *zap.Logger) (*Governance, error) {
	keyCache := &VirtualkeyCache{logger: logger}
	budgetCache := &BudgetCache{logger: logger}
	usageBuffer := NewUsageBuffer(0, logger)
	g := &Governance{
		Store:       store,
		KeyCache:    keyCache,
		BudgetCache: budgetCache,
		UsageBuffer: usageBuffer,
		logger:      logger,
		config: jobsIntervalconfig{
			vkeySyncInterval:        10 * time.Second,
			budgetSyncInterval:      10 * time.Second,
			usageFlushInterval:      10 * time.Second,
			budgetFlushRollInterval: 10 * time.Second,
		},
	}
	return g, nil
}

func (g *Governance) Start(ctx context.Context) error {
	g.workers = worker.NewGroup("governance", g.logger)
	err := g.workers.Add(
		&worker.Job{
			Name:       "budget_sync",
			Interval:   g.config.budgetSyncInterval,
			RunAtStart: true,
			Run: func(ctx context.Context) (worker.Detail, error) {
				loaded, err := g.syncBudget()
				if err != nil {
					return nil, err
				}
				return worker.Detail{"budgets_loaded": loaded}, nil
			},
		},

		&worker.Job{
			Name:       "virtual_keys_sync",
			Interval:   g.config.vkeySyncInterval,
			RunAtStart: true,
			Run: func(ctx context.Context) (worker.Detail, error) {
				loaded, err := g.syncVirtualKey()
				if err != nil {
					return nil, err
				}
				return worker.Detail{"virtual_keys_loaded": loaded}, nil
			},
		},

		&worker.Job{
			Name:      "usage_flush",
			Interval:  g.config.usageFlushInterval,
			RunAtStop: true,
			Run: func(ctx context.Context) (worker.Detail, error) {
				written, err := g.flushUsageHistory()
				return worker.Detail{
					"records_written": written,
					"records_pending": g.UsageBuffer.Len(),
					"records_dropped": g.UsageBuffer.DroppedCount(),
				}, err
			},
		},

		&worker.Job{
			Name:      "budget_flush_roll",
			Interval:  g.config.budgetFlushRollInterval,
			RunAtStop: true,
			Run: func(ctx context.Context) (worker.Detail, error) {
				flushed := g.flushBudgetUsage()
				rolled := g.trackBudgetWindow()
				return worker.Detail{"budgets_flushed": flushed, "windows_rolled": rolled}, nil
			},
		},
	)
	if err != nil {
		return err
	}
	return g.workers.Start(ctx)
}

func (g *Governance) syncVirtualKey() (int, error) {
	start := time.Now()
	g.logger.Debug("virtual key sync started")
	vkeydetail, err := g.Store.ListVirtualKeys()
	if err != nil {
		return 0, fmt.Errorf("sync virtual keys: %w", err)
	}

	tempVkey := make([]*core.VirtualKey, 0, len(vkeydetail))
	for i := range vkeydetail {
		virtualKey, err := vkeydetail[i].ToCore()
		if err != nil {
			return 0, fmt.Errorf("sync virtual key %q: %w", vkeydetail[i].ID, err)
		}
		if err := virtualKey.Validate(); err != nil {
			return 0, fmt.Errorf("sync virtual key %q: %w", vkeydetail[i].ID, err)
		}
		tempVkey = append(tempVkey, virtualKey)
	}
	g.KeyCache.LoadVirtualKeys(tempVkey)

	g.logger.Debug("virtual key sync finished", zap.Int("rows_loaded", len(tempVkey)), zap.Duration("took", time.Since(start)))
	return len(tempVkey), nil
}

func (g *Governance) syncBudget() (int, error) {
	start := time.Now()
	g.logger.Debug("budget sync started")

	db_budget, err := g.Store.ListBudgets()
	if err != nil {
		return 0, fmt.Errorf("sync budget : %w", err)
	}
	tempBudget := make([]*core.Budget, 0, len(db_budget))
	for i := range db_budget {
		tempBudget = append(tempBudget, db_budget[i].ToCore())
	}
	g.BudgetCache.LoadBudgets(tempBudget)

	g.logger.Debug("budget sync finished", zap.Int("rows_loaded", len(db_budget)), zap.Duration("took", time.Since(start)))
	return len(tempBudget), nil
}

// Responsible for hisory drain to the DB
func (g *Governance) flushUsageHistory() (int, error) {
	records := g.UsageBuffer.Drain()
	if len(records) == 0 {
		return 0, nil
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
		return 0, err
	}
	g.logger.Debug("usage flushed", zap.Int("count", len(records)))
	return len(records), nil
}

// Responsible for Budget windows for all
func (g *Governance) flushBudgetUsage() int64 {
	var flushed int64
	g.BudgetCache.BudgetMap.Range(func(key, value any) bool {
		b := value.(*Budget)
		cfg := b.Config.Load()
		if cfg == nil {
			return true
		}
		cost := b.WindowCost.Load()
		reqs := b.WindowReqs.Load()
		if cost == b.LastFlushed.Load() && reqs == b.LastReqs.Load() {
			return true
		}

		if err := g.Store.FlushBudgetUsage(cfg.ID, cost, reqs); err != nil {
			// LastFlushed stays behind, so the next tick rewrites the same value.
			g.logger.Error("Failed to flush budget usage", zap.Error(err), zap.String("budget_id", cfg.ID))
			return true
		}
		b.LastFlushed.Store(cost)
		b.LastReqs.Store(reqs)
		flushed++
		return true
	})
	return flushed
}

func (g *Governance) trackBudgetWindow() int64 {
	now := time.Now()
	var rolled int64
	g.BudgetCache.BudgetMap.Range(func(key, value any) bool {
		b := value.(*Budget)
		cfg := b.Config.Load()
		target := budgetResetTarget(cfg, now)
		if target == nil {
			return true
		}

		// Close the old window at its final total before resetting.
		cost := b.WindowCost.Load()
		reqs := b.WindowReqs.Load()
		if err := g.Store.FlushBudgetUsage(cfg.ID, cost, reqs); err != nil {
			g.logger.Error("closing window flush failed, will retry next tick",
				zap.String("budget_id", cfg.ID), zap.Error(err))
			return true
		}

		if err := g.Store.ResetBudgetWindow(cfg.ID, *target); err != nil {
			g.logger.Error("budget window reset DB write failed, will retry next tick",
				zap.String("budget_id", cfg.ID), zap.Error(err))
			return true
		}

		newCfg := *cfg
		newCfg.LastBudgetRefreshAt = *target
		newCfg.TotalSpend = 0
		newCfg.RequestCount = 0
		b.Config.Store(&newCfg)

		b.WindowCost.Add(-cost)
		b.WindowReqs.Add(-reqs)
		b.LastFlushed.Store(0)
		b.LastReqs.Store(0)
		rolled++
		return true
	})
	return rolled
}
