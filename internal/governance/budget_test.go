package governance

import (
	"errors"
	"sync"
	"testing"
	"time"

	"diffractllm/internal/core"

	"go.uber.org/zap"
)

func testBudget(id string, limit int64, window time.Duration, lastReset time.Time) *core.Budget {
	return &core.Budget{
		ID:                  id,
		Name:                id,
		BudgetLimit:         limit,
		BudgetUnit:          core.BudgetUnitNanoUSD,
		Enforce:             true,
		LastBudgetRefreshAt: lastReset,
		BudgetParseDuration: window,
	}
}

func newCache() *BudgetCache {
	return &BudgetCache{logger: zap.NewNop()}
}

func ptr(t time.Time) *time.Time { return &t }

// ---------------------------------------------------------------------------
// budgetResetTarget - the boundary must be derived, never the caller's clock
// ---------------------------------------------------------------------------

func TestBudgetResetTarget(t *testing.T) {
	base := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	const day = 24 * time.Hour

	tests := []struct {
		name   string
		cfg    *core.Budget
		now    time.Time
		want   *time.Time
		reason string
	}{
		{
			name:   "nil config",
			cfg:    nil,
			now:    base,
			want:   nil,
			reason: "a missing config is not a due window",
		},
		{
			name:   "no window configured",
			cfg:    testBudget("b", 100, 0, base),
			now:    base.Add(1000 * day),
			want:   nil,
			reason: "duration zero means the budget never rolls",
		},
		{
			name:   "window still open",
			cfg:    testBudget("b", 100, day, base),
			now:    base.Add(23 * time.Hour),
			want:   nil,
			reason: "an open window must not reset",
		},
		{
			name:   "exactly at the boundary",
			cfg:    testBudget("b", 100, day, base),
			now:    base.Add(day),
			want:   ptr(base.Add(day)),
			reason: "the boundary itself is due",
		},
		{
			name:   "one window elapsed, ticker late",
			cfg:    testBudget("b", 100, day, base),
			now:    base.Add(day + 7*time.Second),
			want:   ptr(base.Add(day)),
			reason: "the boundary is stamped, not the late wall clock",
		},
		{
			name:   "many windows elapsed collapse to the current boundary",
			cfg:    testBudget("b", 100, day, base),
			now:    base.Add(5*day + 3*time.Hour),
			want:   ptr(base.Add(5 * day)),
			reason: "a long outage rolls once, not five times",
		},
		{
			name:   "sub-second window",
			cfg:    testBudget("b", 100, 100*time.Millisecond, base),
			now:    base.Add(350 * time.Millisecond),
			want:   ptr(base.Add(300 * time.Millisecond)),
			reason: "integer window arithmetic holds at small durations",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := budgetResetTarget(tc.cfg, tc.now)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("%s: got %v, want nil", tc.reason, *got)
			case tc.want != nil && got == nil:
				t.Fatalf("%s: got nil, want %v", tc.reason, *tc.want)
			case tc.want != nil && !got.Equal(*tc.want):
				t.Fatalf("%s: got %v, want %v", tc.reason, *got, *tc.want)
			}
		})
	}
}

// Applying a target must make an immediate re-evaluation return nil, or a
// budget could sit perpetually due and reset on every single tick.
func TestBudgetResetTargetIsNotPerpetuallyDue(t *testing.T) {
	base := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	const window = time.Hour

	for _, elapsed := range []time.Duration{
		window,
		window + time.Nanosecond,
		3*window + 17*time.Minute,
		500 * window,
	} {
		cfg := testBudget("b", 100, window, base)
		now := base.Add(elapsed)

		target := budgetResetTarget(cfg, now)
		if target == nil {
			t.Fatalf("elapsed %v: expected a due window", elapsed)
		}

		cfg.LastBudgetRefreshAt = *target
		if again := budgetResetTarget(cfg, now); again != nil {
			t.Errorf("elapsed %v: still due after applying target %v -> %v", elapsed, *target, *again)
		}
	}
}

