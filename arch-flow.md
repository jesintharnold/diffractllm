# diffractllm — Request Flow (implementer spec)

> Companion to `arch-goal.md`. This document describes **exactly how one request travels
> through the gateway**, from HTTP ingress to response, for both routing modes.
> It references real types/methods in `internal/core` and `internal/registry`.
> The pieces marked **[TO BUILD]** do not exist yet; everything else is already in the tree.
>
> Read `arch-goal.md` first for the component map and the load-balancer code.

---

## 0. Terminology (one line each)

| Term | Meaning |
|---|---|
| **rctx** | `core.DiffractLLMContext` — the pooled per-request context |
| **Deployment** | one (model × key) pairing; carries `APIRegistryID` + `*DeploymentState` |
| **ModelRegistry** | `map[ModelKey][]*Deployment` + pools; lock-free reads (COW) |
| **APIRegistry** | `map[registryID]*ModelAPIRegistry` — the upstream connections |
| **Balancer** | key-level LB: picks one `*Deployment` from a model's candidates |
| **Pool** | named weighted set of `(provider, model)` targets, resolved to a `ModelKey` |

---

## 1. Ingress → handler

```
gin router
  POST /openai/v1/chat/completions   → c.Set(RuteSDKProvider,"openai"); c.Set(RuteRequestKind,...)
  POST /anthropic/v1/messages        → c.Set(RuteSDKProvider,"anthropic"); ...
        │
        ▼
  genericHandler(c):                                            [TO BUILD — server]
     rctx := ctxPool.Acquire(ctx, req, writer)
     defer { RunPostCallHooks(rctx); ctxPool.Release(rctx) }    // rctx reset clears rctx.Deployment
     rctx.SDKProvider  = provider from route
     rctx.RequestKind  = kind from route
     handleRequest(rctx)
```

The **route prefix names the SDK dialect** the client is speaking (`openai`, `anthropic`, …),
not necessarily the destination provider — the body's `model` can override it (§3).

---

## 2. Parse & resolve the SDK descriptor

```
handleRequest(rctx):                                            [TO BUILD — server]
  1. limitReader = io.LimitReader(body, maxBodySize)
  2. strip "/{sdkProvider}" prefix from path → sub-path
  3. descriptor = GetDescriptor(sdkProvider, subPath)           // route → RouteDescriptor
       nil → 404 RouteNotFound
  4. bodyBytes = read(limitReader);  rctx.BodyBytes = bodyBytes
```

`RouteDescriptor` (per SDK dialect + path) knows: `RequestKind`, `NewRequest()`, `ToRute()`,
`FromRuteError()`, `FromRuteStream()`. It is the **SDK-shape ↔ internal-shape adapter**.

---

## 3. Determine provider + model from the body

```
resolveDestinationProvider(bodyBytes):
  raw = jsonparser.GetString(body,"model")        // e.g. "azure/gpt-5" or "gpt-4o" or "my-pool"
  if raw has "provider/model" form → (provider, bareModel, hasSlash=true)
  else                             → ("", raw, hasSlash=false)

  if hasSlash: rctx.Provider = destProvider        // body overrides route dialect
  else:        rctx.Provider = rctx.SDKProvider     // default to the route's dialect
  rctx.Model = bareModel                            // in pool mode this is the POOL NAME
```

**Key subtlety:** in `VKModelPool` mode, `rctx.Model` holds the **pool name**, not a model.
The mode is not known until auth runs (§4), so keep `rctx.Model` as the raw token for now.

---

## 4. Pre-call hooks: auth + admission (single hook)

```
if err := hookEngine.RunPreCallHooks(rctx); err != nil {        // AuthHook (governance)
    rctx.JSON(err.StatusCode, err); return
}
rctx.AuthFrozen = true
```

`AuthHook.Execute(rctx)` does everything identity-related in one place:

