# Sync and Settlement Design

**v4 — 2026-09-01**

Covers the background work that keeps governance and the catalog in step with
the database and the live pricing feed, and the settlement hook that turns a
completed request into a charge.

Replaces `internal/syncer` entirely. Its loop machinery becomes
`internal/worker`; its governance knowledge moves into `internal/governance`,
which is where it belonged.

### Changes since v3

- Cut the size-triggered drain. At the assumed scale the buffer cannot fill
  between ticks, and the callback gave the request path a route into the
  worker that nothing else needed.
- Cut the absolute-write section. That code is shipped; only its two
  consequences are design input, and they moved to where they apply.
- Dropped `flushOne`/`rollOne`. The two existing functions already loop; the
  drain just calls them in order.

---

## Decisions

Every value below is my default, not yours. Change any of them and the design
still holds — none of them alter its shape.

| # | Question | Decision |
| --- | --- | --- |
| Q1 | Acceptable data loss on a hard kill | 10s — sets both drain intervals |
| Q2 | Usage buffer at capacity | Drop and alert. See the note below |
| Q3 | Drain triggered by size as well as time | No — not reachable at the assumed scale |
| Q4 | Revocation window | 15s — the virtual key refresh interval |
| Q5 | Long intervals survive restart | `RunAtStart` covers it; no persisted `last_sync_at` |
| Q6 | First run at start | Yes. A 24h ticker firing first at T+24h is never what you want |
| Q7 | Meaning of `next_run_at` | Not stored. The client derives it and reads `running` |
| Q8 | Manual trigger per job | Yes — the admin "sync now" button and later the write-through path |
| Q9 | Scale | Hundreds of budgets and keys, low thousands of req/s |
| Q10 | Catalog swap doubles memory briefly | Accepted. A ~20MB feed spiking to ~40MB during swap is not worth streaming |

**Q1 is the one worth thinking about.** It is not a tuning knob — it is the
maximum billing data you are willing to lose when the process is killed
without warning. Every other protection is best effort; this number is the
real guarantee.

**Do not tune the two drains apart.** Budget flush is a handful of `UPDATE`s
against a handful of rows, so slowing it saves nothing measurable, while the
unflushed spend it holds is the data you least want to lose.

**Q2/Q3 — why there is no size trigger.** The buffer holds 100k records and
drains every 10s, so filling it needs 10,000 req/s sustained, an order of
magnitude past Q9. A high-water callback would also give the request path a
route into the worker that nothing else needs. If `records_dropped` ever leaves
zero, add the trigger then, sized against a real number rather than a guess.

---

## Constraint: a panic-safe drain does not exist

Requirement R5 asks to drain before anything else, including on panic. Two
honest limits:

Cannot be covered — `SIGKILL`, OOM kill, `os.Exit`, or an unrecovered panic in
a goroutine other than the one panicking. Go tears the process down and no
`defer` runs.

Can be covered — `SIGINT`/`SIGTERM`, panics inside job goroutines, panics
inside HTTP handlers via middleware.

So the drain interval (Q1) is the actual protection, and the run-on-stop drain
is a courtesy for the clean paths. The design treats it that way.

---

## Ownership

```
internal/worker/       periodic execution, panic recovery, graceful stop,
                       run-on-stop, manual trigger, uniform metadata.
                       Knows nothing about budgets, models or shutdown order.

internal/governance/   owns its own worker.Group
                         usage_drain   UsageBuffer -> usage_history
                         budget_drain  flush totals -> roll due windows
                         refresh       budgets -> virtual keys
                       Start / Shutdown / Stats

internal/modelcatalog/ owns its own worker.Group
                         models, pricing
                       Start / Shutdown / Stats

internal/gateway/      sequences components, aggregates Stats for admin
```

Nothing registers with `worker`. Components construct their own group. The
moment `worker` imports a domain type it has become an orchestrator, which is
what we are avoiding.

### Group is a container, not a merge

A `worker.Group` holding three jobs runs **three goroutines and three
tickers**. Nothing is serialised across jobs. The group exists to give the
component one `Start`, one `Shutdown`, and one `Stats`.