// Stamping now() makes the next window start from ticker phase, so the error
// compounds every cycle.
func TestBudgetWindowDoesNotDrift(t *testing.T) {
	base := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	const window = time.Hour

	// A tick that does not divide the window, which is the realistic case and
	// the one where the old behaviour compounds.
	const tick = 7 * time.Second
	const cycles = 100

	cfg := testBudget("b", 100, window, base)
	resets := 0

	driftOrigin := base // what the old now()-stamping code would have produced
	driftResets := 0

	var lastTick time.Time
	end := base.Add(cycles * window)
	for now := base; !now.After(end); now = now.Add(tick) {
		lastTick = now
		if target := budgetResetTarget(cfg, now); target != nil {
			cfg.LastBudgetRefreshAt = *target
			resets++
		}
		// Old behaviour: the observation time becomes the next window's origin.
		if now.Sub(driftOrigin) >= window {
			driftOrigin = now
			driftResets++
		}
	}

	// The invariant: the origin never leaves the grid, whatever the tick phase.
	if off := cfg.LastBudgetRefreshAt.Sub(base) % window; off != 0 {
		t.Errorf("origin left the window grid by %v (at %v)", off, cfg.LastBudgetRefreshAt)
	}
	// One reset per boundary the ticker actually observed - not per boundary
	// that exists, since the last one may fall after the final tick.
	wantResets := int(lastTick.Sub(base) / window)
	if resets != wantResets {
		t.Errorf("reset %d times, want %d - one per observed boundary", resets, wantResets)
	}
	// And the window it opened must be the current one.
	if gap := lastTick.Sub(cfg.LastBudgetRefreshAt); gap >= window {
		t.Errorf("origin is %v behind the last tick, more than one window", gap)
	}

	// Prove the test would have caught the old behaviour: stamping the
	// observation time walks the origin off the grid, a little more each cycle.
	offGrid := driftOrigin.Sub(base) % window
	if offGrid == 0 {
		t.Error("expected now()-stamping to leave the window grid, so this test is meaningful")
	}
	t.Logf("now()-stamping after %d windows: origin %v off the grid, %d rolls vs %d, %v behind",
		cycles, offGrid, driftResets, resets, cfg.LastBudgetRefreshAt.Sub(driftOrigin))
}

// ---------------------------------------------------------------------------
// UpsertBudget - the single install path must never clobber live counters
// ---------------------------------------------------------------------------

func TestUpsertBudgetSeedsOnFirstInsert(t *testing.T) {
	bc := newCache()
	stored := testBudget("b1", 1000, time.Hour, time.Now())
	stored.TotalSpend = 250
	stored.RequestCount = 7

	bc.UpsertBudget(stored)

	b, ok := bc.LookupBudget("b1")
	if !ok {
		t.Fatal("budget was not installed")
	}
	if got := b.TotalCost.Load(); got != 250 {
		t.Errorf("TotalCost = %d, want 250 seeded from the database", got)
	}
	if got := b.RequestCount.Load(); got != 7 {
		t.Errorf("RequestCount = %d, want 7 seeded from the database", got)
	}
}

// The reload regression: a config refresh must not discard recorded spend.
func TestUpsertBudgetPreservesLiveCounters(t *testing.T) {
	bc := newCache()
	now := time.Now()
	bc.UpsertBudget(testBudget("b1", 1000, time.Hour, now))

	b, _ := bc.LookupBudget("b1")
	b.RecordUsage(300) // pending, not yet flushed
	b.TotalCost.Add(150)
	b.RequestCount.Add(2)

	// A reload replays the database row, which still shows the old totals.
	reloaded := testBudget("b1", 5000, time.Hour, now) // limit changed
	reloaded.TotalSpend = 0
	reloaded.RequestCount = 0
	bc.UpsertBudget(reloaded)

	same, _ := bc.LookupBudget("b1")
	if same != b {
		t.Fatal("upsert replaced the Budget pointer, breaking in-flight references")
	}
	if got := same.PendingCost.Load(); got != 300 {
		t.Errorf("PendingCost = %d, want 300 preserved across reload", got)
	}
	if got := same.PendingRequests.Load(); got != 1 {
		t.Errorf("PendingRequests = %d, want 1 preserved across reload", got)
	}
	if got := same.TotalCost.Load(); got != 150 {
		t.Errorf("TotalCost = %d, want 150 preserved across reload", got)
	}
	if got := same.RequestCount.Load(); got != 2 {
		t.Errorf("RequestCount = %d, want 2 preserved across reload", got)
	}

	// The config half of the reload must still take effect.
	if got := same.Config.Load().BudgetLimit; got != 5000 {
		t.Errorf("BudgetLimit = %d, want the reloaded 5000", got)
	}
}

