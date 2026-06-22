# diffractLLM — Architecture & Design

> A lightweight, key-gated LLM gateway built for read-heavy dispatch at extreme QPS.
> The virtual key is both the **gate** (auth) and the **router** (model selection).
> There is no separate policy engine, no route table, no snapshot wrapper.

---

## 1. Design Principles

1. **Lock-free on the hot path.** The request path never takes a mutex and never
   touches a map that can be written concurrently. Reads ride immutable data behind an
   `atomic.Pointer`; per-deployment mutable counters are plain atomic fields on a stable cell.
2. **One source of truth per concern.** Model topology, deployment health, and credentials
   are three separate stores with three different lifecycles — never duplicated, never merged.
3. **State survives reload.** A config reload swaps the model topology but must NOT reset live
   health/connection counters or re-fetch warm connections. State and credentials are keyed by
   stable deployment ID and live *outside* the swappable topology.
4. **Credentials by reference.** Secrets are resolved to a `*Credential` pointer at load time and
   carried on the deployment. They are never serialized into API responses, logs, or the registry snapshot.
5. **Reversible decisions.** Components are split by responsibility so any one can be replaced
   (e.g. extract the health worker, swap the LB) without touching the hot path.

---

## 2. Top-Level Composition

The application is a flat composition of four pointer-held components. They are **pointers**, not
values: each holds a mutex or an atomic, and copying a lock is a correctness bug (`go vet copylocks`).

```go
package app

type App struct {
	Registry   *core.ModelRegistry   // model topology — lock-free reads, COW writes
	Vault      *core.VaultRegistry   // deploymentID -> *Credential (secrets, by reference)
	State      *core.StateManager    // deploymentID -> *DeploymentState (health/conns)
	Health     *health.HealthChecker // active probe worker (writes into State cells)
	Server     *server.Server        // HTTP front door + handler
}
```

**Ownership map**

| Component       | Owns                                   | Lifecycle           | Hot path? |
|-----------------|----------------------------------------|---------------------|-----------|
| `ModelRegistry` | `ModelKey -> []*Deployment` (immutable)| swap on reload/add  | **yes** (read) |
| `StateManager`  | `ID -> *DeploymentState` cells         | get-or-create / sweep | no (cells reached via pointer) |
| `VaultRegistry` | `ID -> *Credential`                    | rotate / reconcile  | no (cred reached via pointer) |
| `HealthChecker` | `[]*Deployment` probe targets + ticker | refresh on reload   | no (background) |
| `Server`        | listener, router, RuteContext pool     | process lifetime    | yes |

The critical wiring: a `Deployment` returned by a registry lookup **already carries** `State` and
`Credential` as resolved pointers. So the hot path follows pointers — it never queries the State map
or the Vault map.

---

## 3. Core Domain Types

### 3.1 Provider

```go
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderAzure     Provider = "azure"
	ProviderCohere    Provider = "cohere"
	ProviderOllama    Provider = "ollama"
	ProviderCustom    Provider = "custom"
)
```

### 3.2 ModelKey — the universal runtime lookup key

Not the DB UUID, not a computed hash. The pair the caller actually sends (`provider` + `model`).

```go
type ModelKey struct {
	Provider  Provider
	ModelName string
}

func (m ModelKey) SlashKey() string { return string(m.Provider) + "/" + m.ModelName }
```

### 3.3 Credential — held by reference, never serialized

```go
type Credential struct {
	APIkey       string
	APIProvider  Provider
	CustomHeader string // header name override; "" => provider default (e.g. Authorization)
}
```

### 3.4 Deployment — one physical model row, with joined pointers

A `Deployment` is one routable upstream: a provider/model at an endpoint, with a credential and a
state cell. The `Credential` and `State` are wired at load time from the Vault and StateManager.

```go
type Deployment struct {
	ID             string      // stable DB UUID — identity across reloads
	ModelProvider  Provider
	ModelName      string
	BaseModelName  string      // upstream's real model id (what we send on the wire)
	Endpoint       string
	Timeout        int

	Credential *Credential      // resolved from Vault (by reference)
	State      *DeploymentState // resolved from StateManager (survives reload)

	HealthCheck    bool          // opt-in active probing
	HealthEndpoint string
}

func (d *Deployment) Key() ModelKey {
	return ModelKey{Provider: d.ModelProvider, ModelName: d.ModelName}
}
```

