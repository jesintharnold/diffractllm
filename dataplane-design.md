# Data plane — design

Status: proposed. Named to match `providerplane-design.md`.

## Context

`internal/dataplane` does not compile and would not work if it did:

- **`roundRobin.Deployment` is `return nil`** (`dataplane.go:187`). It is the only
  registered selector, and both success paths return `NewNoHealthyBackends` when
  it yields nil — so **every request fails**, today and before the refactor.
- **`se.registry` no longer exists** (`:43`, `:70`, `:105`). The field was
  replaced by `CredentialSource` but the body still calls `LookupModel`.
- **`core.Deployment` is deleted**, so `Selector`, `isHealthy` and `commit` all
  reference a missing type.
- **`selectWeightedProvider` returns `nil` and the caller dereferences it**
  (`:85-95`): `selectWeightedProvider(...).Provider` panics when no provider is
  healthy. A process crash where a 503 belongs.
- **`NewSelectionEngine(plane *CredentialSource, ...)`** takes a *pointer to an
  interface*. An interface value is already a reference; the pointer forces
  `*plane` at the call site and prevents passing a plane directly.
- **Health is checked here** (`hasHealthyDeployment`, `isHealthy`) against
  `DeploymentState`, which is moving to the metrics engine.
- `poolModelCursors` is left over from custom pools, which were deleted.

**Outcome:** the data plane owns routing *policy* — which provider, which
credential — and nothing else. Facts come from the provider plane, live state
from the metrics engine when it exists. Round robin only for now.

---

## 1. What the package declares

The data plane depends on **interfaces it defines**, not on `providerplane`.
`*providerplane.ProviderPlane` satisfies `CredentialSource` structurally, so
there is no import between them and a test needs a four-line fake. Same pattern
as `governance.ModelLookup`.

```go
package dataplane

// CredentialSource is the slice of the provider plane selection needs.
type CredentialSource interface {
	Candidates(key core.CatalogKey) []*core.Credential
}

// HealthSource is the metrics engine, once it exists. A nil HealthSource means
// no filtering: an unbuilt metrics engine must not take the gateway down.
type HealthSource interface {
	Healthy(creds []*core.Credential) []*core.Credential
}

// Selector picks one credential from a set that can all serve the request.
// key identifies the model, so a strategy can keep per-model state.
type Selector interface {
	Pick(key core.CatalogKey, creds []*core.Credential) *core.Credential
}

type SelectionEngine struct {
	plane     CredentialSource
	health    HealthSource // nil until the metrics engine lands
	selectors map[core.LBKind]Selector
	logger    *zap.Logger
}

// health may be nil until the metrics engine exists.
func NewSelectionEngine(plane CredentialSource, health HealthSource, logger *zap.Logger) *SelectionEngine {
	return &SelectionEngine{
		plane:  plane,
		health: health,
		selectors: map[core.LBKind]Selector{
			core.LBRoundRobin: newRoundRobin(),
		},
		logger: logger,
	}
}
```

Note `plane CredentialSource`, not `*CredentialSource` — an interface value is
already a reference, and the pointer forces `*plane` at the call site.

`selectors` holds only round robin. `core.LBLeastConnection` and
`core.LBLatencyBased` parse from config today but have no implementation; `kind`
falls back and warns rather than silently degrading.

## 2. Selector: round robin

```go
// roundRobin rotates across candidates. The cursor lives here, not in the
// provider plane's snapshot, so it survives a credential reload - otherwise
// every admin edit would reset every model back to the first key.
type roundRobin struct {
	cursors sync.Map // core.CatalogKey -> *atomic.Uint64
}

func newRoundRobin() *roundRobin { return &roundRobin{} }

func (r *roundRobin) Pick(key core.CatalogKey, creds []*core.Credential) *core.Credential {
	switch len(creds) {
	case 0:
		return nil
	case 1:
		return creds[0] // the overwhelmingly common case: one key per provider
	}

	value, _ := r.cursors.LoadOrStore(key, new(atomic.Uint64))
	cursor := value.(*atomic.Uint64)

	// Add returns the new value; -1 makes the first pick index 0. The counter is
	// monotonic and modulo maps it onto whatever the current candidate set is,
	// so the set changing size does not corrupt the rotation.
	next := cursor.Add(1) - 1
	return creds[next%uint64(len(creds))]
}
```

