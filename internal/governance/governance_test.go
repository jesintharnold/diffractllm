package governance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"diffractllm/internal/worker"
)

// ---------- fixtures owned by this file ----------
//
// Governance drives every cache and the store, so this file needs the whole
// package plus a real sqlite database. Run it with the package.

const (
	gModel  = "gpt-4o"
	gClient = "client-1"
)

func gptr[T any](v T) *T { return &v }

// NewStore is a sync.Once singleton, so one database serves the whole test
// binary. Each caller gets it empty except for the seeded provider rows.
func testStore(t *testing.T) *dbstore.Store {
	t.Helper()

	path := filepath.Join(os.TempDir(), "diffractllm_governance_test.db")
	store, err := dbstore.NewStore(path, strings.Repeat("g", 32), zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	require.NoError(t, store.Seed(false))

	for _, table := range []string{"usage_records", "virtual_keys", "budgets"} {
		require.NoError(t, store.DB.Exec("DELETE FROM "+table).Error)
	}
	return store
}

func testGovernance(t *testing.T, store *dbstore.Store) *Governance {
	t.Helper()
	g, err := NewGovernance(store, zap.NewNop())
	require.NoError(t, err)
	return g
}

// Inserts a budget row and returns its id.
func seedBudget(t *testing.T, store *dbstore.Store, limit int64) string {
	t.Helper()
	row, err := store.CreateBudget(core.Budget{
		Name: "test-budget", BudgetLimit: limit, BudgetDuration: "1D",
	})
	require.NoError(t, err)
	return row.ID
}

// Mints a virtual key row and returns its id and plaintext.
func seedVirtualKey(t *testing.T, store *dbstore.Store, budgetID string) (string, string) {
	t.Helper()
	created, plaintext, err := store.CreateVirtualKeyTx(&core.VirtualKeyRequest{
		ClientID: gClient, BudgetID: budgetID,
		Mode: gptr(core.VKDirect), LoadBalancer: gptr(core.LBRoundRobin),
		ProviderConfigs: []core.ProviderConfig{
			{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, plaintext)
	return created.VirtualKey.ID, plaintext
}

// Ages a cached budget's window so the next roll is due.
func expireWindow(t *testing.T, budget *Budget) {
	t.Helper()
	cfg := budget.Config.Load()
	require.NotNil(t, cfg)
	aged := *cfg
	aged.BudgetParseDuration = time.Hour
	aged.LastBudgetRefreshAt = time.Now().Add(-2 * time.Hour)
	budget.Config.Store(&aged)
}

func statByName(t *testing.T, stats []worker.JobStats, name string) worker.JobStats {
	t.Helper()
	for _, s := range stats {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no job named %q", name)
	return worker.JobStats{}
}

// ---------- NewGovernance ----------

func TestNewGovernanceDefaults(t *testing.T) {
	g := testGovernance(t, testStore(t))

	require.NotNil(t, g.KeyCache)
	require.NotNil(t, g.BudgetCache)
	require.NotNil(t, g.UsageBuffer)
	assert.Equal(t, DefaultUsageBufferCapacity, g.UsageBuffer.maxCapacity)

	for _, interval := range []time.Duration{
		g.config.vkeySyncInterval, g.config.budgetSyncInterval,
		g.config.usageFlushInterval, g.config.budgetFlushRollInterval,
	} {
		assert.Equal(t, 10*time.Second, interval)
	}

	assert.True(t, g.KeyCache.LastSync.IsZero(), "no sync has run yet")
	_, found := g.KeyCache.LookupVkey("dk-anything")
	assert.False(t, found)
}

// ---------- syncVirtualKey ----------

func TestSyncVirtualKeyRoundTrip(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000_000)

	keyID, plaintext := seedVirtualKey(t, store, budgetID)

	g := testGovernance(t, store)
	loaded, err := g.syncVirtualKey()
	require.NoError(t, err)
	assert.Equal(t, 1, loaded)

	// The cache is keyed by the plaintext key, not the id.
	vk, found := g.KeyCache.LookupVkey(plaintext)
	require.True(t, found, "a freshly minted key must be reachable by its plaintext")
	assert.Equal(t, keyID, vk.ID)
	assert.Equal(t, gClient, vk.ClientID)
	assert.Equal(t, budgetID, vk.BudgetID)

	_, found = g.KeyCache.LookupVkey(keyID)
	assert.False(t, found, "the id is not a cache key")
	assert.False(t, g.KeyCache.LastSync.IsZero())
}

func TestSyncVirtualKeyEmptyTable(t *testing.T) {
	g := testGovernance(t, testStore(t))

	loaded, err := g.syncVirtualKey()
	require.NoError(t, err)
	assert.Zero(t, loaded)
	assert.NotNil(t, g.KeyCache.virtual.Load(), "an empty table still publishes a snapshot")
}

// syncVirtualKey returns on the first bad row, so the previously published
// snapshot keeps serving rather than being replaced by a partial one.
func TestSyncVirtualKeyBadRowKeepsThePreviousSnapshot(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000_000)

	_, plaintext := seedVirtualKey(t, store, budgetID)

	g := testGovernance(t, store)
	_, err := g.syncVirtualKey()
	require.NoError(t, err)
	require.NotNil(t, mustLookup(t, g, plaintext))

	// Corrupt the stored provider configs so ToCore or Validate fails.
	require.NoError(t, store.DB.Exec(
		`UPDATE virtual_keys SET provider_configs = '[]'`).Error)

	_, err = g.syncVirtualKey()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync virtual key")

	assert.NotNil(t, mustLookup(t, g, plaintext),
		"a failed sync must not clear the cache")
}

func mustLookup(t *testing.T, g *Governance, key string) *core.VirtualKey {
	t.Helper()
	vk, found := g.KeyCache.LookupVkey(key)
	require.True(t, found)
	return vk
}

// ---------- syncBudget ----------

func TestSyncBudgetRoundTrip(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 5_000_000)

	g := testGovernance(t, store)
	loaded, err := g.syncBudget()
	require.NoError(t, err)
	assert.Equal(t, 1, loaded)

	budget, found := g.BudgetCache.LookupBudget(budgetID)
	require.True(t, found)

	cfg := budget.Config.Load()
	require.NotNil(t, cfg)
	assert.EqualValues(t, 5_000_000, cfg.BudgetLimit)
	assert.Positive(t, cfg.BudgetParseDuration, "the duration string must be parsed on the way in")
	require.NotNil(t, cfg.Enforce)
	assert.True(t, *cfg.Enforce, "enforce defaults to true in the store")

	assert.True(t, budget.CheckBudgetUsage(), "a fresh budget has room")
}

// A budget removed from the database is evicted on the next sync.
func TestSyncBudgetEvictsDeletedRows(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000)

	g := testGovernance(t, store)
	_, err := g.syncBudget()
	require.NoError(t, err)
	_, found := g.BudgetCache.LookupBudget(budgetID)
	require.True(t, found)

	require.NoError(t, store.DeleteBudget(budgetID))

	loaded, err := g.syncBudget()
	require.NoError(t, err)
	assert.Zero(t, loaded)

	_, found = g.BudgetCache.LookupBudget(budgetID)
	assert.False(t, found, "the cache must drop a budget the database no longer has")
}

// ---------- flushUsageHistory ----------

func TestFlushUsageHistoryEmptyBuffer(t *testing.T) {
	g := testGovernance(t, testStore(t))

	written, err := g.flushUsageHistory()
	require.NoError(t, err)
	assert.Zero(t, written)
}

func TestFlushUsageHistoryWritesRows(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000_000)
	g := testGovernance(t, store)

	now := time.Now().UTC()
	g.UsageBuffer.Append(UsageRecord{
		ClientID: gClient, BudgetID: budgetID, Backend: "openai",
		ModelID: "gpt-4o-2024-08-06", ModelName: gModel,
		InputTokens: 100, OutputTokens: 50,
		ResponseBytes: 2048, ResponseStatus: 200,
		Cost: 12_345, RequestedAt: now,
	})

	written, err := g.flushUsageHistory()
	require.NoError(t, err)
	assert.Equal(t, 1, written)
	assert.Zero(t, g.UsageBuffer.Len(), "a successful flush empties the buffer")

	var rows []dbstore.StoreUsageRecord
	require.NoError(t, store.DB.Find(&rows).Error)
	require.Len(t, rows, 1)

	row := rows[0]
	assert.Equal(t, gClient, row.ClientID)
	assert.Equal(t, budgetID, row.BudgetID)
	assert.Equal(t, "openai", row.Backend)
	assert.Equal(t, "gpt-4o-2024-08-06", row.ModelID)
	assert.EqualValues(t, 100, row.InputTokens)
	assert.EqualValues(t, 50, row.OutputTokens)
	assert.EqualValues(t, 150, row.TotalTokens, "total is derived, not carried")
	assert.EqualValues(t, 12_345, row.Cost)
	assert.NotEmpty(t, row.ID, "the flush mints a uuid per row")
	assert.False(t, row.FlushedAt.IsZero())
}