### The rule for same-job versus separate-job

> Two operations share a job **only** if they mutate the same in-memory state
> and must run in a fixed order. Otherwise they get their own job.

| operations | share state | ordered | verdict |
| --- | --- | --- | --- |
| budget flush + window roll | same `Budget` objects | yes | one job |
| budget refresh + vkey refresh | vkey needs its budget loaded | yes | one job |
| usage drain | nothing | — | own job |
| plane reload (later) | nothing | — | own job |

Splitting usage from budget buys two things: a stuck `budgets` table cannot
block usage rows landing, and — most importantly for R4 — `last_error` and
`consecutive_failures` become per-sink and therefore actionable.

### Ordering has two owners

A component owns the order of its own jobs — the budget drain flushes before it
rolls, expressed as one function doing two things in sequence, not two jobs
racing. The gateway owns the order of components: HTTP drains before governance
flushes, which happens before the store closes.

---

## 1. `internal/worker`

Two types. `Job` is the unit of work and owns its own runtime state; `Group`
starts, stops and reports on a set of them.

### Job

```go
package worker

// Detail lets a job report progress without worker knowing what it counted.
type Detail map[string]int64

type Job struct {
	Name     string
	Interval time.Duration

	// Run once during Start, before the ticker begins. A 24h job that first
	// fires at T+24h is almost never what an operator meant.
	RunAtStart bool

	// Run once more during Shutdown. Drain jobs set this; refresh jobs do not.
	RunOnStop bool

	Run func(ctx context.Context) (Detail, error)

	// Runtime state. Group fills trigger in Add; the caller leaves these zero.
	mu      sync.Mutex
	stats   JobStats
	trigger chan struct{}
}
```

`Add` takes `*Job`, never `Job`. A struct holding a `sync.Mutex` cannot be
passed by value — `go vet` reports `range var j copies lock` — and a pointer
is the fix, not a second type to hold the state.

The cost is that the caller keeps a live pointer, so writing `job.Interval`
after `Start` races the loop. `Reconfigure` is the supported path; direct
mutation is a bug, not a case to design against.

### JobStats

```go
// JobStats is what the admin panel renders. Every field is measured, never
// computed from another: NextRunAt is LastRunAt+Interval and an overrun is
// LastDuration > Interval, so neither is stored.
type JobStats struct {
	Name     string        `json:"name"`
	Interval time.Duration `json:"interval"`
	Running  bool          `json:"running"`

	// Separate on purpose: a job failing every tick has a fresh LastRunAt and
	// would otherwise look healthy. The gap between these two is the signal.
	LastRunAt     time.Time `json:"last_run_at,omitzero"`
	LastSuccessAt time.Time `json:"last_success_at,omitzero"`

	LastDuration        time.Duration `json:"last_duration"`
	LastError           string        `json:"last_error,omitempty"`
	ConsecutiveFailures int           `json:"consecutive_failures"`

	LastDetail Detail `json:"last_detail,omitempty"`
}
```

Every field earns a column:

```
usage_drain   10s   ok        2s ago   12ms   240 written, 12 pending
budget_drain  10s   FAILING   4m ago    8ms   "database is locked" x24
refresh       15s   ok        7s ago   31ms   3 budgets, 12 keys
```

Anything domain-specific goes in `Detail` rather than a new field, which is why
the struct can stay this small while the last column stays useful.

### Group

```go
type Group struct {
	name   string
	logger *zap.Logger

	mu      sync.Mutex
	jobs    map[string]*Job
	started bool

	stop chan struct{}
	wg   sync.WaitGroup
}

func NewGroup(name string, logger *zap.Logger) *Group

// Add registers jobs and allocates their trigger channels. Before Start only.
func (g *Group) Add(jobs ...*Job) error

// Start runs every RunAtStart job synchronously, then spawns the tickers.
// A synchronous failure is returned so a component can refuse to serve.
func (g *Group) Start(ctx context.Context) error

// Shutdown stops the tickers, waits for in-flight runs, then executes every
// RunOnStop job once. Honours ctx so a hung DB cannot block forever.
func (g *Group) Shutdown(ctx context.Context) error

// Trigger asks a job to run now. Never blocks, so any caller is safe.
func (g *Group) Trigger(name string) error

func (g *Group) Reconfigure(name string, interval time.Duration) error
func (g *Group) Stats() []JobStats
```

