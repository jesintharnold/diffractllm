# Deferred work

Things consciously postponed, with the reasoning that led to postponing them.
The reasoning is the valuable part — in six months the "why we didn't" is harder
to reconstruct than the "what".

Ordered by when they become relevant, not by size.

---

## A. Performance — revisit after the gateway serves real traffic

### A1. Evaluate fasthttp

**Status:** deferred to post-completion, by decision. Build on `net/http` first.

**What would change:** `internal/dataplane/transport.go` (outbound) and
`internal/core/rutecontext.go` (inbound). Nothing else, *if* the two seams in
A1.1 are honoured.

**The case for:** lower per-request parsing overhead, zero-alloc request reuse.
Bifrost uses it for most providers.

**The case against, and it is strong:**

1. **fasthttp's client has no HTTP/2.** Their `go.mod` carries no http2 package.
   Without multiplexing, 100 concurrent requests to one host need 100 TCP
   connections — which is why Bifrost sets `MaxConnsPerHost = 5000`. That is not
   a tuning choice, it is the cost of HTTP/1.1. For a gateway holding thousands
   of concurrent streams, each pinning a connection for tens of seconds, this
   compounds.
2. **Bifrost itself falls back to net/http where it matters** —
   `providers/bedrock/bedrock.go:82` uses `http.Transport` with
   `ForceAttemptHTTP2`, and so do their auxiliary clients
   (`core/network/http.go:347`). "Bifrost uses fasthttp" is only two-thirds true.
3. **The workload is fasthttp's worst case.** It wins on very high RPS with
   small, short-lived requests and no streaming. An LLM gateway is the inverse:
   modest RPS, large bodies, heavy SSE, requests measured in seconds. The
   per-request parsing win is amortised over a 200ms–60s call and disappears.

**Decide with numbers, not the above.** Benchmark the real handler under real
concurrency before touching it.

#### A1.1 Two seams that make the swap cheap — worth doing whenever convenient

Neither costs anything today; both make the later decision reversible.

**Do not expose `*http.Response` from the transport.** Currently
`TransportResult{Response *http.Response}`, so every provider reads
`result.Response.StatusCode` and `.Body` and learns which library moved the
bytes. Expose transport-neutral fields instead:

```go
type TransportResult struct {
	Status   int
	Header   http.Header   // map[string][]string, trivially replaceable
	Body     io.ReadCloser // stdlib interface; fasthttp can satisfy it
	TTFB     time.Duration
	Attempts int
}
```

**Do not touch `rctx.Writer` from handlers.** The SSE path currently does
`rctx.Writer.Write(...)` and `io.WriteString(rctx.Writer, ...)`, which leaks
`http.ResponseWriter` into the handler. One method closes it:

```go
// Write appends to the body after headers are sent. SSE needs this; WriteData
// sets a status and cannot be called per chunk.
func (rc *DiffractLLMContext) Write(p []byte) (int, error) {
	n, err := rc.Writer.Write(p)
	rc.ResponseBytes += n
	return n, err
}
```

Side benefit: `ResponseBytes` becomes accurate for streams, which it is not
today.

#### A1.2 `idleTimeoutBody` is where the fasthttp swap gets dangerous

Not a seam to prepare — a warning to read before starting. Our idle-stall
detector is ~40 lines because `net/http` is forgiving: `resp.Body.Close()` is
safe to call twice, and closing a body does not hand anything back to a shared
pool. Neither holds under fasthttp.

Bifrost runs the same design on fasthttp and carries three pieces of scar
tissue our version does not have, all in `core/providers/utils/utils.go`:

1. **`recover()` inside the timer callback.** An orphaned timer can fire after
   the connection was already released to the fasthttp pool, at which point
   `CloseWithError` nil-derefs in `(*HostClient).CloseConn`. Because that runs
   on the timer goroutine, the panic is *unrecoverable by callers and takes the
   whole process down*. The recover exists solely to stop a stale timer from
   killing the gateway.
2. **`recover()` inside `Read`** — their issue #3677.
3. **Double-close protection through a context flag**
   (`BifrostContextKeyConnectionClosed`), because a second close re-runs
   `releaseRequestStream` and double-`Put`s the pooled stream, in their words
   "poisoning another request" — a failure that lands on an unrelated caller.