`kind()` currently falls back to round robin for any unregistered `LBKind`, which
silently accepts a `least_connection` config and quietly does something else.
Keep the fallback, but log at warn once per unknown kind so a misconfiguration is
visible rather than invisible.

## 3. `Resolve`

**Two named paths, because the two cases mean different things.**

- `openai/gpt-4o` — the client chose the provider. If openai has no usable
  credential that is a 503; it must **never** silently fall over to anthropic.
- `gpt-4o` — the client did not choose, so weighting across the virtual key's
  providers is the entire point.

They share the half that is genuinely common — asking the plane and applying the
health filter — and nothing else. Merging the *decision* would bury a routing
rule inside an optimisation.

Within the weighted path the ordering is load-bearing: **filter, then weight**.
Choosing a provider by weight first can land on one whose keys are all disabled,
expired or unhealthy, which then needs a re-roll-and-renormalise loop. Filtering
first means a provider with no usable keys never enters the weighting, and
renormalisation falls out for free.

```go
func (se *SelectionEngine) Resolve(rctx *core.DiffractLLMContext) (*core.Credential, *core.DiffractLLMError) {
	if rctx == nil || rctx.VirtualKeyID == "" || rctx.VirtualKeyPolicy == nil {
		return nil, core.NewAuthFailed("virtual key policy missing, contact the administrator")
	}

	vk := rctx.VirtualKeyPolicy
	if len(vk.ProviderConfigs) == 0 {
		return nil, core.NewAuthFailed("virtual key has no provider configs, contact the administrator")
	}

	key := rctx.Modelkey
	if key.ModelName == "" {
		return nil, core.NewMissingParameter("model")
	}

	if key.Provider != "" {
		return se.resolveExplicit(rctx, key, vk)
	}
	return se.resolveWeighted(rctx, key, vk)
}

// resolveExplicit handles "openai/gpt-4o": the client named the provider, so no
// weighting runs and no other provider is considered. A dead provider here is a
// 503, not a reason to route somewhere the client did not ask for.
func (se *SelectionEngine) resolveExplicit(
	rctx *core.DiffractLLMContext,
	key core.CatalogKey,
	vk *core.VirtualKey,
) (*core.Credential, *core.DiffractLLMError) {
	// The admission hook already checked this, but the data plane must not
	// route to a provider the key does not carry on the hook's say-so alone.
	if vk.ProviderConfig(key.Provider) == nil {
		se.logger.Warn("selection rejected",
			zap.String("virtual_key_id", rctx.VirtualKeyID),
			zap.String("provider", string(key.Provider)),
			zap.String("reason", "provider is not configured on this key"))
		return nil, core.NewForbidden("provider " + string(key.Provider) + " is not permitted for this key")
	}

	creds := se.candidates(key.Provider, key)
	if len(creds) == 0 {
		return nil, core.NewNoHealthyBackends(key.SlashKey())
	}

	chosen := se.kind(vk.LoadBalancer).Pick(key, creds)
	if chosen == nil {
		return nil, core.NewNoHealthyBackends(key.SlashKey())
	}
	return se.commit(rctx, key, chosen), nil
}

// resolveWeighted handles a bare "gpt-4o": gather every provider the key permits
// that actually has a usable credential, weight across those, then pick a
// credential within the winner.
func (se *SelectionEngine) resolveWeighted(
	rctx *core.DiffractLLMContext,
	key core.CatalogKey,
	vk *core.VirtualKey,
) (*core.Credential, *core.DiffractLLMError) {
	viable := make(map[core.Provider][]*core.Credential, len(vk.ProviderConfigs))
	for _, cfg := range vk.ProviderConfigs {
		if creds := se.candidates(cfg.Provider, key); len(creds) > 0 {
			viable[cfg.Provider] = creds
		}
	}
	if len(viable) == 0 {
		return nil, core.NewNoHealthyBackends(key.ModelName)
	}

	provider := se.pickProvider(viable, vk)
	if provider == "" {
		return nil, core.NewNoHealthyBackends(key.ModelName)
	}

	key.Provider = provider
	chosen := se.kind(vk.LoadBalancer).Pick(key, viable[provider])
	if chosen == nil {
		return nil, core.NewNoHealthyBackends(key.SlashKey())
	}
	return se.commit(rctx, key, chosen), nil
}

// candidates is the shared half of both paths: plane facts, then live state.
// ModelType is carried through even though the plane ignores it, so a future
// type-aware filter does not need this call site changed.
func (se *SelectionEngine) candidates(provider core.Provider, key core.CatalogKey) []*core.Credential {
	creds := se.plane.Candidates(core.CatalogKey{
		Provider:  provider,
		ModelName: key.ModelName,
		ModelType: key.ModelType,
	})

	// A nil HealthSource means the metrics engine has not landed. Returning
	// creds unchanged is deliberate: an absent engine must not reject traffic.
	if se.health == nil {
		return creds
	}
	return se.health.Healthy(creds)
}

// commit pins the choice to the request context. Golden rule: pick once, pin,
// never re-resolve later in the request.
func (se *SelectionEngine) commit(
	rctx *core.DiffractLLMContext,
	key core.CatalogKey,
	cred *core.Credential,
) *core.Credential {
	rctx.Modelkey = key
	rctx.SelectedCredential = cred
	return cred
}

// kind returns the configured selector. Unknown kinds fall back to round robin
// rather than failing the request, but warn: least_connection and latency_based
// parse successfully today and would otherwise degrade invisibly.
func (se *SelectionEngine) kind(kind core.LBKind) Selector {
	if selector, ok := se.selectors[kind]; ok {
		return selector
	}
	se.logger.Warn("load balancer not implemented, using round robin",
		zap.String("requested", kind.String()))
	return se.selectors[core.LBRoundRobin]
}
```