`Start` sets `started` under the mutex and refuses a second call. The old
syncer did not, and a second `Start` orphaned the first set of goroutines with
no reachable stop channel.

### Running a job

```go
func (j *Job) loop(ctx context.Context, stop <-chan struct{}, log *zap.Logger) {
	ticker := time.NewTicker(j.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			j.exec(ctx, log)
		case <-j.trigger:
			j.exec(ctx, log)
			ticker.Reset(j.Interval) // a manual run restarts the window
		case <-stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

// exec runs the job once and records what happened. It recovers panics so one
// bad job cannot take the process down.
func (j *Job) exec(ctx context.Context, log *zap.Logger) {
	start := time.Now()
	j.markRunning(start)

	var (
		detail Detail
		err    error
	)
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
				log.Error("job panicked",
					zap.String("job", j.Name),
					zap.Any("panic", p),
					zap.String("stack", string(debug.Stack())))
			}
		}()
		detail, err = j.Run(ctx)
	}()

	took := time.Since(start)
	j.markDone(detail, err, took)

	if err != nil {
		log.Error("job failed", zap.String("job", j.Name), zap.Error(err))
	}
	if took > j.Interval {
		// A Go ticker drops ticks when the receiver is slow, so a job that
		// overruns silently stretches its own cadence. Nothing else says so.
		log.Warn("job overran its interval",
			zap.String("job", j.Name),
			zap.Duration("took", took),
			zap.Duration("interval", j.Interval))
	}
}
```

`Trigger` — the admin "sync now" path (Q8):

```go
func (g *Group) Trigger(name string) error {
	g.mu.Lock()
	j, ok := g.jobs[name]
	g.mu.Unlock()
	if !ok {
		return fmt.Errorf("worker: no job named %q", name)
	}
	select {
	case j.trigger <- struct{}{}:
	default: // already pending, coalesce
	}
	return nil
}
```

Cap 1 with a non-blocking send, so an impatient operator clicking twice gets
one run rather than two, and no caller can ever block on a full channel.

## 2. Governance lifecycle

### Shape

```go
type Governance struct {
	Store       *dbstore.Store
	KeyCache    *VirtualkeyCache
	BudgetCache *BudgetCache
	UsageBuffer *UsageBuffer
	logger      *zap.Logger

	workers *worker.Group
}

type Config struct {
	DrainInterval   time.Duration // Q1: the data-loss window, both drains
	RefreshInterval time.Duration // Q4: the revocation window
}
```

### Start

```go
// Start loads policy synchronously before any worker spawns. Serving traffic
// with an empty virtual key cache would reject every request.
func (g *Governance) Start(ctx context.Context, cfg Config) error {
	if _, err := g.refresh(ctx); err != nil {
		return fmt.Errorf("governance start: %w", err)
	}

	g.workers = worker.NewGroup("governance", g.logger)
	if err := g.workers.Add(
		&worker.Job{
			Name:      "usage_drain",
			Interval:  cfg.DrainInterval,
			RunOnStop: true,
			Run:       g.usageDrain,
		},
		&worker.Job{
			Name:      "budget_drain",
			Interval:  cfg.DrainInterval,
			RunOnStop: true,
			Run:       g.budgetDrain,
		},
		&worker.Job{
			Name:     "refresh",
			Interval: cfg.RefreshInterval,
			Run:      g.refresh,
		},
	); err != nil {
		return err
	}

	return g.workers.Start(ctx)
}

func (g *Governance) Shutdown(ctx context.Context) error { return g.workers.Shutdown(ctx) }
func (g *Governance) Stats() []worker.JobStats           { return g.workers.Stats() }
```

`refresh` is not `RunAtStart` because `Start` already called it directly — it
needs the error to be fatal, which a background run cannot express.