```
1. extract key (x-rute-key | Authorization: Bearer | x-api-key | x-goog-api-key)
2. validate signature + cache lookup + active/expiry check      → 401 on failure
3. set on rctx:  VirtualKeyID, BudgetRef, Mode, AllowedModels, ModelPools
4. ADMISSION (mode-specific):
     VKAllowedModel: key = ModelKey{rctx.Provider, rctx.Model}
                     if key ∉ vk.AllowedModels → 403 Forbidden
     VKModelPool:    if rctx.Model ∉ vk.ModelPools → 403 Forbidden   // rctx.Model = pool name
```

After this hook, the request is **authenticated and admitted**; `rctx.Mode` selects the
routing path below.

---

## 5. Routing — resolve to ONE deployment

This is the heart. Two modes; both end at a single pinned `*core.Deployment`.

```
                         rctx.Mode?
              ┌──────────────┴───────────────┐
        VKAllowedModel                   VKModelPool
              │                               │
  key = ModelKey{Provider, Model}    pool,_ = modelRegistry.LookupPool(rctx.Model)
              │                          nil → 404
              │                          │
              │                     mk = pool-level select (dataplane):
              │                        LBWeight     → WeightedModel(pool.AllowedModel)
              │                        LBDirect     → FirstModel(pool.AllowedModel)
              │                        LBRoundRobin → (v1: WeightedModel fallback)
              │                          │
              └──────────────┬───────────┘
                             ▼
        deps,ok = modelRegistry.LookupModel(mk)        // candidates = keys serving mk
             !ok/empty → 404
                             ▼
        chosen,err = balancer.Pick(deps, keyKind)      // key-level LB (§ arch-goal §9.1)
             ErrNoDeployments      → 404
             ErrNoHealthyDeployment→ 503  (see fallback below)
                             ▼
        rctx.Deployment = chosen                       // PIN — never re-resolve after this
```

- `keyKind` (the key-level strategy) defaults to `core.LBLeastConnection`.
  A single candidate collapses to itself inside `Pick`.
- **Pool fallback:** if `Pick` returns `ErrNoHealthyDeployment` on the pool path, re-roll the
  pool **excluding the failed model** (renormalize weights) and retry; only 503 when every
  pool member is exhausted. (Handler-side loop; the pure `WeightedModel` stays stateless.)

### Why "pin once" matters
`rctx.Deployment` and its `*DeploymentState` are captured **by pointer**. A concurrent registry
reload is safe (COW + GC + `Acquire` reuse — see `arch-goal.md §7`). Do **not** call
`LookupModel` / `LookupAPIRegistry` again later in the request.

---

## 6. Connection accounting

```
rctx.Deployment.State.AddConnection()                 // the instant we commit to this key
defer rctx.Deployment.State.RemoveConnection()        // NON-stream: on handler return
                                                       // STREAM: after the SSE loop drains (§8)
```

This gauge is what `leastConnection` reads. Getting the **stream timing** right is the only
correctness-sensitive part — decrement too early and a slow-streaming key looks idle.

---

## 7. Resolve the connection + build the upstream request

```
conn, ok = apiRegistry.LookupAPIRegistry(rctx.Deployment.APIRegistryID)
    !ok → 500 (deployment references a missing connection — should not happen if §8 orchestration holds)

providerInstance = ProviderMap[rctx.Provider]         // adapter registry           [TO BUILD]
    nil → 500 provider not supported

reqObj = descriptor.NewRequest()
sonic.Unmarshal(bodyBytes, reqObj)                     // SDK-shaped request
ruteReq = descriptor.ToRute(reqObj, rctx)              // → internal rute request
```

The **provider adapter** turns `(ruteReq, conn)` into an upstream `*http.Request`:
`conn.BaseURL` + path (from `descriptor`/kind) + auth header from `conn.APIkey`
(decrypted) + `conn.CustomHeader` when `conn.EnableCustomHeader`.

> `conn.APIkey` arrives already-decrypted from `dbstore.Load()` (`deref(k.APIKey)` after
> `AfterFind`). Never log it; never serialize it into a response.

---

## 8. Dispatch — non-streaming vs streaming

```
switch descriptor.RequestKind {
case <chat/speech/...>:
    if reqObj.IsStreaming():  handleStream(...)
    else:                     handleUnary(...)
default: 500 unknown request kind
}
```