// A write failure puts every record back, so nothing is billed-but-unrecorded.
func TestFlushUsageHistoryReEnqueuesOnFailure(t *testing.T) {
	store := testStore(t)
	g := testGovernance(t, store)

	for i := range 3 {
		g.UsageBuffer.Append(UsageRecord{
			ClientID: gClient, BudgetID: "no-such-budget",
			ModelName: gModel, Cost: int64(i + 1), RequestedAt: time.Now(),
		})
	}
	require.Equal(t, 3, g.UsageBuffer.Len())

	// Force the insert to fail. testStore re-migrates for the next test.
	require.NoError(t, store.DB.Exec("DROP TABLE usage_records").Error)

	written, err := g.flushUsageHistory()
	require.Error(t, err)
	assert.Zero(t, written)
	assert.Equal(t, 3, g.UsageBuffer.Len(), "every record must be back in the buffer")

	require.NoError(t, store.Migrate())
	var count int64
	require.NoError(t, store.DB.Model(&dbstore.StoreUsageRecord{}).Count(&count).Error)
	assert.Zero(t, count, "nothing was written")
}

// ---------- flushBudgetUsage ----------

func TestFlushBudgetUsage(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000_000)
	g := testGovernance(t, store)
	_, err := g.syncBudget()
	require.NoError(t, err)

	t.Run("nothing spent writes nothing", func(t *testing.T) {
		assert.Zero(t, g.flushBudgetUsage())
	})

	budget, found := g.BudgetCache.LookupBudget(budgetID)
	require.True(t, found)
	budget.RecordUsage(7_500)

	t.Run("spend is written and marked flushed", func(t *testing.T) {
		assert.EqualValues(t, 1, g.flushBudgetUsage())
		assert.EqualValues(t, 7_500, budget.LastFlushed.Load())
		assert.EqualValues(t, 1, budget.LastReqs.Load())

		row, err := store.GetBudget(budgetID)
		require.NoError(t, err)
		assert.EqualValues(t, 7_500, row.TotalCost)
		assert.EqualValues(t, 1, row.RequestCount)
	})

	t.Run("a second flush with no new spend is a no-op", func(t *testing.T) {
		assert.Zero(t, g.flushBudgetUsage())
	})
}