func TestLoadBudgetsPreservesCountersAndPrunes(t *testing.T) {
	bc := newCache()
	now := time.Now()
	bc.LoadBudgets([]*core.Budget{
		testBudget("keep", 1000, time.Hour, now),
		testBudget("drop", 1000, time.Hour, now),
	})

	keep, _ := bc.LookupBudget("keep")
	keep.RecordUsage(420)

	// Second sync: "drop" is gone from the database, "keep" has a new limit.
	bc.LoadBudgets([]*core.Budget{testBudget("keep", 9999, time.Hour, now)})

	if _, ok := bc.LookupBudget("drop"); ok {
		t.Error("a budget removed from the database should be pruned")
	}
	survived, ok := bc.LookupBudget("keep")
	if !ok {
		t.Fatal("keep was pruned by mistake")
	}
	if got := survived.PendingCost.Load(); got != 420 {
		t.Errorf("PendingCost = %d, want 420 to survive the sync", got)
	}
	if got := survived.Config.Load().BudgetLimit; got != 9999 {
		t.Errorf("BudgetLimit = %d, want the resynced 9999", got)
	}
}

func TestUpsertBudgetConcurrentFirstWritersCollapse(t *testing.T) {
	bc := newCache()
	now := time.Now()

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			bc.UpsertBudget(testBudget("b1", 1000, time.Hour, now))
		})
	}
	wg.Wait()

	count := 0
	bc.BudgetMap.Range(func(_, _ any) bool { count++; return true })
	if count != 1 {
		t.Errorf("map holds %d entries, want 1", count)
	}
}

// Accrual must survive a reload racing it, which is the shape the budget
// re-sync job will create.
func TestUpsertBudgetRacingRecordUsage(t *testing.T) {
	bc := newCache()
	now := time.Now()
	bc.UpsertBudget(testBudget("b1", 1_000_000, time.Hour, now))
	b, _ := bc.LookupBudget("b1")

	const charges = 500
	var wg sync.WaitGroup
	wg.Go(func() {
		for range charges {
			b.RecordUsage(1)
		}
	})
	wg.Go(func() {
		for range charges {
			bc.UpsertBudget(testBudget("b1", 1_000_000, time.Hour, now))
		}
	})
	wg.Wait()

	if got := b.PendingCost.Load(); got != charges {
		t.Errorf("PendingCost = %d, want %d - a reload swallowed spend", got, charges)
	}
}

// ---------------------------------------------------------------------------
// CheckBudgetUsage - a stale window is not a free window
// ---------------------------------------------------------------------------

func TestCheckBudgetUsage(t *testing.T) {
	now := time.Now()

	t.Run("under the limit passes", func(t *testing.T) {
		b := &Budget{}
		b.Config.Store(testBudget("b", 1000, time.Hour, now))
		b.TotalCost.Store(400)
		b.PendingCost.Store(100)
		if !b.CheckBudgetUsage() {
			t.Error("500 of 1000 should pass")
		}
	})

	t.Run("pending counts toward the limit", func(t *testing.T) {
		b := &Budget{}
		b.Config.Store(testBudget("b", 1000, time.Hour, now))
		b.TotalCost.Store(400)
		b.PendingCost.Store(700)
		if b.CheckBudgetUsage() {
			t.Error("unflushed spend must still be enforced")
		}
	})

	t.Run("an expired window is not a free window", func(t *testing.T) {
		b := &Budget{}
		b.Config.Store(testBudget("b", 1000, time.Hour, now.Add(-48*time.Hour)))
		b.TotalCost.Store(5000)
		if b.CheckBudgetUsage() {
			t.Error("a stale window must stay strict until the reset lands")
		}
	})

	t.Run("enforcement off always passes", func(t *testing.T) {
		cfg := testBudget("b", 1000, time.Hour, now)
		cfg.Enforce = false
		b := &Budget{}
		b.Config.Store(cfg)
		b.TotalCost.Store(999_999)
		if !b.CheckBudgetUsage() {
			t.Error("enforce=false must not block")
		}
	})

	t.Run("nil config passes rather than locking everyone out", func(t *testing.T) {
		b := &Budget{}
		if !b.CheckBudgetUsage() {
			t.Error("a budget with no config must not deny traffic")
		}
	})
}

