package governance

import (
	"context"
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"diffractllm/internal/observability/logging"
	"diffractllm/internal/worker"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	JobVirtualKeysSync = "virtual_keys_sync"
	JobBudgetSync      = "budget_sync"
	JobUsageFlush      = "usage_flush"
	JobBudgetFlushRoll = "budget_flush_roll"
)

var ErrNotRunning = errors.New("governance workers are not running")

type jobsIntervalconfig struct {
	vkeySyncInterval        time.Duration
	budgetSyncInterval      time.Duration
	usageFlushInterval      time.Duration
	budgetFlushRollInterval time.Duration
}

type Governance struct {
	Store         *dbstore.Store
	KeyCache      *VirtualkeyCache
	BudgetCache   *BudgetCache
	UsageBuffer   *UsageBuffer
	logger        *zap.Logger
	workers       *worker.Group
	config        jobsIntervalconfig
	logUnreadable map[string]struct{} // keys already reported; only the sync job touches it
}

func NewGovernance(store *dbstore.Store, running Settings) (*Governance, error) {
	if err := running.Validate(); err != nil {
		return nil, fmt.Errorf("governance settings: %w", err)
	}
	logger := logging.For("governance")

	keyCache := &VirtualkeyCache{}
	budgetCache := &BudgetCache{logger: logger}
	usageBuffer := NewUsageBuffer(int(running.UsageBufferSize))
	g := &Governance{
		Store:         store,
		KeyCache:      keyCache,
		BudgetCache:   budgetCache,
		UsageBuffer:   usageBuffer,
		logger:        logger,
		logUnreadable: map[string]struct{}{},
		config: jobsIntervalconfig{
			vkeySyncInterval:        time.Duration(running.VKeySyncSeconds) * time.Second,
			budgetSyncInterval:      time.Duration(running.BudgetSyncSeconds) * time.Second,
			usageFlushInterval:      time.Duration(running.UsageFlushSeconds) * time.Second,
			budgetFlushRollInterval: time.Duration(running.BudgetFlushSeconds) * time.Second,
		},
	}
	return g, nil
}

func (g *Governance) Start(ctx context.Context) error {
	g.workers = worker.NewGroup("governance")
	err := g.workers.Add(
		&worker.Job{
			Name:       JobBudgetSync,
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
			Name:       JobVirtualKeysSync,
			Interval:   g.config.vkeySyncInterval,
			RunAtStart: true,
			Run: func(ctx context.Context) (worker.Detail, error) {
				loaded, skipped, err := g.syncVirtualKey()
				if err != nil {
					return nil, err
				}
				return worker.Detail{"virtual_keys_loaded": loaded, "virtual_keys_skipped": skipped}, nil
			},
		},

		&worker.Job{
			Name:      JobUsageFlush,
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
			Name:      JobBudgetFlushRoll,
			Interval:  g.config.budgetFlushRollInterval,
			RunAtStop: true,
			Run: func(ctx context.Context) (worker.Detail, error) {
				flushed, flushErr := g.flushBudgetUsage()
				rolled, rollErr := g.trackBudgetWindow()
				return worker.Detail{"budgets_flushed": flushed, "windows_rolled": rolled}, errors.Join(flushErr, rollErr)
			},
		},
	)
	if err != nil {
		return err
	}
	return g.workers.Start(ctx)
}

func (g *Governance) Shutdown(ctx context.Context) error { return g.workers.Shutdown(ctx) }

func (g *Governance) RunNow(job string) error {
	if g == nil || g.workers == nil {
		return ErrNotRunning
	}
	return g.workers.Trigger(job)
}

func (g *Governance) Ready() bool {
	return g != nil && g.KeyCache.Loaded() && g.BudgetCache != nil
}

func (g *Governance) Stats() []worker.JobStats {
	if g == nil || g.workers == nil {
		return nil
	}
	return g.workers.Stats()
}

func (g *Governance) syncVirtualKey() (loaded, skipped int, err error) {
	rows, unreadable, err := g.Store.ListReadableVirtualKeys()
	if err != nil {
		return 0, 0, fmt.Errorf("sync virtual keys: %w", err)
	}
	for _, id := range unreadable {
		g.logUnreadableKey(id, logErrCannotDecrypt)
	}

	tempVkey := make([]*core.VirtualKey, 0, len(rows))
	for i := range rows {
		virtualKey, err := rows[i].ToCore()
		if err == nil {
			err = virtualKey.Validate()
		}
		if err != nil {
			g.logUnreadableKey(rows[i].ID, err)
			unreadable = append(unreadable, rows[i].ID)
			continue
		}
		tempVkey = append(tempVkey, virtualKey)
	}
	if len(tempVkey) == 0 && len(unreadable) > 0 {
		return 0, len(unreadable), fmt.Errorf("sync virtual keys: none of the %d keys can be read", len(unreadable))
	}
	g.KeyCache.LoadVirtualKeys(tempVkey)
	g.logForgetReadableKeys(tempVkey)
	return len(tempVkey), len(unreadable), nil
}

var logErrCannotDecrypt = errors.New("cannot decrypt the key; check server.aes_pass_key")

// logUnreadableKey logs a bad key once at Error; later ticks stay at Debug until it reads again.
func (g *Governance) logUnreadableKey(id string, err error) {
	if _, seen := g.logUnreadable[id]; seen {
		g.logger.Debug("virtual key still unreadable", zap.String("virtual_key_id", id), zap.Error(err))
		return
	}
	g.logUnreadable[id] = struct{}{}
	g.logger.Error("virtual key unreadable, skipped", zap.String("virtual_key_id", id), zap.Error(err))
}

func (g *Governance) logForgetReadableKeys(loaded []*core.VirtualKey) {
	for _, vk := range loaded {
		delete(g.logUnreadable, vk.ID)
	}
}

func (g *Governance) syncBudget() (int, error) {
	db_budget, err := g.Store.ListBudgets()
	if err != nil {
		return 0, fmt.Errorf("sync budget : %w", err)
	}
	tempBudget := make([]*core.Budget, 0, len(db_budget))
	for i := range db_budget {
		tempBudget = append(tempBudget, db_budget[i].ToCore())
	}
	g.BudgetCache.LoadBudgets(tempBudget)
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
		for _, r := range records {
			g.UsageBuffer.Append(r)
		}
		return 0, fmt.Errorf("usage flush failed, %d records re-queued: %w", len(records), err)
	}
	return len(records), nil
}

// Responsible for Budget windows for all
func (g *Governance) flushBudgetUsage() (int64, error) {
	var flushed int64
	var errs []error
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
			errs = append(errs, fmt.Errorf("flush budget %s: %w", cfg.ID, err))
			return true
		}
		b.LastFlushed.Store(cost)
		b.LastReqs.Store(reqs)
		flushed++
		return true
	})
	return flushed, errors.Join(errs...)
}

// trackBudgetWindow rolls due windows; a failed write is retried next tick.
func (g *Governance) trackBudgetWindow() (int64, error) {
	now := time.Now()
	var rolled int64
	var errs []error
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
			errs = append(errs, fmt.Errorf("close window of budget %s: %w", cfg.ID, err))
			return true
		}

		if err := g.Store.ResetBudgetWindow(cfg.ID, *target); err != nil {
			errs = append(errs, fmt.Errorf("reset window of budget %s: %w", cfg.ID, err))
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
	return rolled, errors.Join(errs...)
}