// ---------- trackBudgetWindow ----------

func TestTrackBudgetWindowRollsAtTheBoundary(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000_000)
	g := testGovernance(t, store)
	_, err := g.syncBudget()
	require.NoError(t, err)

	budget, found := g.BudgetCache.LookupBudget(budgetID)
	require.True(t, found)
	budget.RecordUsage(9_000)

	// Not due yet.
	assert.Zero(t, g.trackBudgetWindow())
	assert.EqualValues(t, 9_000, budget.WindowCost.Load())

	expireWindow(t, budget)

	assert.EqualValues(t, 1, g.trackBudgetWindow())

	assert.Zero(t, budget.WindowCost.Load(), "the window resets")
	assert.Zero(t, budget.WindowReqs.Load())
	assert.Zero(t, budget.LastFlushed.Load())
	assert.Zero(t, budget.LastReqs.Load())

	rolled := budget.Config.Load()
	assert.Zero(t, rolled.TotalSpend)
	assert.Zero(t, rolled.RequestCount)
	assert.True(t, rolled.LastBudgetRefreshAt.After(time.Now().Add(-2*time.Hour)))

	// The roll flushes the closing total and then resets it, so the row ends at
	// zero. The flush is not redundant: if the reset write fails, the window's
	// final spend is already durable. The per-request history lives in
	// usage_records either way.
	row, err := store.GetBudget(budgetID)
	require.NoError(t, err)
	assert.Zero(t, row.TotalCost)
	assert.Zero(t, row.RequestCount)
	assert.Equal(t, rolled.LastBudgetRefreshAt.UTC(), row.LastBudgetRefreshAt.UTC(),
		"the cache and the row must agree on the new window start")
}