So the fasthttp swap is not only "replace the client". Budget for: a timer that
can outlive the connection it was watching, a pool that punishes double-release,
and panics on a goroutine no caller can recover. Our single `sync.Once` around
`Close` is the right starting point but is not sufficient on its own — theirs
has one too, and they still needed all three of the above.

### A2. Pull-based streaming — remove the goroutine per stream

**Status:** deferred; the channel was chosen deliberately for simplicity.

Today each stream costs one goroutine (~8KB stack) plus a 256-slot channel
(~2KB). At 10k concurrent streams that is roughly 100MB and 10k scheduler
entries.

A pull-based iterator removes both:

```go
type ChatStream interface {
	Next() (*core.DiffractLLMChatCompletionStreamResponse, bool, *core.DiffractLLMError)
	Close() error
}
```

The handler drives it on its own goroutine. No spawn, no channel, no `select`,
and no "who closes the channel on a panic path" class of bug. Go 1.23+
`iter.Seq2` would make it idiomatic.

**Revisit when:** concurrent stream count makes the memory visible, or a
goroutine leak is observed.

### A3. Connection age recycling

`net/http` has no equivalent of fasthttp's `MaxConnDuration`. Bifrost sets it to
300s with the note: *"forces connection recycling to prevent stale connections
from NAT/LB silent drops."*

The failure: a NAT or load balancer drops a connection without a FIN, the pool
keeps it, the next request fails with `unexpected EOF`.

`IdleConnTimeout: 90s` covers the idle case and is already set. A connection in
*steady* use is never recycled, so the retry path is what actually covers this —
a connection error is retried, so the symptom is one extra attempt rather than a
failure.

**Revisit when:** `unexpected EOF` shows up in logs at a rate the retry does not
absorb. Fix would be a `DialContext` wrapper tracking connection age.

### A4. Rejected, with reasons — do not re-add without new evidence

- **Response buffer pool.** alphaX has a 32KB `sync.Pool`. It only pays for
  `io.Copy`, and every path here either `ReadAll`s or scans line by line. It
  would sit unused.
- **Router change (chi/gin/echo).** All are under 1µs against a 200ms–60s call.
  gin would additionally give a second context object competing with
  `DiffractLLMContext`.
- **Avoiding `any` boxing in the descriptor funcs.** Boxing a pointer does not
  allocate; the indirect call is ~2ns against a ~2µs marshal.

### A5. Small, uncontroversial — do whenever

Pre-size the request body read. `io.ReadAll` starts at 512 bytes and doubles, so
a 64KB conversation reallocates about seven times:

```go
size := r.ContentLength
if size <= 0 || size > s.maxBodyBytes {
	size = 64 << 10
}
buf := bytes.NewBuffer(make([]byte, 0, size))
_, err := buf.ReadFrom(io.LimitReader(r.Body, s.maxBodyBytes))
rctx.BodyBytes = buf.Bytes()
```

**Self-re-arming idle watchdog.** `idleTimeoutBody.Read` currently calls
`timer.Reset` on every read that returns bytes — one runtime timer-heap
operation per SSE token, each taking a global runtime lock, on the hottest path
in the process. The cheaper shape keeps an `atomic.Int64` of the last-progress
time and lets the timer re-arm itself, so it fires at most once per timeout
window regardless of throughput and `Read` pays one atomic store:

```go
func (b *idleTimeoutBody) watchdog() {
	idle := time.Duration(time.Now().UnixNano() - b.last.Load())
	if idle < b.timeout {
		b.timer.Reset(b.timeout - idle) // progress happened; wait out the remainder
		return
	}
	b.stalled.Store(true)
	b.Close()
}
```

Written, tested under `-race`, then **deliberately reverted** to the plain
Reset — 2026-08-24. Reason: bifrost serves real traffic with the plain Reset
(`core/providers/utils/utils.go`, `NewIdleTimeoutReader`), so the contention is
not biting anyone at that scale, and the clever version cost more in
readability than it bought at zero traffic. Bring it back only if a profile
names the timer lock.

### A6. Token subsets were double-charged — FIXED 2026-08-25

Not deferred; recorded because the reasoning is the thing worth keeping, and
because it is the same misreading that made B2 wrong.

