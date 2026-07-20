# diffractllm — Architecture Goal

> A reference sketch of where the gateway is heading. Kept as a memory aid, not a spec.
> Design is **flat-map**: API key owns `AllowedModels`; deployments are cross-produced at runtime.

---

## 1. The two registries (joined by an ID)

Two runtime structures, joined by `Deployment.APIRegistryID`:

```
ModelAPIRegistry (a connection)              ModelRegistry snapshot
  ID: "reg-openai-1"                           ModelLookup:
  Provider: openai                               {openai, gpt-4} → [
  BaseURL: https://api.openai.com/v1                Deployment{APIRegistryID:"reg-openai-1", State},
  APIkey: <encrypted-at-rest>                       Deployment{APIRegistryID:"reg-openai-2", State},
  AllowedModels: [gpt-4, gpt-4o]                 ]   (one bucket = many keys serving same model)
```

- **API Registry row** = one upstream credential/connection (base URL + key + headers). Owns the list of models it may serve.
- **Deployment** = one (model × key) pairing. Holds only identity + its own `DeploymentState` (per-key circuit breaker). A 429 on `reg-openai-1` trips *that* deployment, not `reg-openai-2`.
- **The join**: `Deployment.APIRegistryID` → `APIRegistry.LookupAPIRegistry(id)` returns the shared connection at dispatch time. Keeps deployments lean; a key's credential rotates in one place.

Adding a model = append to a key's `AllowedModels` → `SyncDeployments(apiDetails)` rebuilds just that key's bucket.

---

## 2. Gateway struct (proposed shape)

Flatter than alphaX — no rules engine / routing-table / policies. Two registries + governance instead.

```go
type gatewayImpl struct {
    // --- config / source ---
    config   *config.GatewayConfig
    dbsource *dbstore.DBSource        // Load() → ModelPlaneSnapshot; owns *Store

    // --- runtime state (the two registries, joined by APIRegistryID) ---
    modelRegistry *registry.ModelRegistry   // [provider/model] → []*Deployment (+ per-key State)
    apiRegistry   *registry.APIRegistry     // registryID → *ModelAPIRegistry (baseURL, key, headers)

    // --- policy / control plane ---
    governance *governance.Governance    // vkeys, budgets, usage
    hookEngine *core.HookEngine          // PreCall(auth+admission) / PreProvider / PostProvider / PostCall
    syncer     *syncer.Syncer            // background flush jobs

    // --- data plane ---
    proxy *dataplane.ReverseProxy        // transport + health checker      [TO BUILD]

    // --- serving + lifecycle ---
    httpServer server.Server             // gin router + genericHandler     [TO BUILD]
    logger     *zap.Logger
    stopChan   chan struct{}
    startTime  time.Time
    isRunning  bool
    mu         sync.RWMutex
}
```

**Note vs alphaX:** alphaX wraps `modelRegistry` in `atomic.Pointer` for whole-registry swaps.
Here you **don't** — the atomic COW swap already lives *inside* `ModelRegistry` / `APIRegistry`
(their `SyncDeployments` / `Upsert` do copy-on-write internally). The gateway holds plain pointers.

### Lifecycle

`Initialize()` → `Start()` → `WaitForSignal()` → `Stop()`

- **Initialize**: build logger → `dbsource.Init()` (migrate+seed) → `dbsource.Load()` →
  construct `modelRegistry` + `apiRegistry` from the snapshot → `governance.InitGovernance()` +
  register `AuthHook` → build `proxy` → register providers.
- **Start**: `proxy.StartHealthCheck()` → `syncer.Start()` → `httpServer.Start()`.
- **Stop**: reverse order, drain transports.

---

## 3. Request flow