// The roll subtracts what it flushed instead of storing zero, so a request
// settling while the roll is in flight is not discarded. Run with -race.
func TestTrackBudgetWindowUnderConcurrentSpend(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000_000_000)
	g := testGovernance(t, store)
	_, err := g.syncBudget()
	require.NoError(t, err)

	budget, found := g.BudgetCache.LookupBudget(budgetID)
	require.True(t, found)

	expireWindow(t, budget)

	const writers, each = 8, 100
	done := make(chan struct{}, writers)
	for range writers {
		go func() {
			for range each {
				budget.RecordUsage(1)
			}
			done <- struct{}{}
		}()
	}

	rolled := g.trackBudgetWindow()

	for range writers {
		<-done
	}

	assert.EqualValues(t, 1, rolled)
	remaining := budget.WindowCost.Load()
	assert.GreaterOrEqual(t, remaining, int64(0),
		"subtracting what was flushed must never drive the counter negative")
	assert.LessOrEqual(t, remaining, int64(writers*each),
		"nothing beyond the concurrent spend can survive the roll")

	// Cost and count are deliberately NOT asserted equal. The roll loads them on
	// separate lines, so every RecordUsage completing between those two loads is
	// subtracted from one counter and not the other. The gap is bounded by
	// concurrency, not by one. It is tolerable because WindowCost is what gates
	// the budget; WindowReqs is a report.
	assert.GreaterOrEqual(t, budget.WindowReqs.Load(), int64(0))
}

// ---------- the worker group ----------

func TestStartRunsEveryJob(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000_000)
	g := testGovernance(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, g.Start(ctx))

	stats := g.Stats()
	require.Len(t, stats, 4)

	names := make([]string, 0, 4)
	for _, s := range stats {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"budget_flush_roll", "budget_sync", "usage_flush", "virtual_keys_sync"}, names,
		"Stats is sorted by name")

	// The two RunAtStart jobs ran synchronously inside Start.
	for _, name := range []string{"budget_sync", "virtual_keys_sync"} {
		s := statByName(t, stats, name)
		assert.False(t, s.LastSuccessAt.IsZero(), name)
		assert.Empty(t, s.LastError, name)
	}
	assert.EqualValues(t, 1, statByName(t, stats, "budget_sync").LastDetail["budgets_loaded"])

	_, found := g.BudgetCache.LookupBudget(budgetID)
	assert.True(t, found, "Start must leave the caches populated")

	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	require.NoError(t, g.Shutdown(shutdownCtx))
}

// usage_flush and budget_flush_roll carry RunAtStop, so a shutdown drains what
// is still buffered instead of losing it.
func TestShutdownDrainsTheUsageBuffer(t *testing.T) {
	store := testStore(t)
	budgetID := seedBudget(t, store, 1_000_000)
	g := testGovernance(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, g.Start(ctx))

	g.UsageBuffer.Append(UsageRecord{
		ClientID: gClient, BudgetID: budgetID, Backend: "openai",
		ModelName: gModel, InputTokens: 10, OutputTokens: 5,
		Cost: 999, RequestedAt: time.Now().UTC(),
	})
	require.Equal(t, 1, g.UsageBuffer.Len())

	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	require.NoError(t, g.Shutdown(shutdownCtx))

	assert.Zero(t, g.UsageBuffer.Len(), "RunAtStop must drain the buffer")

	var rows []dbstore.StoreUsageRecord
	require.NoError(t, store.DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.EqualValues(t, 999, rows[0].Cost)
}