### usage_drain

```go
// usageDrain writes the buffered ledger rows. It shares no state with the
// budget drain, so a stuck budgets table cannot hold these rows back.
func (g *Governance) usageDrain(ctx context.Context) (worker.Detail, error) {
	written := g.FlushUsageHistory()
	return worker.Detail{
		"records_written": written,
		"records_pending": int64(g.UsageBuffer.Len()),
		"records_dropped": g.UsageBuffer.DroppedCount(),
	}, nil
}
```

`records_dropped` climbing means billing data is being lost. It belongs on a
dashboard, not only in a log line.

### budget_drain — one job, ordered

Flush and roll stay in one goroutine. The write is absolute — see Already done
— so a roll landing first no longer loses the pending spend, but it would
charge it to the wrong window, so the ordering still matters.

```go
// budgetDrain persists the totals this process holds, then rolls any window
// that is due. Order matters: spend must be attributed to the window it
// happened in.
func (g *Governance) budgetDrain(ctx context.Context) (worker.Detail, error) {
	flushed := g.FlushBudgetUsage()
	rolled := g.TrackBudgetWindow()
	return worker.Detail{"budgets_flushed": flushed, "windows_rolled": rolled}, nil
}
```

Both functions already exist and already loop over the map; the only change is
an `int64` return so the job can report counts. Two passes instead of one costs
microseconds at this scale, and every budget is still flushed before any is
rolled.

The map can change between the passes if `refresh` runs concurrently. Both
cases are harmless: a budget added between them has no pending spend to lose,
and one removed simply is not rolled this tick.

### refresh — ordered, stale-tolerant

```go
// refresh pulls policy from the database. Budgets load first: a virtual key
// whose budget is missing is rejected, not allowed.
func (g *Governance) refresh(ctx context.Context) (worker.Detail, error) {
	budgets, err := g.syncBudgets(ctx)
	if err != nil {
		return nil, err // previous snapshot stays live
	}
	keys, err := g.syncVirtualKeys(ctx)
	if err != nil {
		return worker.Detail{"budgets": budgets}, err
	}
	return worker.Detail{"budgets": budgets, "virtual_keys": keys}, nil
}
```

A failure returns the error and changes nothing. `LoadBudgets` and
`LoadVirtualKeys` both swap atomically, and `UpsertBudget` preserves live
counters, so a refresh can never blank a cache or discard unflushed spend.

---

## 3. Catalog lifecycle

Same shape, different cadence. `internal/modelcatalog/sync.go` loses its
duplicate `loop()` and keeps its domain logic.

```go
func (c *ModelCatalog) Start(ctx context.Context, cfg config.ModelCatalogConfig) error {
	c.workers = worker.NewGroup("catalog", c.logger)
	if err := c.workers.Add(
		&worker.Job{
			Name:       "models",
			Interval:   cfg.SyncInterval,
			RunAtStart: true,
			Run:        c.syncModels,
		},
		&worker.Job{
			Name:       "pricing",
			Interval:   cfg.SyncInterval,
			RunAtStart: true,
			Run:        c.syncPricing,
		},
	); err != nil {
		return err
	}
	return c.workers.Start(ctx)
}
```

`RunAtStart` is true and the errors are fatal, because serving with no pricing
means every request bills zero. `Group.Start` returns the first synchronous
failure.

**Q5 — overdue at boot.** With `RunAtStart` the catalog always syncs on start,
so a 24h interval on a process that restarts every 12h still gets fresh data.
Persisting `last_sync_at` would let a restart *skip* a sync that is not yet
due; that is an optimisation, not a correctness need. Deferred.

Before any of this compiles, `sync.go` needs its `//go:build ignore` removed
and `cfg.SourceName` corrected to `cfg.SourceURL` — the file predates the
current `ModelCatalogConfig`.

---

## 4. Settlement hook

Replaces both `dummyUsageHook` and `dummyBudgetHook`. Cost is computed once
and both halves need it, so it is one hook, not two.