```
                          ┌─────────────────────────────────────────────┐
   HTTP request           │                 GATEWAY                      │
  (POST /openai/...)      │                                             │
        │                 │                                             │
        ▼                 │                                             │
  ┌───────────┐           │                                             │
  │  SERVER   │  genericHandler:                                        │
  │  (gin)    │  • acquire rctx from pool                               │
  └─────┬─────┘  • match SDK descriptor from URL path                   │
        │        • parse "model" from body → rctx.Provider, rctx.Model  │
        ▼                                                               │
  ┌───────────────┐   RunPreCallHooks                                   │
  │  HOOK ENGINE  │──────────────┐                                      │
  └───────────────┘              ▼                                      │
        │              ┌──────────────────┐  validate vkey, set        │
        │              │   AuthHook        │  Mode / AllowedModels /    │
        │              │  (governance)     │  ModelPools; admission-    │
        │              └──────────────────┘  check the model  ── reject → 401/403
        ▼
  ModelKey{Provider, Model}
        │
        ▼
  ┌────────────────────┐   LookupModel(key)
  │   MODEL REGISTRY    │──────────────────► []*Deployment   (each has its own State)
  └────────────────────┘
        │
        ▼
  ┌────────────────────┐   pick 1 healthy deployment
  │  DATA PLANE  (LB)  │   using DeploymentState (circuit breaker)   [TO BUILD]
  └────────┬───────────┘
           │  chosen.APIRegistryID
           ▼
  ┌────────────────────┐   LookupAPIRegistry(id)
  │   API REGISTRY      │──────────────────► *ModelAPIRegistry
  └────────────────────┘                     (BaseURL, APIkey, headers)
           │
           ▼
  ┌────────────────────┐   build upstream request from rute request + connection
  │  PROVIDER adapter  │   ProviderMap[provider]                      [TO BUILD]
  └────────┬───────────┘
           ▼
  ┌────────────────────┐
  │  REVERSE PROXY /   │──────────► UPSTREAM (api.openai.com ...)     [TO BUILD]
  │  TRANSPORT         │◄──────────  response / SSE stream
  └────────┬───────────┘
           ▼
  RunPostProvider / PostCall hooks  → usage, budget decrement, audit
           │
           ▼
     response back to client
```

**One line:** *auth decides if you're allowed → ModelRegistry gives candidate deployments →
LB picks one → its APIRegistryID resolves the real connection → provider adapter + proxy send upstream.*

The left column (server → hooks → model registry → api registry) is the **control/lookup path — already built**.
The three `[TO BUILD]` boxes (LB, provider adapter, reverse proxy) are the **data path — still empty**.

---

## 4. Component status

| Layer | Package | State |
|---|---|---|
| Core types | `internal/core` | ✅ Solid |
| DB store | `internal/dbstore` | ✅ Solid (encrypt on `BeforeSave`) |
| Model registry (deployments + COW + state) | `internal/registry/models.go` | ✅ Advanced |
| API registry (connections by ID) | `internal/registry/apiregistry.go` | ✅ Built |
| Governance (vkeys/budget/hooks) | `internal/governance` | 🟡 Built, `AuthHook` **not registered yet** |
| Syncer (bg flush jobs) | `internal/syncer` | ✅ Built (no registry-reload job yet) |
| Observability | `internal/observability` | ✅ Built |
| **Data plane** (LB + reverse proxy + health) | `internal/dataplane` | ❌ Empty |
| **Providers** (adapter + openai + ProviderMap) | `internal/providers` | ❌ Empty |
| **Server** (genericHandler + routes + admin CRUD) | `internal/server` | ❌ Empty |
| **Gateway** (composition root) | `internal/gateway` | ❌ Empty |
| Entrypoint | `main.go` | ❌ Empty |

**Build order to boot:** dataplane → providers → server → gateway → main.go → tests.

---

## 5. Reload wiring (methods exist, callers don't)

Admin CRUD write → DB → then update **both** registries together (joined by ID):

- `apiRegistry.UpsertToAPIRegistry(conn)` / `RemoveFromAPIRegistry(id)`
- `modelRegistry.SyncDeployments(apiDetails)` / `RemoveDeployments(id)`

Both move together on every credential/model change so the connection and its deployments stay consistent.

---

## 6. Two-level routing & weighted pools

Routing happens in **two independent layers**. Confusing them is the classic mistake.