---

## 4. ModelRegistry — lock-free, copy-on-write topology

The registry answers the only hot-path question: *"give me the deployments for this `ModelKey`."*
At billions of reads/sec, the read path must not contend. It does so by holding an **immutable**
snapshot behind an `atomic.Pointer`. Reads do one atomic load + a map read on a map that is never
mutated. Writes build a new snapshot and atomic-swap it; a write-side mutex serializes writers only —
readers never touch it.

Why not `sync.RWMutex`? An `RLock` is an atomic RMW on a shared lock word; under massive read
concurrency that word ping-pongs across core caches and stalls. Why not `sync.Map`? It is built for
key-churn, boxes values into interfaces, and allocates per read. Our workload is read-billions /
write-rarely on whole-slice values — the textbook RCU (read-copy-update) case.

```go
package core

import (
	"sync"
	"sync/atomic"
)

// snapshot is immutable. Once published it is never mutated.
type snapshot struct {
	lookup      map[ModelKey][]*Deployment
	Deployments []*Deployment
}

func buildSnapshot(deployments []*Deployment) *snapshot {
	lookup := make(map[ModelKey][]*Deployment, len(deployments))
	for _, d := range deployments {
		k := d.Key()
		lookup[k] = append(lookup[k], d)
	}
	return &snapshot{lookup: lookup, Deployments: deployments}
}

type ModelRegistry struct {
	snap    atomic.Pointer[snapshot]
	writeMu sync.Mutex // serializes writers only; the read path never touches it
}

func NewModelRegistry(deployments []*Deployment) *ModelRegistry {
	m := &ModelRegistry{}
	m.snap.Store(buildSnapshot(deployments))
	return m
}

// Lookup — the hot path. Lock-free, no allocation on a miss.
func (m *ModelRegistry) Lookup(key ModelKey) ([]*Deployment, bool) {
	d, ok := m.snap.Load().lookup[key]
	return d, ok
}

// Add — copy-on-write a single deployment in: shallow-copy the map spine (other keys
// share their slices), fresh slice only for the touched key, atomic swap. No DB re-read,
// no full rebuild. 10 sequential adds = 10 cheap µs copies, not 10 DB rebuilds.
func (m *ModelRegistry) Add(d *Deployment) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	old := m.snap.Load()
	key := d.Key()

	next := make(map[ModelKey][]*Deployment, len(old.lookup)+1)
	for k, v := range old.lookup {
		next[k] = v // share every untouched key's slice
	}
	cur := old.lookup[key]
	cp := make([]*Deployment, len(cur)+1) // fresh slice so we never mutate the old backing array
	copy(cp, cur)
	cp[len(cp)-1] = d
	next[key] = cp

	deps := make([]*Deployment, len(old.Deployments)+1)
	copy(deps, old.Deployments)
	deps[len(deps)-1] = d

	m.snap.Store(&snapshot{lookup: next, Deployments: deps})
}

// Replace — swap the entire table. The reload-from-DB path.
func (m *ModelRegistry) Replace(deployments []*Deployment) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.snap.Store(buildSnapshot(deployments))
}
```

**Memory note:** a copy-on-write `Add` copies the map *spine* (one bucket array + N slice headers),
not the deployments themselves. Old readers keep reading the old snapshot until they return; it is
GC'd once no reader holds it. This is correct and cheap.

---

## 5. StateManager — the state store (no goroutine)

`StateManager` owns the lifecycle of `DeploymentState` cells, keyed by stable deployment ID. It is a
**store**, not a worker: no HTTP client, no ticker, no `Start/Stop`. Its map is cold-path — touched
only at load / reload / sweep, never on the request path (the request reaches a cell via
`deployment.State`). A plain `map + RWMutex` is the right primitive; `Acquire` is a get-or-create,
which a COW swap would make awkward and which has nothing to optimize away off the hot path.