**Unary:**
```
resp, ruteErr = providerInstance.Do(rctx, ruteReq, conn)      // proxy round-trip
  ruteErr → record status; body = descriptor.FromRuteError(*ruteErr); rctx.JSON(...)
resp     → write status + body; rctx.RequestCompleted = true
```

**Streaming (SSE):**
```
if writer not http.Flusher → 500
stream, ruteErr = providerInstance.DoStream(rctx, ruteReq, conn)
  ruteErr → FromRuteError → rctx.JSON(...)
set headers: Content-Type text/event-stream, Cache-Control no-cache, X-Accel-Buffering no
writer.WriteHeader(200); flush

for chunk := range stream:
    if ctx.Done()           → client disconnected, return   (defer RemoveConnection fires)
    if chunk.Type == Error  → write `event: error\ndata:…`, flush, return
    out = descriptor.FromRuteStream(*chunk)
    write `data: %s\n\n`, flush
write `data: [DONE]\n\n`; flush
rctx.RequestCompleted = true
```

`RemoveConnection` (deferred in §6) fires when this loop exits — **that** is the correct
moment for streams.

---

## 9. Post hooks

```
defer RunPostProviderHooks(rctx)   // usage capture, budget decrement (log-only, can't reject)
...
defer RunPostCallHooks(rctx)       // audit/metrics (from genericHandler defer, §1)
```

PostProvider is where **latency/usage metrics** are recorded (p50/p95, tokens, cost) — **not**
the LB. Budget is decremented here based on realized usage.

---

## 10. Error → HTTP status map

| Condition | Status | Source |
|---|---|---|
| Missing/invalid/expired key | 401 | AuthHook |
| Model/pool not permitted for key | 403 | AuthHook admission |
| Unknown route/descriptor | 404 | `GetDescriptor` nil |
| Model has no deployments (`ErrNoDeployments`) | 404 | `Balancer.Pick` |
| Pool name unknown | 404 | `LookupPool` |
| All candidates unhealthy/tripped (`ErrNoHealthyDeployment`) | 503 | `Balancer.Pick` (after pool fallback) |
| Budget exceeded | 402/429 | BudgetHook |
| Provider unsupported / missing connection | 500 | handler |
| Upstream error | passthrough | `descriptor.FromRuteError` |

---

## 11. End-to-end sequence (compact)

```
HTTP → genericHandler(acquire rctx) → parse descriptor → read body → parse provider/model
     → RunPreCallHooks[AuthHook: authn + admission, set Mode/Allowed/Pools]
     → route:
          VKAllowedModel: ModelKey → LookupModel → Balancer.Pick
          VKModelPool:    LookupPool → WeightedModel → ModelKey → LookupModel → Balancer.Pick (+fallback)
     → pin rctx.Deployment → State.AddConnection (+defer Remove)
     → LookupAPIRegistry(APIRegistryID) → ProviderMap[provider]
     → descriptor.ToRute → provider.Do / DoStream(conn) → upstream
     → write response / stream [DONE]
     → RunPostProviderHooks(usage,budget) → RunPostCallHooks(audit) → release rctx
```

---

## 12. Build checklist (what an implementer creates, in order)

1. **`internal/dataplane`** — `loadbalancer.go`, `poolrouter.go`, `loadbalancer_test.go`
   (full code in `arch-goal.md §9`); then the reverse-proxy/transport file.
2. **`internal/providers`** — adapter interface, `ProviderMap`, OpenAI adapter,
   `RouteDescriptor` + `GetDescriptor` + `ToRute`/`FromRute*`.
3. **`internal/server`** — `genericHandler` + `handleRequest` (this document), routes,
   middleware, and the **admin CRUD handlers** that run the §8-orchestration of `arch-goal.md`
   (DB write → APIRegistry upsert → ModelRegistry sync).
4. **`internal/gateway`** — composition root (struct in `arch-goal.md §2`): build registries
   from `dbsource.Load()`, register `AuthHook`, start proxy/syncer/server.
5. **`main.go`** — `NewGateway().Initialize().Start().WaitForSignal()`.
6. **Tests** — per layer, then one e2e against a mock upstream.