**The rule:** `InputTokens` and `OutputTokens` are TOTALS. Every
`*_tokens_details` figure a provider reports — cached, audio, image, reasoning,
prediction — is a **subset already counted inside them**, not an extra on top.
OpenAI documents this for all of them: reasoning tokens *"are billed as output
tokens"*, and `prompt_tokens` is inclusive of its cached and audio breakdowns.

`CalculateCost` applied that rule to exactly one field. Cached input was
subtracted from the base before being charged its own rate:

```go
uncachedInput := u.InputTokens
uncachedInput -= u.CachedInputTokens       // correct
total := uncachedInput * inputRate
total += u.CachedInputTokens * cacheReadRate
```

Three lines later, reasoning was not:

```go
total += u.OutputTokens * outputRate                 // includes reasoning
total += u.ReasoningTokens * reasoningRate           // charged AGAIN
```

Same shape for input audio, output audio, and both image-token lines. Every one
of them billed twice whenever the model's pricing row set that specific rate.

**Severity.** Guarded by `rate()` returning 0 for a nil pointer, so it only
fired where litellm actually sets the field — but those are precisely the
reasoning models, where reasoning is usually most of the output. Measured on an
o1-shaped request (1000 output tokens, 800 of them reasoning): **79.2%
overcharge**. Overcharging is the worse direction; undercharging costs margin,
overcharging costs trust.

**The fix** extends the existing subtract-then-charge pattern to every subset
that has its own rate, keeping the original guard: if no specific rate is
configured, the subset stays in the base and is charged the ordinary
input/output rate, which is the correct fallback rather than dropping it.

Regression tests live in `internal/core/pricing_test.go`, including the case
that must NOT change (cached input) and the malformed-upstream case where a
subset exceeds its base.

**Deliberately left alone:** `CitationTokens`. Unlike the others it is not an
OpenAI `*_tokens_details` breakdown — it comes from providers that report
citations separately — and whether it sits inside `OutputTokens` has not been
verified. Guessing there would be repeating the mistake in the other direction.
Likewise the nested audio caches (`CachedAudioTokens`,
`CacheCreationAudioTokens`) are subsets of `InputAudioTokens`, a third level
down; worth a look before any audio model is priced seriously.

---

## B. Correctness gaps — these cost money, not milliseconds

### B1. `Pricing` cannot express two billing dimensions