```go
package core

import (
	"sync"
	"sync/atomic"
	"time"
)

// DeploymentState — a stable mutable cell. NEVER swapped; its fields are mutated in place
// via atomics by three writers: the request path (conns/fails/trip), the passive breaker,
// and the active health checker (alive). The address is pinned for the deployment's life.
type DeploymentState struct {
	Alive            atomic.Uint32 // 1 = serving, 0 = down
	ActiveConns      atomic.Int64  // in-flight requests (least-conn LB + drain)
	ConsecutiveFails atomic.Uint32 // passive breaker counter
	TripUntil        atomic.Int64  // unix-nano; >now => circuit open (passive)
	Checking         atomic.Uint32 // CAS guard: 1 = a probe is in flight (no pile-up)
	lastSeen         atomic.Int64  // unix-nano; updated on Acquire — used by Sweep
}

// Routable reports whether this deployment may receive traffic right now.
// Passive breaker + liveness. now is unix-nano (pass time.Now().UnixNano()).
func (s *DeploymentState) Routable(now int64) bool {
	if s.Alive.Load() == 0 {
		return false
	}
	if t := s.TripUntil.Load(); t > now {
		return false // circuit open
	}
	return true
}

type StateManager struct {
	mu     sync.RWMutex
	states map[string]*DeploymentState // deploymentID -> cell
}

func NewStateManager() *StateManager {
	return &StateManager{states: make(map[string]*DeploymentState)}
}

// Acquire returns the existing cell for id, or creates one. Called at load/reload for every
// deployment so the SAME cell is re-wired to the freshly-built *Deployment — health and conns
// survive the registry swap. healthChecked seeds Alive: probed deployments start down (0) until
// the first successful probe; un-probed deployments start alive (1) and rely on the passive breaker.
func (m *StateManager) Acquire(id string, healthChecked bool) *DeploymentState {
	now := time.Now().UnixNano()

	m.mu.RLock()
	if s, ok := m.states[id]; ok {
		m.mu.RUnlock()
		s.lastSeen.Store(now)
		return s
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[id]; ok { // re-check after upgrade
		s.lastSeen.Store(now)
		return s
	}
	s := &DeploymentState{}
	if !healthChecked {
		s.Alive.Store(1)
	}
	s.lastSeen.Store(now)
	m.states[id] = s
	return s
}

// Sweep drops cells not seen within grace (deployments removed from config). Call after a reload
// has Acquired all current deployments, so anything stale has an old lastSeen.
func (m *StateManager) Sweep(grace time.Duration) {
	cutoff := time.Now().Add(-grace).UnixNano()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.states {
		if s.lastSeen.Load() < cutoff {
			delete(m.states, id)
		}
	}
}
```

---

## 6. VaultRegistry — credentials by reference

Separate from StateManager by **lifecycle and sensitivity**: secrets rotate independently, carry
expiry/age, must be auditable, and must never land in the registry snapshot or logs. Same cold-path
shape as StateManager — `map + RWMutex`, resolved at load.

```go
package core

import (
	"sync"
	"time"
)

type VaultEntry struct {
	Credential *Credential
	ExpiresAt  time.Time // zero = no expiry
	CreatedAt  time.Time
}

type VaultRegistry struct {
	mu      sync.RWMutex
	entries map[string]*VaultEntry // credentialID -> entry
}

func NewVaultRegistry() *VaultRegistry {
	return &VaultRegistry{entries: make(map[string]*VaultEntry)}
}

// Resolve returns the current *Credential for a credentialID. Called at load to wire a
// Deployment; the deployment then holds the pointer and never re-queries the vault.
func (v *VaultRegistry) Resolve(credentialID string) (*Credential, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	e, ok := v.entries[credentialID]
	if !ok {
		return nil, false
	}
	return e.Credential, true
}

// Upsert installs/rotates a credential. Rotation replaces one entry under the write lock.
func (v *VaultRegistry) Upsert(credentialID string, c *Credential, expiresAt time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.entries[credentialID] = &VaultEntry{Credential: c, ExpiresAt: expiresAt, CreatedAt: time.Now()}
}
```

> If lock-free credential reads *during* a live rotation ever matter, make `Deployment.Credential`
> an `atomic.Pointer[Credential]` (a per-cell swap, not a map swap). Not needed now.

---

## 7. HealthChecker — the active probe worker

A worker, not a store. It holds the probe target list + a ticker + an HTTP client, and writes results
**into the StateManager's cells** (`d.State.Alive`). It does not own state — one source of truth.
This mirrors the existing alphaX `dataplane.HealthChecker`, with one upgrade: the target list is
**reload-aware** (swapped behind an `atomic.Pointer`), because a registry `Replace` produces new
`*Deployment` wrappers even though the `State` cells survive.