// ---------------------------------------------------------------------------
// Absolute flush - writing the same total twice must be harmless
// ---------------------------------------------------------------------------

// fakeBudgetStore records what was written and can fail on demand, standing in
// for a commit that lands but reports an error.
type fakeBudgetStore struct {
	mu       sync.Mutex
	cost     map[string]int64
	reqs     map[string]int64
	writes   int
	failNext bool
}

func newFakeStore() *fakeBudgetStore {
	return &fakeBudgetStore{cost: map[string]int64{}, reqs: map[string]int64{}}
}

// flush mirrors Store.FlushBudgetUsage: an absolute write, not an increment.
func (f *fakeBudgetStore) flush(id string, totalCost, totalReqs int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	f.cost[id] = totalCost
	f.reqs[id] = totalReqs
	if f.failNext {
		f.failNext = false
		return errors.New("commit landed but the connection dropped")
	}
	return nil
}

// flushOne mirrors the body of Governance.FlushBudgetUsage for one budget, so
// the logic is testable without a database.
func flushOne(b *Budget, write func(string, int64, int64) error) {
	cfg := b.Config.Load()
	if cfg == nil {
		return
	}
	pendingCost := b.PendingCost.Swap(0)
	pendingReq := b.PendingRequests.Swap(0)
	if pendingCost == 0 && pendingReq == 0 {
		return
	}
	totalCost := b.TotalCost.Add(pendingCost)
	totalReq := b.RequestCount.Add(pendingReq)
	_ = write(cfg.ID, totalCost, totalReq)
}

// The double-billing scenario: the write commits, the caller sees an error,
// and the next tick writes again. With an absolute write the total must hold.
func TestAbsoluteFlushSurvivesUncertainWrite(t *testing.T) {
	store := newFakeStore()
	b := &Budget{}
	b.Config.Store(testBudget("b1", 1_000_000, time.Hour, time.Now()))

	b.RecordUsage(250)

	// Tick 1: the DB commits, then reports failure.
	store.failNext = true
	flushOne(b, store.flush)

	if got := store.cost["b1"]; got != 250 {
		t.Fatalf("after the uncertain write DB holds %d, want 250", got)
	}

	// Tick 2: nothing new was charged, so there is nothing to write.
	flushOne(b, store.flush)

	if got := store.cost["b1"]; got != 250 {
		t.Errorf("DB holds %d after retry, want 250 - a delta write would show 500", got)
	}
	if got := b.TotalCost.Load(); got != 250 {
		t.Errorf("memory holds %d, want 250", got)
	}
	if store.writes != 1 {
		t.Errorf("store took %d writes, want 1 - the second tick had nothing pending", store.writes)
	}
}

// Spend recorded while an uncertain write was in flight must still land, and
// the rewritten total must include it exactly once.
func TestAbsoluteFlushRewritesTotalAfterFailure(t *testing.T) {
	store := newFakeStore()
	b := &Budget{}
	b.Config.Store(testBudget("b1", 1_000_000, time.Hour, time.Now()))

	b.RecordUsage(250)
	store.failNext = true
	flushOne(b, store.flush)

	// More traffic arrives, then the next tick writes the new total.
	b.RecordUsage(100)
	flushOne(b, store.flush)

	if got := store.cost["b1"]; got != 350 {
		t.Errorf("DB holds %d, want 350", got)
	}
	if got := store.reqs["b1"]; got != 2 {
		t.Errorf("DB holds %d requests, want 2", got)
	}
}

func TestAbsoluteFlushAccumulatesAcrossTicks(t *testing.T) {
	store := newFakeStore()
	b := &Budget{}
	b.Config.Store(testBudget("b1", 1_000_000, time.Hour, time.Now()))

	for tick := 1; tick <= 4; tick++ {
		b.RecordUsage(100)
		flushOne(b, store.flush)

		if got, want := store.cost["b1"], int64(tick*100); got != want {
			t.Errorf("tick %d: DB holds %d, want %d", tick, got, want)
		}
		if got, want := store.reqs["b1"], int64(tick); got != want {
			t.Errorf("tick %d: DB holds %d requests, want %d", tick, got, want)
		}
	}
}