```go
// PriceLookup mirrors ModelLookup: governance depends on the behaviour, not
// on the modelcatalog package.
type PriceLookup interface {
	ResolvePrice(virtualKeyID string, key core.CatalogKey, selectorKey string) *core.Pricing
}

type SettlementHook struct {
	catalog PriceLookup
	budgets *BudgetCache
	usage   *UsageBuffer
	logger  *zap.Logger
}

func (h *SettlementHook) Name() string { return "settlement" }

func (h *SettlementHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	// The provider was never reached, so there is nothing to charge for.
	if !rctx.RequestCompleted || rctx.Usage == nil {
		return nil
	}

	price := h.catalog.ResolvePrice(rctx.VirtualKeyID, rctx.Modelkey, core.EmptySelectorKey)
	if price == nil {
		// Record the tokens anyway. Losing the row to a stale catalog is worse
		// than a zero charge that can be backfilled.
		h.logger.Warn("no price for model, recording zero cost",
			zap.String("model", rctx.Modelkey.SlashKey()))
	}

	var nano int64
	if price != nil {
		nano = core.ToNanoUSD(core.CalculateCost(*price, *rctx.Usage))
	}
	rctx.Cost = core.FromNanoUSD(nano)

	h.usage.Append(UsageRecord{
		ClientID:       rctx.ClientID,
		BudgetID:       rctx.BudgetRef,
		Backend:        string(rctx.Modelkey.Provider),
		ModelName:      rctx.Modelkey.ModelName,
		InputTokens:    rctx.Usage.InputTokens,
		OutputTokens:   rctx.Usage.OutputTokens,
		ResponseBytes:  rctx.ResponseBytes,
		ResponseStatus: rctx.ResponseStatus,
		Cost:           nano,
		RequestedAt:    time.Now(),
	})

	if budget, ok := h.budgets.LookupBudget(rctx.BudgetRef); ok {
		budget.RecordUsage(nano)
	}
	return nil
}
```

Registered on the post-provider stage, replacing both dummies:

```go
engine.AddPostProviderHook(NewSettlementHook(catalog, gov.BudgetCache, gov.UsageBuffer, logger))
```

### The contract this sets

```
request path   memory only     UsageBuffer.Append()   mutex + slice append
                               Budget.RecordUsage()   two atomic adds

worker         database only   BulkInsertUsageHistory()
                               FlushBudgetUsage()
```

The two never touch. The request path only appends to memory; every database
write happens on a worker goroutine. Nothing on the request path calls into
`worker` at all — which is what dropping the high-water callback bought.

### Decisions embedded above

Settlement is **post-provider**, not pre-provider where `dummyBudgetHook` sits
today — otherwise a request that never reached the upstream gets charged.
Admission (`BudgetCheckHook`) stays pre-call.

`rctx.Cost` is set from the rounded nano, not the raw float, so what is
reported matches what was stored.

### `ModelID` has a field but no source

`UsageRecord.ModelID` exists and `FlushUsageHistory` already maps it to the
store row. Nothing populates it: the provider resolves the deployment as
`alias.ModelID` and never writes it back to `rctx`, so settlement has nothing
to read.

Either add `rctx.UpstreamModel`, set in the provider's config builder, or drop
the column. Leaving a mapped column permanently empty is the worst of the
three.

`RequestedAt` has the same shape of problem — `rctx` carries `TTFB` but no
request start time. `time.Now()` at settlement is close enough today; a real
`StartedAt` is more honest and is needed for latency metrics anyway.

### Not yet: double-settlement guard

A streaming request can end two ways — clean finish, or client disconnect. If
both paths settle, the request is charged twice. That is not the rare
commit-then-timeout case; it is a user closing a browser tab.

The guard is an idempotency set keyed on request id plus attempt number, swept
on a TTL. Only the terminal settlement is deduped, because a stream
legitimately reports usage on many chunks. Build it with the SSE renderer,
which is where the second path appears.

Note this is needed for the **usage ledger** specifically. The budget side is
already covered by the absolute write.

---

## 5. Gateway sequencing