```go
package health

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"diffractllm/internal/core"
)

type HealthChecker struct {
	targets  atomic.Pointer[[]*core.Deployment] // only HealthCheck==true; swapped on reload
	client   *http.Client
	interval time.Duration
	stopCh   chan struct{}

	failThreshold    uint32
	successThreshold uint32
}

func NewHealthChecker(interval time.Duration, failN, successN uint32) *HealthChecker {
	hc := &HealthChecker{
		client:           &http.Client{Timeout: 30 * time.Second},
		interval:         interval,
		stopCh:           make(chan struct{}),
		failThreshold:    failN,
		successThreshold: successN,
	}
	empty := make([]*core.Deployment, 0)
	hc.targets.Store(&empty)
	return hc
}

// SetTargets is called at startup AND after every reload with the current health-enabled deployments.
func (hc *HealthChecker) SetTargets(deployments []*core.Deployment) {
	t := make([]*core.Deployment, 0, len(deployments))
	for _, d := range deployments {
		if d.HealthCheck {
			t = append(t, d)
		}
	}
	hc.targets.Store(&t)
}

func (hc *HealthChecker) Start() {
	ticker := time.NewTicker(hc.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-hc.stopCh:
				return
			case <-ticker.C:
				for _, d := range *hc.targets.Load() { // lock-free read of current targets
					go hc.probe(d)
				}
			}
		}
	}()
}

func (hc *HealthChecker) Stop() { close(hc.stopCh) }

func (hc *HealthChecker) probe(d *core.Deployment) {
	// CAS guard: skip if a probe for this deployment is already running (no pile-up).
	if !d.State.Checking.CompareAndSwap(0, 1) {
		return
	}
	defer d.State.Checking.Store(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ok := false
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.HealthEndpoint, nil); err == nil {
		if resp, err := hc.client.Do(req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			ok = resp.StatusCode >= 200 && resp.StatusCode < 400
		}
	}
	hc.record(d.State, ok)
}

// record updates the SAME cell the request path and passive breaker read. All atomic, no lock.
func (hc *HealthChecker) record(s *core.DeploymentState, ok bool) {
	if ok {
		s.ConsecutiveFails.Store(0)
		if s.Alive.Load() == 0 {
			s.Alive.Store(1) // recovered
		}
		return
	}
	if s.ConsecutiveFails.Add(1) >= hc.failThreshold {
		s.Alive.Store(0) // tripped down
	}
}
```

---

## 8. Governance — VirtualKey as the router

The virtual key is the gate *and* the router. It has two modes:

```go
type VKMode uint8

const (
	VKAllowedModel VKMode = iota // Mode 1: direct dispatch to an explicit model. NO load balancer.
	VKModelPool                  // Mode 2: load-balance across the members of a pool.
)

const (
	VK_ALLOWED_MODEL = "allowed_model"
	VK_MODEL_POOL    = "model_pool"
)

func (mode VKMode) String() string { /* allowed_model | model_pool */ }
func ParseVKMode(mode string) VKMode { /* inverse */ }
```

**Admin-plane shape (DTO):**

```go
type VirtualKey struct {
	ClientID      string     `json:"client_id"      binding:"required"`
	BudgetID      string     `json:"budget_id"      binding:"required"`
	ExpiresAt     *time.Time `json:"expires_at"`
	Mode          string     `json:"mode"           binding:"required"`
	AllowedModels []string   `json:"allowed_models"` // Mode 1: provider/model refs
	Pools         []string   `json:"pools"`          // Mode 2: pool names
}
```

**Runtime shape** (what the auth step materializes onto the request context):

- Mode 1 → `AllowedModels` becomes `map[ModelKey]struct{}` — O(1) membership check.
- Mode 2 → `Pools` becomes `map[string]struct{}` — O(1) pool membership check.

Maps for runtime (lookups), slices for the API (stable JSON). Keep the two representations distinct.

---

## 9. Data Plane — load balancing (Mode 2 only)

```go
type LBkind uint8

const (
	LBDirect LBkind = iota // single member / first-healthy — no balancing
	LBRoundRobin
	LBLeastConnection
)

const (
	LB_DIRECT      = "direct"
	LB_ROUND_ROBIN = "round_robin"
	LB_LEAST_CONN  = "least_connection"
)

func (LB LBkind) String() string  { /* direct | round_robin | least_connection */ }
func ParseLBKind(LB string) LBkind { /* inverse, defaults to LBDirect */ }
```