The duplication the current file has — two copies of lookup, select, nil-check,
commit — is gone, because the shared work is in `candidates` and `commit`. What
stays separate is the decision, which is the part that differs.

## 4. Weighted provider selection, fixed

```go
// pickProvider chooses among providers that already have usable credentials.
// It never returns "" for a non-empty viable set - that was the nil dereference
// at dataplane.go:85.
func (se *SelectionEngine) pickProvider(
	viable map[core.Provider][]*core.Credential,
	vk *core.VirtualKey,
) core.Provider {
	if len(viable) == 1 {
		for provider := range viable {
			return provider
		}
	}

	var total float64
	for _, cfg := range vk.ProviderConfigs {
		if _, ok := viable[cfg.Provider]; ok && cfg.Weight > 0 {
			total += float64(cfg.Weight)
		}
	}

	// Every viable provider has weight 0 - legal, since CompileProviderConfigs
	// only enforces the sum for VKWeighted. Degrade to uniform rather than fail.
	if total <= 0 {
		return anyProvider(viable)
	}

	target := rand.Float64() * total
	for _, cfg := range vk.ProviderConfigs {
		if _, ok := viable[cfg.Provider]; !ok || cfg.Weight <= 0 {
			continue
		}
		target -= float64(cfg.Weight)
		if target < 0 {
			return cfg.Provider
		}
	}
	return anyProvider(viable) // float rounding guard
}

// anyProvider returns an arbitrary provider from a non-empty set, and "" from an
// empty one. Used only for the two degradation cases - all weights zero, and
// float rounding leaving the walk short - never on the primary path, because map
// iteration order is random and routing should not be.
func anyProvider(viable map[core.Provider][]*core.Credential) core.Provider {
	for provider := range viable {
		return provider
	}
	return ""
}
```

`pickProvider` never returns `""` for a non-empty `viable`: the single-entry
shortcut, the zero-weight degradation and the rounding guard all end at
`anyProvider`, which only returns `""` when the map is empty. That is the fix for
the nil dereference at `dataplane.go:85`, where the old `selectWeightedProvider`
returned `nil` and the caller immediately read `.Provider` off it.