```go
func (gw *Gateway) Shutdown(ctx context.Context) error {
	// Drain HTTP first, or the flush below races requests still arriving.
	if err := gw.server.Shutdown(ctx); err != nil {
		gw.logger.Error("http shutdown", zap.Error(err))
	}
	if err := gw.governance.Shutdown(ctx); err != nil {
		gw.logger.Error("governance shutdown", zap.Error(err))
	}
	if err := gw.catalog.Shutdown(ctx); err != nil {
		gw.logger.Error("catalog shutdown", zap.Error(err))
	}
	return gw.store.Close()
}
```

Errors are logged and shutdown continues. Stopping early on a catalog error
would skip `store.Close()` and leave the database file locked.

Governance before catalog because governance holds unflushed money and the
catalog holds nothing that needs saving.

---

## 6. Admin stats

```go
type ComponentStats struct {
	Component string            `json:"component"`
	Jobs      []worker.JobStats `json:"jobs"`
}

func (gw *Gateway) Stats() []ComponentStats {
	return []ComponentStats{
		{Component: "governance", Jobs: gw.governance.Stats()}, // 3 rows
		{Component: "catalog", Jobs: gw.catalog.Stats()},       // 2 rows
	}
}
```

Five rows, one table. Because the shape is defined once in `worker`, adding a
component adds a row rather than new marshalling.

```json
{
  "component": "governance",
  "jobs": [{
    "name": "usage_drain",
    "interval": "10s",
    "running": false,
    "last_run_at": "2026-08-31T14:22:10Z",
    "last_success_at": "2026-08-31T14:22:10Z",
    "last_duration": "12ms",
    "consecutive_failures": 0,
    "last_detail": {
      "records_written": 240,
      "records_pending": 12,
      "records_dropped": 0
    }
  }]
}
```

The panel derives the next run as `last_run_at + interval` and shows "running
now" instead when `running` is set, so no stale future timestamp is stored.

Per-sink `last_error` and `consecutive_failures` are why the drains are
separate jobs: grouped, neither field could tell an operator which sink was
sick.

---

## Delivery order

1. `internal/worker` with its own tests. Pure and self-contained; no domain types.
2. Governance `Start`/`Shutdown`/`Stats`, the two drains, `refresh`. Delete `internal/syncer`.
3. Catalog `Start`/`Shutdown`/`Stats`. Un-ignore `sync.go`, fix `SourceName`, delete its `loop()`.
4. `SettlementHook`, replacing both dummies. Delete `dummyhooks.go`.
5. Gateway sequencing and the stats endpoint.

Steps 1–3 can land before the server exists; they are testable without a
request. Step 4 needs `rctx.Usage` populated, which is the orchestrator's job.

---

## Already done

The budget correctness work this design assumes is in the tree and tested
(`internal/governance/budget_test.go`, 23 tests under `-race`):

- `UpsertBudget` is the single install path and preserves live counters.
- `budgetResetTarget` derives the window boundary instead of stamping the
  clock, so a late ticker cannot drift the schedule.
- `CheckBudgetUsage` no longer treats a stale window as a free window.
- The budget flush writes an absolute total, so a retry after an uncertain
  write cannot double-charge.

That last one has a consequence worth recording: an absolute write is
last-writer-wins, so two gateways sharing a database would clobber each other's
totals rather than sum them. **Budget enforcement is per-instance** — N
gateways allow roughly N times the limit. This was already true in effect,
since `PendingCost` is per-process and one node never sees another's unflushed
spend. It is now structural, and worth stating rather than solving.

---

## Open items

**`internal/worker` tests** — ticker fires, run-at-start, run-on-stop, panic
recovery does not kill the group, trigger coalesces, shutdown waits for
in-flight, double `Start` is refused, an overrun logs its warning.

**Backpressure alerting.** `records_dropped` climbing means billing data is
being lost. It needs to reach an operator, not just a log line.

**Write-through on revocation.** `Trigger("refresh")` from an admin delete
makes revocation immediate instead of bounded by Q4. Lands with the admin API.

**Q1 confirmation.** The 10s drain interval is the only number here that is a
guarantee rather than a tuning knob.