**Selection contract** — the LB picks among the *routable* members of a pool:

```go
// Select returns the chosen deployment, or false if none is routable.
type LoadBalancer interface {
	Select(members []*Deployment, now int64) (*Deployment, bool)
}
```

- `LeastConnection` reads `d.State.ActiveConns.Load()` and picks the minimum among `Routable` members.
- `RoundRobin` keeps an `atomic.Uint64` cursor and skips non-`Routable` members.
- `Direct` returns the first `Routable` member.

Mode 1 does **not** use a LoadBalancer — it picks the first `Routable` deployment in declared order
directly in the handler. No LB object is constructed on that path.

---

## 10. Server & Gateway

### 10.1 RuteContext — pooled per-request state

A `sync.Pool`-recycled context carrying request, parsed routing intent, materialized governance, and
the chosen target. Reset on release; no per-request allocation in steady state.

```go
type RuteContext struct {
	ctx       context.Context
	Request   *http.Request
	BodyBytes []byte
	Writer    http.ResponseWriter

	// parsed intent
	Provider Provider
	Model    string

	// materialized governance (from the virtual key)
	ClientID      string
	Mode          core.VKMode
	AllowedModels map[core.ModelKey]struct{}
	Pools         map[string]struct{}
	VirtualKeyID  string

	// chosen target
	Target *core.Deployment

	aborted atomic.Bool
}
```

### 10.2 Server

```go
type Server struct {
	registry *core.ModelRegistry
	state    *core.StateManager
	auth     *AuthService     // resolves bearer -> materialized VirtualKey
	pool     *RuteContextPool
	proxy    *Proxy           // pure forwarder
	http     *http.Server
}
```

The proxy is a **pure forwarder**: given a chosen `*Deployment` it sets exactly one auth header from
`Target.Credential` and streams the request upstream. Selection happens in the handler, not the proxy.

---

## 11. Request Flow

```
                 ┌─────────────────────────────────────────────┐
  HTTP request → │ 1. Acquire RuteContext (pool)               │
                 │ 2. Parse intent → Provider + Model          │
                 │ 3. AUTH (top): bearer → VirtualKey          │  ← reject early, before any routing work
                 │      materialize Mode + AllowedModels/Pools │
                 └───────────────┬─────────────────────────────┘
                                 │
              ┌──────────────────┴───────────────────┐
       Mode 1 (VKAllowedModel)              Mode 2 (VKModelPool)
       ─────────────────────────            ─────────────────────────
       a. key ∈ AllowedModels?              a. resolve pool(s) for key
          no → 403                          b. gather members []*Deployment
       b. Lookup(ModelKey) → []*Deployment  c. LB.Select(members, now)
       c. first Routable(now) in order         (Routable filter inside)
          none → 503                            none → 503
                                 │
                                 ▼
                 ┌─────────────────────────────────────────────┐
                 │ 4. Target.State.ActiveConns.Add(1)          │
                 │ 5. Proxy: one Header.Set(Target.Credential) │
                 │    stream upstream (BaseModelName on wire)  │
                 │ 6. defer ActiveConns.Add(-1); passive       │
                 │    breaker on failure (ConsecutiveFails,    │
                 │    TripUntil)                               │
                 │ 7. Release RuteContext                      │
                 └─────────────────────────────────────────────┘
```

Key properties:
- **Auth is at the top.** No routing, LB, or topology work happens for an unauthorized request.
- **Mode 1 has no LB.** First-healthy-in-declared-order; zero LB allocation.
- **Hot path is pointer-only.** `Lookup` (lock-free) → `d.State` (atomic fields) → `d.Credential`
  (deref). No map writes, no mutex, on the request path.

---

## 12. Load & Reload Wiring

The sequence that makes "state survives reload" true. The registry swaps; State and Vault persist by ID.