```
LEVEL 1 — POOL selection  (only on VKModelPool mode)     "which model?"
   pool.LBType decides the strategy across DIFFERENT models:
     LBWeight      → weighted pick using AllowedModel.Weight
     LBRoundRobin  → rotate across models
     LBDirect      → first model
        ↓ yields ONE core.ModelKey (e.g. {anthropic, claude-fable})
LEVEL 2 — DEPLOYMENT LB   (both modes)                    "which key serves it?"
   Balancer.Pick(deployments, kind) across the KEYS of that one model
     default: LBLeastConnection
```

**Weight lives at the POOL level, not the deployment level.** Weighting across keys of the *same* model is meaningless; weighting across *different* models (`azure/gpt-5 40% / anthropic/claude-fable 50% / other 10%`) is the real use case. So `LBWeight` is a `pool.LBType`, **not** a `Balancer.Pick` case.

### Data model (verified — already supported)

```go
type AllowedModel struct {          // core/virtualkey.go
	Provider string
	Model    string
	Weight   int   // ← per-model weight, persisted on the pool (type:json)
}
// ModelPool.AllowedModels []AllowedModel  +  ModelPool.LBType (LBkind)
```

Storage + load (`LookupPool`) support this today. The **runtime selection is not built yet** — the `VKModelPool` handler branch is empty.

### Pool resolution step (to build — illustrative)

In pool mode the request's "model" field is the **pool name**. Handler flow:

```go
pool, _ := modelRegistry.LookupPool(rctx.Model)   // rctx.Model = pool name here
mk      := pickModel(pool)                         // Level 1: weighted → ONE ModelKey
deps, _ := modelRegistry.LookupModel(mk)           // keys serving that model
chosen, err := balancer.Pick(deps, core.LBLeastConnection)   // Level 2: key-level LB
```

Weighted pick (stateless weighted-random, normalized by total so 40/50/10 and 4/5/1 both work):

```go
func pickModel(pool *core.ModelPool) core.ModelKey {
	total := 0
	for _, m := range pool.AllowedModel { if m.Weight > 0 { total += m.Weight } }
	if total == 0 { /* fall back: round-robin or first */ }
	r := rand.Intn(total)
	for _, m := range pool.AllowedModel {
		if m.Weight <= 0 { continue }
		if r -= m.Weight; r < 0 {
			return core.ModelKey{Provider: core.Provider(m.Provider), ModelName: m.Model}
		}
	}
	// unreachable if total>0
}
```

**Fallback:** if the weighted-chosen model has no healthy deployments, re-roll over the remaining models (renormalize) before failing with 503.

---

## 7. In-flight lifecycle & reload safety

**Golden rule: pick once, pin to the context, never re-resolve mid-request.**

```go
// on DiffractLLMContext (rctx) — reset on pool Release
Deployment *core.Deployment

// handler:  chosen, _ := balancer.Pick(...); rctx.Deployment = chosen   // PIN
// proxy:    rctx.Deployment.State.AddConnection()
//           defer rctx.Deployment.State.RemoveConnection()              // after FULL completion
```

- **Connection count = the traffic gauge.** `activeConnections` (already on `DeploymentState`) is what least-connection reads. Bracket `AddConnection`/`RemoveConnection` around the *real* dispatch.
- **Streaming:** `RemoveConnection` must fire when the SSE loop exits (stream drained), **not** when headers are sent — else a slow-streaming key looks idle and gets overloaded.
- **Latency:** not needed for v1 routing — least-connection is *implicitly* latency-aware (a slow key accumulates connections → gets less traffic). Explicit latency EWMA + P2C is a v2 enhancement. Metrics-latency (p50/p95) belongs in the PostProvider hook, **not** the LB.

### What happens if the registry reloads under an in-flight request — SAFE by construction

COW snapshots + Go GC + `StateManager.Acquire` returning existing pointers means no teardown/refcounting is needed:

