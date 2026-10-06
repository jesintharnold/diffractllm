package governance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"diffractllm/internal/core"
)

// ---------- fixtures owned by this file ----------

func bptr[T any](v T) *T { return &v }

// A config with an open window and a limit that is not reached.
func bCfg(id string, limit int64, opts ...func(*core.Budget)) *core.Budget {
	b := &core.Budget{
		ID: id, Name: id,
		BudgetLimit: limit, BudgetUnit: "nano_usd", BudgetDuration: "1D",
		BudgetParseDuration: 24 * time.Hour,
		LastBudgetRefreshAt: time.Now().UTC(),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func bLive(cfg *core.Budget) *Budget {
	b := &Budget{}
	b.Config.Store(cfg)
	return b
}

func bCache(t *testing.T, budgets ...*core.Budget) *BudgetCache {
	t.Helper()
	bc := &BudgetCache{logger: zap.NewNop()}
	for _, cfg := range budgets {
		bc.UpsertBudget(cfg)
	}
	return bc
}

// ---------- CheckBudgetUsage ----------

func TestCheckBudgetUsage(t *testing.T) {
	tests := []struct {
		name  string
		cfg   *core.Budget
		spent int64
		want  bool
	}{
		{name: "no config allows", want: true},
		{
			name: "zero limit means unmetered",
			cfg:  bCfg("b", 0), spent: 999_999, want: true,
		},
		{
			name:  "enforce false allows past the limit",
			cfg:   bCfg("b", 100, func(b *core.Budget) { b.Enforce = bptr(false) }),
			spent: 1_000, want: true,
		},
		{
			name: "enforce nil still enforces",
			cfg:  bCfg("b", 100), spent: 1_000, want: false,
		},
		{
			name:  "enforce true enforces",
			cfg:   bCfg("b", 100, func(b *core.Budget) { b.Enforce = bptr(true) }),
			spent: 1_000, want: false,
		},
		{name: "nothing spent", cfg: bCfg("b", 100), want: true},
		{name: "under the limit", cfg: bCfg("b", 100), spent: 99, want: true},
		{
			name: "exactly at the limit is exhausted",
			cfg:  bCfg("b", 100), spent: 100, want: false,
		},
		{name: "over the limit", cfg: bCfg("b", 100), spent: 101, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &Budget{}
			if tc.cfg != nil {
				b.Config.Store(tc.cfg)
			}
			b.WindowCost.Store(tc.spent)
			assert.Equal(t, tc.want, b.CheckBudgetUsage())
		})
	}
}

// ---------- CheckBudgetWindow ----------

func TestCheckBudgetWindow(t *testing.T) {
	tests := []struct {
		name string
		cfg  *core.Budget
		want bool
	}{
		{name: "no config is never due"},
		{
			name: "zero duration is never due",
			cfg: bCfg("b", 100, func(b *core.Budget) {
				b.BudgetParseDuration = 0
				b.LastBudgetRefreshAt = time.Now().Add(-100 * time.Hour)
			}),
		},
		{
			name: "negative duration is never due",
			cfg: bCfg("b", 100, func(b *core.Budget) {
				b.BudgetParseDuration = -time.Hour
			}),
		},
		{
			name: "fresh window is not due",
			cfg: bCfg("b", 100, func(b *core.Budget) {
				b.BudgetParseDuration = time.Hour
				b.LastBudgetRefreshAt = time.Now()
			}),
		},
		{
			name: "elapsed window is due",
			cfg: bCfg("b", 100, func(b *core.Budget) {
				b.BudgetParseDuration = time.Hour
				b.LastBudgetRefreshAt = time.Now().Add(-2 * time.Hour)
			}),
			want: true,
		},
		{
			name: "exactly at the boundary is due",
			cfg: bCfg("b", 100, func(b *core.Budget) {
				b.BudgetParseDuration = time.Hour
				b.LastBudgetRefreshAt = time.Now().Add(-time.Hour)
			}),
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &Budget{}
			if tc.cfg != nil {
				b.Config.Store(tc.cfg)
			}
			assert.Equal(t, tc.want, b.CheckBudgetWindow())
		})
	}
}