```go
func (a *App) Reload(rows []DeploymentRow) error {
	deployments := make([]*core.Deployment, 0, len(rows))
	for _, r := range rows {
		cred, ok := a.Vault.Resolve(r.CredentialID) // by reference — secret never copied into topology
		if !ok {
			return fmt.Errorf("credential %s missing for deployment %s", r.CredentialID, r.ID)
		}
		d := &core.Deployment{
			ID:             r.ID,
			ModelProvider:  r.Provider,
			ModelName:      r.ModelName,
			BaseModelName:  r.BaseModelName,
			Endpoint:       r.Endpoint,
			Timeout:        r.Timeout,
			Credential:     cred,
			State:          a.State.Acquire(r.ID, r.HealthCheck), // SAME cell re-wired → conns/health survive
			HealthCheck:    r.HealthCheck,
			HealthEndpoint: r.HealthEndpoint,
		}
		deployments = append(deployments, d)
	}

	a.Registry.Replace(deployments)   // atomic swap of the immutable topology
	a.Health.SetTargets(deployments)  // refresh probe targets (new wrappers, surviving cells)
	a.State.Sweep(5 * time.Minute)    // drop cells for deployments no longer present
	return nil
}
```

Why each line matters:
- `Vault.Resolve` keeps secrets *out* of the topology snapshot — only a pointer crosses over.
- `State.Acquire` re-binds the **existing** cell, so a reload during traffic does not reset
  in-flight connection counts or flip a healthy deployment to "unknown."
- `Registry.Replace` is the only step the hot path observes — and it observes it as one atomic
  pointer store. In-flight readers finish on the old snapshot; new readers see the new one.
- `Health.SetTargets` re-points the worker at the new wrappers (cells unchanged).
- `State.Sweep` GCs cells whose deployments were removed from config.

---

## 13. Concurrency Model — at a glance

| Data                         | Primitive                     | Readers            | Writers                         |
|------------------------------|-------------------------------|--------------------|---------------------------------|
| Model topology (`lookup`)    | `atomic.Pointer[snapshot]`    | lock-free, hot     | `writeMu` (Add/Replace), rare   |
| `DeploymentState` fields     | per-field atomics on a pinned cell | lock-free, hot | request path + breaker + probe  |
| `StateManager.states` map    | `sync.RWMutex`                | cold (load/sweep)  | cold (Acquire/Sweep)            |
| `VaultRegistry.entries` map  | `sync.RWMutex`                | cold (load)        | cold (Upsert/rotate)            |
| `HealthChecker.targets`      | `atomic.Pointer[[]*Deployment]` | lock-free (ticker) | `SetTargets` on reload        |

The rule: **anything the request touches is lock-free; anything with a mutex is off the hot path.**

---

## 14. Deliberately Cut

To keep the gateway lean, the following are intentionally absent (each was considered and rejected):

| Concept                          | Why it is out                                                            |
|----------------------------------|--------------------------------------------------------------------------|
| Separate route table / policy engine | The virtual key *is* the router. A second routing layer is redundant. |
| `RoutingSnapshot` / `RouteTarget` / `ModelRoute` wrappers | Extra indirection over a flat `[]*Deployment`. |
| `ComputeID` / content-hash identity | DB UUID is the stable identity; no hashing needed.                    |
| `Weight` / `Priority` per route  | Mode 1 is ordered-first-healthy; Mode 2 is LB by conns. No weights yet.   |
| `sync.Map` for any registry      | Built for key-churn; boxes + allocates per read. Wrong for bulk-swap.    |
| LB on Mode 1                      | A single explicit model needs no balancing — first-healthy is enough.    |

---

## 15. Open Items

1. **Timeout precedence** — deployment `Timeout` vs a global default vs per-pool override: define order.
2. **`credential_id` nullable?** — can a deployment exist without a credential (e.g. local Ollama)?
   If yes, `Vault.Resolve` must tolerate empty and the proxy must skip the auth header.
3. **Uniqueness migration** — enforce `(provider, model_name)` uniqueness at the DB so `Lookup`
   collisions can't occur silently.
4. **Bare vs aliased model names** — `ModelName` (client-facing) vs `BaseModelName` (wire) mapping
   rules when they differ.
5. **Credential → header mapping** — codify how `CustomHeader` overrides the provider default
   (Authorization: Bearer vs x-api-key vs api-key).
6. **Sweep grace vs reload cadence** — pick a `Sweep` grace window that can't drop a cell that a
   slow concurrent reload is about to re-Acquire.

---

*This document is the architectural source of truth for diffractLLM. Code blocks are the intended
shape of each component; identifiers match `internal/core/*`.*
```