`core.Pricing` has no `InferenceGeoUSMultiplier` and no fast-mode rate.
Bifrost's billing tier is a triple — `tierFromResponse(ServiceTier, Speed,
InferenceGeo)` (`framework/modelcatalog/datasheet/cost.go:288`) — and it applies
`tokenCost *= *pricing.InferenceGeoUSMultiplier` for US data residency
(`cost.go:550`). Ours is a scalar `ServiceTier`.

The chat response carries `Speed` and `InferenceGeo` already; `CalculateCost`
ignores them. Until `Usage.Tier` becomes a triple, an Anthropic US-residency
request is billed ~10% under.

### B2. Prediction token breakdown — RESOLVED 2026-08-25

```go
AcceptedPredictionTokens int64 `json:"accepted_prediction_tokens,omitempty"`
RejectedPredictionTokens int64 `json:"rejected_prediction_tokens,omitempty"`
```

Both fields now exist on `core.Usage` and are carried in both directions by
`ToDMChatCompletionUsage` / `ToOpenAIChatCompletionUsage`.

**Two things this entry got wrong, both worth keeping visible.**

*It was never a billing gap.* The original text claimed "every predicted-outputs
caller is currently undercharged by exactly the tokens they wasted." Wrong.
OpenAI's docs: *"Both accepted and rejected prediction tokens are counted toward
the `completion_tokens` metric used for billing purposes."* They are a
**breakdown of** `completion_tokens`, not an addition to it, so `OutputTokens`
already carried the cost and `CalculateCost` already charged it. The bill was
correct the whole time.

*The blocker was imaginary.* The converter carried a comment saying the counts
"have nowhere to go: `core.Usage` has no field for either" — describing a
missing field as if it were a constraint. It is our IR. The field was two lines.

What was genuinely missing was **reporting**: no way to tell a caller that 300
of the output tokens they paid for were predictions that missed. Predicted
outputs is the one feature where using it badly costs more, and the user could
not see it.

**The trap, recorded on the fields themselves:** they must NEVER get a rate line
in `CalculateCost`. They are inside `OutputTokens`; charging them again is
exactly the double-charge fixed in A6. The field comments in `pricing.go` say
REPORTING ONLY for that reason.

Still open, and genuinely blocked on nothing but a decision: whether to surface
the split in the request log and any usage API. That is a section C question.

### B3. Cache-creation TTL split

`Usage` has one `CacheCreationTokens` plus a `CacheLongTTL bool`. Anthropic
reports `cached_write_tokens_5m` and `cached_write_tokens_1h` separately
(`ChatCachedWriteTokenDetails`). A turn writing both TTLs cannot be billed
correctly today.

### B4. Code interpreter prices against a different catalog row

`Usage{CodeSessions: 1}` priced against `gpt-4o` returns **0** — the rate lives
on `openai/container`, its own row. Three rows in the feed carry
`code_interpreter_cost_per_session`.

Fix is in the billing hook, not the schema: when `Usage.CodeSessions > 0`, do a
second `ResolvePrice` against `<provider>/container`. An `AuxUsage` field was
designed and cut — the response should not carry instructions for how to bill
itself.

---

## C. Feature gaps with a known shape

### C1. Server-tool and MCP routing filter

`providerplane.Candidates(key)` needs the request's server tools and MCP servers
to filter on, so a provider that cannot serve them is dropped **before**
selection rather than failing after. Signature change; deferred until a request
actually carries them.

### C2. Metrics engine

Owns `StateManager`, `HealthSource`, `least_connection` and `latency_based`
selection. Its input is the `ActiveConns` counter and idempotent release closure
from alphaX's `ReverseProxy` — deliberately not added yet, because a counter
nothing reads is worse than no counter.

Round-robin only until then.

### C3. Remaining request kinds

`kind-chat-completion.go` is done. Designed but unwritten: text completion
(`CompletionPrompt`, `CompletionParams`, `CompletionLogProbs`), responses,
embedding, image, speech, transcription, moderation, models. Designs live in
`diffractllm-schemas.md`.


### C4. Operator-declared internal IP ranges for the SSRF guard

`isBlockedIP` (transport, §1 of `provider-design.md`) blocks only the ranges Go
itself can name: loopback, RFC1918, link-local, multicast, unspecified. A
hardcoded `100.64.0.0/10` (carrier-grade NAT / Tailscale) was written and then
**removed on purpose** — 2026-08-23.

**Why removed:** it was the gateway guessing at the operator's network topology.
Right most of the time, silently wrong exactly when someone picked an unusual
range — and "silently wrong" here means the SSRF guard waves through the
internal address it exists to block. Adding more constants does not fix that
class of error; it just moves where the guess is wrong.

**Where it bites:** EKS clusters that add `100.64.0.0/10` as a secondary VPC CIDR
to escape RFC1918 exhaustion (a documented AWS pattern), any Tailscale mesh
(every node sits in that range), and any cluster whose pod/service CIDR was
customised away from the CNI default. AKS and default EKS/Flannel/Calico/Cilium
all land inside `IsPrivate()` already, so the built-in set covers them.

**Shape when picked up** — two fields, opposite directions, different owners:

- `upstream.blocked_cidrs` in server.yaml: extra ranges to treat as internal,
  parsed to `[]netip.Prefix` at load so a typo fails at boot, not at dial time.
  Operator-only, deliberately NOT reachable from provider config, so a tenant
  cannot weaken it.
- `core.NetworkConfig.AllowedPrivateCIDRs`: narrows `allow_private_network` from
  a blunt on/off to a specific set. A self-hosted Ollama needs `10.0.0.5/32`
  reachable, not the whole private internet — which is what the bool grants
  today. Still a privilege boundary: `0.0.0.0/0` is a full bypass, so it belongs
  in the same admin-only bucket as `allow_private_network` and `proxy_config`
  when the management API lands.

**Prerequisite, not a nice-to-have:** this is only worth building alongside the
provider-management API. Until an untrusted party can write `cred.Endpoint`,
the whole guard is protecting the operator from their own config.

**Ordering caveat:** in Kubernetes this is the third line of defence, not the
first. NetworkPolicy/egress rules and IMDSv2 + hop-limit come before it, because
they cover the whole pod rather than only connections made through this dialer.
---

## How to use this file

When the question is "what else can we optimise", read section A — and start by
asking whether anything in section B should come first, because those are
revenue, not latency.