// ---------- RecordUsage ----------

func TestRecordUsage(t *testing.T) {
	b := bLive(bCfg("b", 10_000))

	b.RecordUsage(1_500)
	assert.EqualValues(t, 1_500, b.WindowCost.Load())
	assert.EqualValues(t, 1, b.WindowReqs.Load())

	b.RecordUsage(500)
	assert.EqualValues(t, 2_000, b.WindowCost.Load())
	assert.EqualValues(t, 2, b.WindowReqs.Load())

	// A zero-cost request still counts as a request.
	b.RecordUsage(0)
	assert.EqualValues(t, 2_000, b.WindowCost.Load())
	assert.EqualValues(t, 3, b.WindowReqs.Load())

	assert.Zero(t, b.LastFlushed.Load(), "recording must not touch the flushed marks")
	assert.Zero(t, b.LastReqs.Load())
}

func TestRecordUsageIsConcurrencySafe(t *testing.T) {
	b := bLive(bCfg("b", 0))

	const writers, each = 8, 500
	done := make(chan struct{}, writers)
	for range writers {
		go func() {
			for range each {
				b.RecordUsage(2)
			}
			done <- struct{}{}
		}()
	}
	for range writers {
		<-done
	}

	assert.EqualValues(t, writers*each*2, b.WindowCost.Load())
	assert.EqualValues(t, writers*each, b.WindowReqs.Load())
}

// Spend and the limit are compared on one counter, so a reader can never catch
// the pair mid-move and let a request through against a stale total.
func TestCheckBudgetUsageUnderConcurrentSpend(t *testing.T) {
	b := bLive(bCfg("b", 100))

	done := make(chan struct{})
	go func() {
		for range 1_000 {
			b.RecordUsage(1)
		}
		close(done)
	}()

	for {
		select {
		case <-done:
			assert.False(t, b.CheckBudgetUsage(), "the limit is passed by the end")
			return
		default:
			// Exhausted must never flip back to allowed.
			if !b.CheckBudgetUsage() {
				require.False(t, b.CheckBudgetUsage(), "exhaustion must be monotonic")
			}
		}
	}
}

// ---------- budgetResetTarget ----------