Weights are **renormalised implicitly**: `total` sums only viable providers, so
a key configured 30/70 whose 70% provider has no usable credentials sends 100% to
the other one, with no explicit renormalise step.

Determinism note: `anyProvider` iterating a map is unordered. That is acceptable
for the all-zero-weights degradation but must not be the primary path, which is
why the weighted walk iterates `vk.ProviderConfigs` (a slice, stable order)
rather than the map.

## 5. Deleted

`isHealthy`, `hasHealthyDeployment` — health moves to the metrics engine.
`poolModelCursors` — custom pools were removed.
The `Selector.Deployment` method name — it is `Pick`, and it takes credentials.
The duplicated explicit/bare selection blocks — one path now.

`internal/registry` is deleted in the same pass; `dataplane` was its only
importer.

## 6. Verification

```
go build ./internal/... && go vet ./internal/... && go test ./internal/... -race
```

`internal/dataplane/roundrobin_test.go`

- Empty set returns nil; single candidate returns it without touching a cursor.
- Three candidates over six picks visit each exactly twice, in order.
- **Cursor survives a candidate-set change** — rotate twice over three, then call
  with two, and assert it neither panics nor restarts at zero. This is the
  property that makes the cursor live in the balancer rather than the snapshot.
- Two different `CatalogKey`s keep independent cursors.
- Concurrent `Pick` under `-race` with 100 goroutines; assert every returned
  credential is from the input set and the distribution is within one of even.

`internal/dataplane/dataplane_test.go`, with a fake `CredentialSource` that
records which providers it was asked about.

Explicit path:

- `openai/gpt-4o` asks the plane about **openai only** — assert the fake was
  never queried for the key's other providers. This is the "client chose, do not
  fall over" rule.
- `openai/gpt-4o` where openai has no candidates → `NewNoHealthyBackends`, and
  **not** a credential belonging to anthropic even though anthropic has one.
- A provider not on the virtual key → `NewForbidden` naming the provider.

Weighted path:

- One provider config → selected without consulting weights.
- Two providers, only one with candidates → always that one. **Regression guard
  for the nil dereference**: the old code panicked here.
- Two viable providers weighted 1.0 / 0.0 → the zero-weight one is never chosen
  across many iterations.
- All viable weights zero → both appear across many iterations (uniform
  degradation, not failure).
- Weights 0.3 / 0.7 with both viable → distribution within tolerance over 10k
  iterations.
- Weights 0.3 / 0.7 where the 0.7 provider has no candidates → 100% to the other,
  proving renormalisation is implicit.
- No provider has candidates → `NewNoHealthyBackends`, no panic.

Shared:

- Empty model name → `NewMissingParameter`, not an internal error.
- Nil virtual key policy → `NewAuthFailed`.
- Empty `ProviderConfigs` → `NewAuthFailed`.
- `commit` sets both `rctx.Modelkey.Provider` and `rctx.SelectedCredential`, and
  the pinned key carries the resolved provider on the bare-model path.
- Nil `HealthSource` admits everything; a fake that drops one credential is
  respected; a fake that drops all of one provider's removes that provider from
  the weighting entirely.
- `kind()` with `core.LBLeastConnection` returns round robin and logs a warning.

## 7. Follow-on (not in this change)

**Metrics engine** — implements `HealthSource`, plus the counters
`least_connection` and `latency_based` need. Those two `core.LBKind` values exist
today with nothing behind them.

**Reactive rotation** — proactive health filtering cannot catch a key that goes
bad between selection and the call, so a 429 must mark the credential in metrics
and retry the next candidate. Bifrost relies on this exclusively (they return a
pool and rotate); here it complements the filter rather than replacing it.

**Error split** — `Candidates` returning empty currently cannot distinguish "no
credential serves this model" (a 404: the client asked for something that does
not exist) from "credentials exist but all are disabled or unhealthy" (a 503:
try again). Both are `NewNoHealthyBackends` today. Splitting them needs an
unfiltered count from the plane, and matters for client retry behaviour.