- **Model survives reload:** `SyncDeployments` builds a new `*Deployment`, but `Acquire(id)` returns the **same `*DeploymentState`** → in-flight and new requests share one live counter. Consistent. ✅
- **Model removed:** `Release(id)` deletes the map entry; the in-flight request still holds its own valid pointer (GC-alive); `RemoveConnection` decrements the orphan harmlessly; object collected when the request ends. ✅
- **Edge (accept, don't fix):** same `id` removed+re-added while in-flight → new fresh state (count 0) while old request decrements the orphan → briefly off-by-one count. Rare, never panics/leaks.

Same rule for the connection: resolve `*ModelAPIRegistry` **once** after the pick; a mid-flight credential rotation correctly affects only *new* requests.

---

## 8. Add / Update / Delete orchestration (the missing glue → server layer)

The registry methods exist; the **handler that sequences them does not** (server is empty). Contract:

```
DB is source of truth → write row first; if it fails, DON'T touch registries.
Then update BOTH registries together (joined by APIRegistryID):
```

| Operation | 1. DB | 2. APIRegistry | 3. ModelRegistry |
|---|---|---|---|
| Add key | Create | `UpsertToAPIRegistry` | `SyncDeployments` |
| Update key (add/remove models) | Update | `UpsertToAPIRegistry` | `SyncDeployments` (incremental — releases dropped models' states, keeps survivors') |
| Delete key | Delete | `RemoveFromAPIRegistry` | `RemoveDeployments` (releases all its states) |
| Pool add/update/delete | Create/Update/Delete | — | `AddPool` / `UpdatePool` / `DeletePool` |

**Worked example — update `reg-azure-1` from `[gpt-4o, gpt-35-turbo]` → `[gpt-4o]`:**
`SyncDeployments` deletes the `{azure, gpt-35-turbo}` bucket and `Release`s its state, **keeps `{azure, gpt-4o}` with the same state object** (in-flight `gpt-4o` requests unaffected), and never touches other keys. Verified against `SyncDeployments` logic.

---

## 9. Data plane — full implementation (`internal/dataplane`)

> Copy-paste ready. Two responsibilities, cleanly separated:
> **key-level LB** (`Balancer`, stateful for round-robin) and **pool-level weighted model
> selection** (pure function). Neither imports the registry — the handler wires them (§ `arch-flow.md`).

### 9.1 `internal/dataplane/loadbalancer.go`

```go
package dataplane

import (
	"diffractllm/internal/core"
	"errors"
	"sync"
	"sync/atomic"
)

var (
	// ErrNoDeployments — the model has no configured deployments at all
	// (empty candidate list: a registry/lookup problem → map to 404).
	ErrNoDeployments = errors.New("dataplane: no deployments available for model")

	// ErrNoHealthyDeployment — every candidate is dead or circuit-tripped
	// (→ map to 503).
	ErrNoHealthyDeployment = errors.New("dataplane: all deployments are unhealthy or tripped")
)

// Balancer selects ONE key/deployment from a single model's candidate set.
// Safe for concurrent use. Its only mutable state is per-model round-robin
// cursors, kept here (NOT in the registry snapshot) so they survive COW reloads.
type Balancer struct {
	rrCursors sync.Map // core.ModelKey -> *atomic.Uint64
}

func NewBalancer() *Balancer { return &Balancer{} }

// Pick returns one healthy deployment for the given strategy.
//
// Caller contract (server/proxy — NOT done here):
//   chosen.State.AddConnection()
//   defer chosen.State.RemoveConnection()   // for streams: after the stream fully drains
func (b *Balancer) Pick(deployments []*core.Deployment, kind core.LBkind) (*core.Deployment, error) {
	if len(deployments) == 0 {
		return nil, ErrNoDeployments
	}

	healthy := filterHealthy(deployments)
	if len(healthy) == 0 {
		return nil, ErrNoHealthyDeployment
	}
	if len(healthy) == 1 {
		return healthy[0], nil
	}

	switch kind {
	case core.LBRoundRobin:
		return b.roundRobin(healthy), nil
	case core.LBLeastConnection:
		return leastConnection(healthy), nil
	case core.LBWeight:
		// Weight is a POOL-level concept (§6). At key level it is meaningless,
		// so fall back to least-connection instead of a silent no-op.
		return leastConnection(healthy), nil
	case core.LBDirect:
		fallthrough
	default:
		return healthy[0], nil
	}
}

// filterHealthy keeps deployments whose breaker is closed and alive.
// Nil-guards both the deployment and its state so the data path never panics.
func filterHealthy(deployments []*core.Deployment) []*core.Deployment {
	healthy := make([]*core.Deployment, 0, len(deployments))
	for _, d := range deployments {
		if d != nil && d.State != nil && d.State.IsHealthy() {
			healthy = append(healthy, d)
		}
	}
	return healthy
}

// leastConnection returns the deployment with fewest active in-flight requests.
// Ties resolve to the earliest in snapshot order (stable).
func leastConnection(deployments []*core.Deployment) *core.Deployment {
	best := deployments[0]
	least := best.State.GetConnections()
	for _, d := range deployments[1:] {
		if c := d.State.GetConnections(); c < least {
			best, least = d, c
		}
	}
	return best
}

// roundRobin rotates across candidates. Cursor is keyed by ModelKey (all
// candidates in a bucket share it) and increments monotonically; modulo maps
// it onto the current healthy slice.
func (b *Balancer) roundRobin(deployments []*core.Deployment) *core.Deployment {
	key := deployments[0].Key()
	v, _ := b.rrCursors.LoadOrStore(key, new(atomic.Uint64))
	cursor := v.(*atomic.Uint64)
	n := cursor.Add(1) - 1
	return deployments[n%uint64(len(deployments))]
}
```

### 9.2 `internal/dataplane/poolrouter.go`

```go
package dataplane

import (
	"diffractllm/internal/core"
	"math/rand"
)

// WeightedModel selects one (provider, model) target from a pool's members,
// proportional to each member's Weight. Normalized by total, so weights of
// {40,50,10} and {4,5,1} distribute identically. Stateless / safe for concurrent use.
//
// Returns ok=false only when the pool has no members. If members exist but all
// weights are <= 0, it degrades to uniform-random over the members (never fails).
func WeightedModel(models []core.AllowedModel) (core.ModelKey, bool) {
	if len(models) == 0 {
		return core.ModelKey{}, false
	}

	total := 0
	for i := range models {
		if models[i].Weight > 0 {
			total += models[i].Weight
		}
	}

	// No usable weights → uniform random.
	if total == 0 {
		m := models[rand.Intn(len(models))]
		return core.ModelKey{Provider: core.Provider(m.Provider), ModelName: m.Model}, true
	}

	r := rand.Intn(total)
	for i := range models {
		w := models[i].Weight
		if w <= 0 {
			continue
		}
		if r -= w; r < 0 {
			m := models[i]
			return core.ModelKey{Provider: core.Provider(m.Provider), ModelName: m.Model}, true
		}
	}

	// Unreachable when total > 0 (loop always consumes r); defensive fallback.
	m := models[len(models)-1]
	return core.ModelKey{Provider: core.Provider(m.Provider), ModelName: m.Model}, true
}

// FirstModel returns the first pool member — used for pool.LBType == LBDirect.
func FirstModel(models []core.AllowedModel) (core.ModelKey, bool) {
	if len(models) == 0 {
		return core.ModelKey{}, false
	}
	m := models[0]
	return core.ModelKey{Provider: core.Provider(m.Provider), ModelName: m.Model}, true
}
```

> **Pool-level round-robin (`pool.LBType == LBRoundRobin`) is deferred.** It needs a
> cursor keyed by pool ID; for v1 the handler treats it as weighted (or uniform if
> unweighted). Implement it later mirroring `Balancer.roundRobin` with a pool-ID cursor.

### 9.3 `internal/dataplane/loadbalancer_test.go`

```go
package dataplane

import (
	"diffractllm/internal/core"
	"testing"
)

func dep(id string, alive int32, conns int32) *core.Deployment {
	st := &core.DeploymentState{}
	st.SetAliveState(alive)
	for i := int32(0); i < conns; i++ {
		st.AddConnection()
	}
	return &core.Deployment{
		ID:            id,
		ModelProvider: core.ProviderOpenAI,
		ModelName:     "gpt-4",
		APIRegistryID: id,
		State:         st,
	}
}

func TestPick_Empty(t *testing.T) {
	b := NewBalancer()
	if _, err := b.Pick(nil, core.LBDirect); err != ErrNoDeployments {
		t.Fatalf("want ErrNoDeployments, got %v", err)
	}
}

func TestPick_AllUnhealthy(t *testing.T) {
	b := NewBalancer()
	in := []*core.Deployment{dep("a", 0, 0), dep("b", 0, 0)}
	if _, err := b.Pick(in, core.LBLeastConnection); err != ErrNoHealthyDeployment {
		t.Fatalf("want ErrNoHealthyDeployment, got %v", err)
	}
}

func TestPick_SkipsUnhealthy(t *testing.T) {
	b := NewBalancer()
	in := []*core.Deployment{dep("dead", 0, 0), dep("live", 1, 5)}
	got, err := b.Pick(in, core.LBLeastConnection)
	if err != nil || got.ID != "live" {
		t.Fatalf("want live, got %v (err %v)", got, err)
	}
}

func TestPick_LeastConnection(t *testing.T) {
	b := NewBalancer()
	in := []*core.Deployment{dep("busy", 1, 10), dep("idle", 1, 1), dep("mid", 1, 5)}
	if got, _ := b.Pick(in, core.LBLeastConnection); got.ID != "idle" {
		t.Fatalf("want idle, got %s", got.ID)
	}
}

func TestPick_RoundRobin(t *testing.T) {
	b := NewBalancer()
	in := []*core.Deployment{dep("a", 1, 0), dep("b", 1, 0), dep("c", 1, 0)}
	want := []string{"a", "b", "c", "a", "b", "c"}
	for i := range want {
		if got, _ := b.Pick(in, core.LBRoundRobin); got.ID != want[i] {
			t.Fatalf("rr[%d] = %s, want %s", i, got.ID, want[i])
		}
	}
}

func TestPick_DirectFirstHealthy(t *testing.T) {
	b := NewBalancer()
	in := []*core.Deployment{dep("dead", 0, 0), dep("first", 1, 9), dep("second", 1, 0)}
	if got, _ := b.Pick(in, core.LBDirect); got.ID != "first" {
		t.Fatalf("want first healthy, got %s", got.ID)
	}
}

func TestWeightedModel_Distribution(t *testing.T) {
	models := []core.AllowedModel{
		{Provider: "azure", Model: "gpt-5", Weight: 40},
		{Provider: "anthropic", Model: "claude-fable", Weight: 50},
		{Provider: "openai", Model: "gpt-4o", Weight: 10},
	}
	counts := map[string]int{}
	const N = 100000
	for i := 0; i < N; i++ {
		mk, ok := WeightedModel(models)
		if !ok {
			t.Fatal("want ok")
		}
		counts[mk.SlashKey()]++
	}
	// ~40/50/10 with generous tolerance for randomness.
	check := func(key string, wantPct float64) {
		got := float64(counts[key]) / N * 100
		if got < wantPct-3 || got > wantPct+3 {
			t.Errorf("%s = %.1f%%, want ~%.0f%%", key, got, wantPct)
		}
	}
	check("azure/gpt-5", 40)
	check("anthropic/claude-fable", 50)
	check("openai/gpt-4o", 10)
}

func TestWeightedModel_Empty(t *testing.T) {
	if _, ok := WeightedModel(nil); ok {
		t.Fatal("want ok=false on empty")
	}
}
```

### 9.4 What this package deliberately does NOT do

- **No I/O, no HTTP, no registry import** — pure selection. The reverse proxy/transport is a
  *separate* dataplane file (next block).
- **No connection accounting** — `AddConnection`/`RemoveConnection` are the caller's job,
  because only the caller knows when the request (or stream) actually finishes.
- **No pool-name → deployment glue** — that needs the registry, so it lives in the handler
  (see `arch-flow.md §4`).