func TestBudgetResetTarget(t *testing.T) {
	last := time.Now().Add(-90 * time.Minute).Truncate(time.Second)

	tests := []struct {
		name string
		cfg  *core.Budget
		want *time.Time
	}{
		{name: "nil config"},
		{
			name: "zero duration",
			cfg:  &core.Budget{LastBudgetRefreshAt: last, BudgetParseDuration: 0},
		},
		{
			name: "negative duration",
			cfg:  &core.Budget{LastBudgetRefreshAt: last, BudgetParseDuration: -time.Hour},
		},
		{
			name: "not yet elapsed",
			cfg:  &core.Budget{LastBudgetRefreshAt: time.Now(), BudgetParseDuration: time.Hour},
		},
		{
			name: "one window elapsed",
			cfg:  &core.Budget{LastBudgetRefreshAt: last, BudgetParseDuration: time.Hour},
			want: bptr(last.Add(time.Hour)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := budgetResetTarget(tc.cfg, time.Now())
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}
}

// Missed ticks do not stack up: the target is the most recent boundary, so a
// process that was down for a day does not owe a day of rolls.
func TestBudgetResetTargetSnapsToWholeWindows(t *testing.T) {
	last := time.Now().Add(-5*time.Hour - 20*time.Minute).Truncate(time.Second)
	cfg := &core.Budget{LastBudgetRefreshAt: last, BudgetParseDuration: time.Hour}

	target := budgetResetTarget(cfg, time.Now())
	require.NotNil(t, target)
	assert.Equal(t, last.Add(5*time.Hour), *target, "five whole windows, not five hours twenty")
	assert.True(t, target.Before(time.Now()), "the target is a past boundary, never now")
}

// Exactly at the boundary resets; a nanosecond short does not.
func TestBudgetResetTargetAtTheBoundary(t *testing.T) {
	now := time.Now()
	cfg := &core.Budget{LastBudgetRefreshAt: now.Add(-time.Hour), BudgetParseDuration: time.Hour}

	target := budgetResetTarget(cfg, now)
	require.NotNil(t, target)
	assert.Equal(t, now, *target)

	cfg.LastBudgetRefreshAt = now.Add(-time.Hour + time.Nanosecond)
	assert.Nil(t, budgetResetTarget(cfg, now))
}

// ---------- BudgetCache ----------

func TestBudgetCacheLookupMiss(t *testing.T) {
	bc := bCache(t)

	budget, found := bc.LookupBudget("nope")
	assert.Nil(t, budget)
	assert.False(t, found)
}

func TestUpsertBudgetSeedsFromTheRow(t *testing.T) {
	bc := bCache(t, bCfg("b1", 5_000, func(b *core.Budget) {
		b.TotalSpend = 1_200
		b.RequestCount = 3
	}))

	budget, found := bc.LookupBudget("b1")
	require.True(t, found)

	// All four counters start from the row, so a restart resumes mid-window
	// rather than handing the tenant a fresh allowance.
	assert.EqualValues(t, 1_200, budget.WindowCost.Load())
	assert.EqualValues(t, 3, budget.WindowReqs.Load())
	assert.EqualValues(t, 1_200, budget.LastFlushed.Load())
	assert.EqualValues(t, 3, budget.LastReqs.Load())
}

func TestUpsertBudgetNilIsANoOp(t *testing.T) {
	bc := bCache(t, bCfg("b1", 100))

	bc.UpsertBudget(nil)

	_, found := bc.LookupBudget("b1")
	assert.True(t, found)
}

// An upsert onto a live budget replaces the config and leaves the counters, so
// spend accrued this window is not wiped by a routine sync. The database is
// written FROM these counters, so reading its totals back would clobber
// in-flight spend.
func TestUpsertBudgetPreservesLiveCounters(t *testing.T) {
	bc := bCache(t, bCfg("b1", 5_000))

	budget, found := bc.LookupBudget("b1")
	require.True(t, found)
	budget.RecordUsage(2_500)

	bc.UpsertBudget(bCfg("b1", 9_999, func(b *core.Budget) {
		b.TotalSpend = 0
		b.RequestCount = 0
	}))

	same, found := bc.LookupBudget("b1")
	require.True(t, found)
	assert.Same(t, budget, same, "the same Budget must stay in the map")
	assert.EqualValues(t, 2_500, same.WindowCost.Load(), "live spend survives the sync")
	assert.EqualValues(t, 1, same.WindowReqs.Load())
	assert.EqualValues(t, 9_999, same.Config.Load().BudgetLimit, "the new limit takes effect")
}

func TestLoadBudgetsEvictsAbsentIDs(t *testing.T) {
	bc := bCache(t)

	bc.LoadBudgets([]*core.Budget{
		bCfg("keep", 100),
		bCfg("drop", 200),
	})
	_, found := bc.LookupBudget("drop")
	require.True(t, found)

	bc.LoadBudgets([]*core.Budget{
		bCfg("keep", 100),
		bCfg("new", 300),
	})

	_, found = bc.LookupBudget("keep")
	assert.True(t, found)
	_, found = bc.LookupBudget("new")
	assert.True(t, found)
	_, found = bc.LookupBudget("drop")
	assert.False(t, found, "a budget absent from the new set must be evicted")
}

func TestLoadBudgetsWithNothingClearsTheCache(t *testing.T) {
	bc := bCache(t, bCfg("b1", 100), bCfg("b2", 200))

	bc.LoadBudgets(nil)

	_, found := bc.LookupBudget("b1")
	assert.False(t, found)
	_, found = bc.LookupBudget("b2")
	assert.False(t, found)
}

func TestDeleteBudget(t *testing.T) {
	bc := bCache(t, bCfg("b1", 100))

	bc.DeleteBudget("b1")
	_, found := bc.LookupBudget("b1")
	assert.False(t, found)

	bc.DeleteBudget("b1")
	bc.DeleteBudget("never-existed")
}
