# Provider layer — build plan

**v2 — 2026-08-25**

What is left to build. Implemented sections are deleted, not archived; the
reasoning behind past decisions lives in `deferred-work.md`.

| | state |
|---|---|
| `configs/`, `internal/dataplane/transport.go` | done, tested |
| `internal/core` | done except `ChatContent` (§0) |
| `internal/providers/openai` | in progress — §2 to §5 |
| `internal/server` | not started — §6 to §9 |

---

## Where things live

```
internal/core/
  kind-chat-completion.go   the IR (exists)
  upstream.go               NetworkConfig gains 3 fields   ← APPLIED
  rutecontext.go            outcome fields + Overwrite/Write ← APPLIED

internal/dataplane/
  dataplane.go              SelectionEngine (exists, per dataplane-design.md)
  transport.go              Transport: the shared outbound HTTP call   ← NEW

internal/providers/
  providers.go              Provider interface, ProviderMap, Register, shared helpers
  openai/
    openai.go               OpenAIProvider: transport call + orchestration
    types.go                OpenAIChatCompletionRequest — wire shapes only
    chat.go                 request codec: To/From IR. no I/O
    response.go             usage codec: OpenAI vocabulary -> core.Usage
    stream.go               SSE scan loop, shared by every streaming kind
    errors.go               OpenAI error body -> core.DiffractLLMError

internal/server/
  registry.go               RouteDescriptor table (no lookup - routes close over it)
  routes.go                 mux
  handlers.go               generic handler + per-kind handler + request log

internal/governance/
  hook_virtualkey_auth.go   gains Authenticate() adapter; leaves PreCall chain
```

**Transport in `dataplane`.** The data plane already owns which upstream a
request goes to; owning *how* it gets there is the same concern. A provider
decides the payload; the data plane moves bytes.

**One `Provider` interface, not four.** Methods are added as kinds are
implemented. A provider that does not serve a kind returns
`NewUnsupportedOperation` — the trade for a single interface, correct at this
size.

---

## 0. `internal/core/kind-chat-completion.go` — `ChatContent`

`content` arrives as either a string or an array of parts, and the IR currently
models only the array:

```go
Content []ChatContentPart `json:"content,omitempty"`
```

So the most common client body in existence fails to decode:

```json
{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}
```

`"Hello"` is not an array. The same split appears on the way back — OpenAI's
*response* returns a plain string for the assistant turn.

**This belongs in the IR, not in a provider.** String-or-array `content` is not
an OpenAI quirk: Anthropic accepts both forms too, and so does every
OpenAI-compatible endpoint. A shape every provider shares is the IR's job.

The obvious alternative — a second `*string` field beside `Content` — does not
work and would be worse if it did. Two fields cannot share the `content` JSON
tag, so one would be silently ignored; and giving them different tags would put
two fields in the IR meaning the same thing, which every hook, converter and
token counter then has to check twice and will eventually disagree about.

One field, one tag, a custom decoder:

```go
// ChatContent is the message body in either wire form. A bare string lifts to
// a single text part, so everything downstream sees one shape and never has to
// ask which form arrived.
type ChatContent []ChatContentPart

func (c *ChatContent) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		// null is normal on a tool-call turn. It must stay nil rather than
		// become a zero-length slice: "no content" and "empty content" are
		// different things to anything counting parts.
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := sonic.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = ChatContent{{Type: ContentText, Text: &s}}
		return nil
	}
	var parts []ChatContentPart
	if err := sonic.Unmarshal(b, &parts); err != nil {
		return err
	}
	*c = parts
	return nil
}
```

Then in `DiffractLLMChatMessage`:

```go
Content ChatContent `json:"content,omitempty"`
```

`MarshalJSON` is required too, and it is not cosmetic. Responses go back to
CLIENTS, and every OpenAI SDK types `message.content` as `string | null`. Emit
the array form there and a typed client breaks. So the single-text case
collapses back to a string, and anything else stays an array — which OpenAI
accepts inbound, and which is the only honest thing to do with an image part.

```go
func (c ChatContent) MarshalJSON() ([]byte, error) {
	// Exactly one text part is the overwhelmingly common case and the only one
	// that can round-trip to a string without losing something.
	if len(c) == 1 && c[0].Type == ContentText && c[0].Text != nil {
		return sonic.Marshal(*c[0].Text)
	}
	if c == nil {
		return []byte("null"), nil
	}
	// The conversion is load-bearing: without it this method calls itself.
	return sonic.Marshal([]ChatContentPart(c))
}
```

Note the value receiver. A pointer receiver would not fire when the value is
reached through a non-addressable path, which is exactly how it is reached
inside a slice of choices.

What this removes, verified before writing it: with `ChatContent` in place,
`core.DiffractLLMChatMessage` decodes an OpenAI response message as it stands
and `core.ChatResponseChoice` decodes the choice around it. So §3c needs no
provider-side message or choice type, and `OpenAIChatCompletionResponse` shadows exactly
one field.

`bytes` and the sonic import join the file.

## 2. `internal/providers/providers.go`

```go
package providers

import (
	"diffractllm/internal/core"
	"fmt"
)

// ProviderMap is every registered provider, keyed by name. A package-level map
// rather than a Registry type with an Entry struct: at two providers there is
// nothing for a type to add.
var ProviderMap = map[core.Provider]Provider{}

func Register(p Provider) { ProviderMap[p.ProviderName()] = p }

func Get(name core.Provider) Provider { return ProviderMap[name] }

// Provider is one interface with one method per operation.
type Provider interface {
	ProviderName() core.Provider

	ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest,
		cred *core.Credential) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError)

	ChatCompletionStream(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest,
		cred *core.Credential) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError)
}

// ProviderRequest is implemented by every provider-shaped inbound request, so
// the generic handler can ask whether to take the streaming path before it
// knows which kind it is holding.
type ProviderRequest interface {
	IsStreaming() bool
}

func NewUnsupportedOperation(kind core.RequestKind, provider core.Provider) *core.DiffractLLMError {
	return core.NewInternalError("provider",
		fmt.Sprintf("%s does not support %s", provider, kind), nil)
}
```


### Two things `go vet` flags in the current `providers.go`

Neither breaks the build, so both are easy to carry a long way.

**1. The `Get` error message loses its argument.**

```go
fmt.Sprintf("%s does not exist %s", string(provider))   // two verbs, one arg
```

`go vet` reports `format %s reads arg #2, but call has 1 arg`, and the message
renders as `openai does not exist %!s(MISSING)`. Suggested:

```go
fmt.Sprintf("provider %s is not registered", string(provider))
```

**2. The interface's `ChatCompletion` declares no return values.**

```go
ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest,
    cred *core.Credential)          // <- compiles; returns nothing
```

Legal Go, so nothing complains, but no implementation can hand a response back
through it. `ChatCompletionStream` beside it has the right shape. It wants:

```go
ChatCompletion(rctx *core.DiffractLLMContext, req *core.DiffractLLMChatCompletionRequest,
    cred *core.Credential) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError)
```

Worth fixing before a second provider is written against the interface, because
after that it is a change to every implementation rather than to one line.

Also worth a look while in here: `Get` returns `(*Provider, error)` - a pointer
to an interface, which is almost never what is wanted. `Provider` is already a
reference type, so `(Provider, *core.DiffractLLMError)` gives callers the thing
itself and keeps the typed error the rest of the codebase uses.
---

## 3. `internal/providers/openai/types.go`

Wire shapes only. No conversion, no I/O.

```go
package openai

import (
	"diffractllm/internal/core"
)

// OpenAI's body is flat; the IR nests params under "parameters". The embed is
// anonymous so it serialises flat with no custom marshaller.
type OpenAIChatCompletionRequest struct {
	Model    string                        `json:"model"`
	Messages []core.DiffractLLMChatMessage `json:"messages"`

	core.DiffractLLMChatParameters // anonymous VALUE -> flattens
}

// IsStreaming lets the generic handler pick the path before it knows the kind.
func (r *OpenAIChatCompletionRequest) IsStreaming() bool {
	return r.Stream != nil && *r.Stream
}

// The response shapes live in response.go, not here: OpenAI's usage block uses
// a different vocabulary from core.Usage (prompt_tokens vs input_tokens), so
// they need real conversion rather than a direct unmarshal. See §3c.
```

---

## 3b. `internal/providers/openai/chat.go`

The codec, and **nothing else**. No transport, no HTTP, no context beyond
reading `BodyBytes`. That is the point of the file: every conversion here is
testable with a struct literal and no network, no mock, no fixture server.

Bifrost splits the same way — their `chat.go` is `ToBifrostChatRequest` /
`ToOpenAIChatCompletionRequest` plus normalisers, with the HTTP call living elsewhere.
When a second OpenAI-dialect provider (azure, ollama, custom) arrives it imports
these two functions directly; there is deliberately no shared "dialect" package
until a second consumer proves what actually varies. The tripwire for
revisiting: the day `openai.go` grows an `if provider == azure` branch.

```go
package openai

import (
	"diffractllm/internal/core"
)

// ToDMChatCompletionRequest is INBOUND: an OpenAI-dialect body from a client
// into the IR. The route descriptor calls it.
func (r *OpenAIChatCompletionRequest) ToDMChatCompletionRequest(rctx *core.DiffractLLMContext) *core.DiffractLLMChatCompletionRequest {
	params := r.DiffractLLMChatParameters
	return &core.DiffractLLMChatCompletionRequest{
		Model:      r.Model,
		Messages:   r.Messages,
		Parameters: &params,
		Raw:        rctx.BodyBytes, // kept for the same-dialect fast path
	}
}

// ToOpenAIChatCompletionRequest is OUTBOUND: the IR into OpenAI's wire shape.
//
// model comes from the caller, not req.Model: the data plane resolves aliases,
// so the name the client typed and the name the provider expects can differ.
func ToOpenAIChatCompletionRequest(req *core.DiffractLLMChatCompletionRequest, model string) *OpenAIChatCompletionRequest {
	out := &OpenAIChatCompletionRequest{Model: model, Messages: req.Messages}
	if req.Parameters != nil {
		out.DiffractLLMChatParameters = *req.Parameters
	}
	return out
}
```

---

## 3c. `internal/providers/openai/response.go`

An earlier draft of this document said the response shape and the IR's were
identical, so `sonic.Unmarshal(respBody, &out)` straight into
`core.DiffractLLMChatCompletionResponse` was enough. **That was wrong, and the
way it was wrong is expensive.**

`core.Usage` is named after the Responses/Anthropic vocabulary:

```go
InputTokens       int64 `json:"input_tokens,omitempty"`
OutputTokens      int64 `json:"output_tokens,omitempty"`
CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
```

OpenAI Chat Completions returns a different vocabulary entirely:

```json
"usage": {
  "prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7,
  "prompt_tokens_details":     {"cached_tokens": 0, "audio_tokens": 0},
  "completion_tokens_details": {"reasoning_tokens": 0, "audio_tokens": 0}
}
```

Not one field name matches. A direct unmarshal leaves `InputTokens` and
`OutputTokens` at **zero**, `pricing.go:722` multiplies zero by the rate, and
every OpenAI request bills at **nothing** — with no error anywhere, because a
zero-valued `Usage` is structurally valid. Streaming has the same hole: the
usage chunk decodes to a non-nil `Usage`, so `StreamEventComplete` still fires
and the request still looks accounted for.

A type ALIAS cannot fix this. An alias is the same type with the same struct
tags; the tags are the problem. It needs a distinct wire struct and a real
converter.

The second mismatch is `content`. The IR models it as `[]ChatContentPart`
because that is what a request may carry; OpenAI's *response* returns a plain
string (or null when the turn is a tool call).

```go
package openai

import (
	"diffractllm/internal/core"
)

// Usage differs per KIND - responses uses input_tokens, embeddings has no
// completion_tokens - so every kind gets its own struct and converter pair.
type OpenAIChatCompletionUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`

	PromptTokensDetails     *openAIChatPromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openAIChatCompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type openAIChatPromptTokensDetails struct {
	CachedTokens     int64 `json:"cached_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	AudioTokens      int64 `json:"audio_tokens,omitempty"`
	ImageTokens      int64 `json:"image_tokens,omitempty"`
	TextTokens       int64 `json:"text_tokens,omitempty"`
}

type openAIChatCompletionTokensDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens,omitempty"`
	AudioTokens     int64 `json:"audio_tokens,omitempty"`
	TextTokens      int64 `json:"text_tokens,omitempty"`

	// REPORTING ONLY - a breakdown of completion_tokens, not an addition.
	// Never give these a rate line; that is the double-charge in A6.
	AcceptedPredictionTokens int64 `json:"accepted_prediction_tokens,omitempty"`
	RejectedPredictionTokens int64 `json:"rejected_prediction_tokens,omitempty"`
}

// No response message or choice type needed: core.ChatContent absorbs the
// string form, so only usage disagrees with the IR.

// Embeds the IR response; only usage disagrees, so only usage is shadowed.
// A depth-0 field wins over a promoted one, so the embedded core.Usage stays nil.
type OpenAIChatCompletionResponse struct {
	core.DiffractLLMChatCompletionResponse

	Usage *OpenAIChatCompletionUsage `json:"usage,omitempty"`
}
```

### The converters

```go
// prompt_tokens -> InputTokens UNCHANGED. It already includes the cached count
// and pricing.go subtracts; subtracting here too undercharges every cache hit.
func (u *OpenAIChatCompletionUsage) ToDMChatCompletionUsage() *core.Usage {
	if u == nil {
		return nil
	}
	out := &core.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
	if d := u.PromptTokensDetails; d != nil {
		out.CachedInputTokens = d.CachedTokens
		out.InputAudioTokens = d.AudioTokens
		out.InputImageTokens = d.ImageTokens

		// cache_write_tokens is the expensive one. pricing.go charges
		// CacheCreationTokens at cacheCreationRate - a PREMIUM over the normal
		// input rate - and before this line nothing populated the field, so
		// every cache write billed at zero.
		out.CacheCreationTokens = d.CacheWriteTokens
	}
	if d := u.CompletionTokensDetails; d != nil {
		out.ReasoningTokens = d.ReasoningTokens
		out.OutputAudioTokens = d.AudioTokens

		// REPORTING ONLY - subsets of completion_tokens, already billed via
		// OutputTokens. Never give them a rate line (deferred-work A6).
		out.AcceptedPredictionTokens = d.AcceptedPredictionTokens
		out.RejectedPredictionTokens = d.RejectedPredictionTokens
	}

	// TotalTokens is stored as the provider reported it rather than recomputed,
	// because core.Usage has the field and leaving it zero would hand a wrong
	// answer to anything that reads it.
	out.TotalTokens = u.TotalTokens

	// TextTokens is the one figure deliberately not carried: it is derivable
	// from the others, OpenAI reports it only on some models, and core.Usage
	// has nowhere for it that would not just be a third way to say the same
	// thing.
	return out
}

// Lifts the embed and converts the one field that differs. Raw rides along,
// set by the caller on the wire struct.
func (r *OpenAIChatCompletionResponse) ToDMChatCompletionResponse() *core.DiffractLLMChatCompletionResponse {
	out := r.DiffractLLMChatCompletionResponse
	out.Usage = r.Usage.ToDMChatCompletionUsage()
	return &out
}
```


### The outbound half

Every inbound conversion needs its mirror, because the client's dialect and the
provider's are independent. An OpenAI client talking to an **Anthropic** upstream
gets the IR back and it has to be rendered as OpenAI on the way out — there is no
provider `Raw` in that dialect to fall back on.

Without these, `FromDiffract` returns the IR and `rctx.JSON` marshals it with the
IR's own tags, so the client receives `"usage":{"input_tokens":5}` when every
OpenAI SDK reads `response.usage.prompt_tokens`. Undefined, silently. It is the
same defect as the inbound one, pointed the other way.

```go
// Inverse of ToDMChatCompletionUsage. TotalTokens falls back to Input+Output;
// input already includes the cached count, so do NOT add it again.
func ToOpenAIChatCompletionUsage(u *core.Usage) *OpenAIChatCompletionUsage {
	if u == nil {
		return nil
	}
	out := &OpenAIChatCompletionUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
	}
	// Fall back to the sum only when the provider did not give us one. Input
	// already includes its cached portion, so this is a plain add - do NOT
	// re-add CachedInputTokens.
	if out.TotalTokens == 0 {
		out.TotalTokens = u.InputTokens + u.OutputTokens
	}

	if u.CachedInputTokens > 0 || u.CacheCreationTokens > 0 ||
		u.InputAudioTokens > 0 || u.InputImageTokens > 0 {
		out.PromptTokensDetails = &openAIChatPromptTokensDetails{
			CachedTokens:     u.CachedInputTokens,
			CacheWriteTokens: u.CacheCreationTokens,
			AudioTokens:      u.InputAudioTokens,
			ImageTokens:      u.InputImageTokens,
		}
	}
	if u.ReasoningTokens > 0 || u.OutputAudioTokens > 0 ||
		u.AcceptedPredictionTokens > 0 || u.RejectedPredictionTokens > 0 {
		out.CompletionTokensDetails = &openAIChatCompletionTokensDetails{
			ReasoningTokens:          u.ReasoningTokens,
			AudioTokens:              u.OutputAudioTokens,
			AcceptedPredictionTokens: u.AcceptedPredictionTokens,
			RejectedPredictionTokens: u.RejectedPredictionTokens,
		}
	}
	return out
}

// OUTBOUND: IR to OpenAI wire. Only usage converts - ChatContent.MarshalJSON
// already collapses a single text part back to a string.
func ToOpenAIChatCompletionResponse(res *core.DiffractLLMChatCompletionResponse) *OpenAIChatCompletionResponse {
	if res == nil {
		return nil
	}
	out := &OpenAIChatCompletionResponse{DiffractLLMChatCompletionResponse: *res}
	out.Usage = ToOpenAIChatCompletionUsage(res.Usage)
	return out
}

// Same trick for one SSE chunk. Deltas need no conversion - ChatMessageDelta
// .Content is already *string - so only the terminal usage chunk differs.
type OpenAIChatCompletionStreamResponse struct {
	core.DiffractLLMChatCompletionStreamResponse

	Usage *OpenAIChatCompletionUsage `json:"usage,omitempty"`
}

func ToOpenAIChatCompletionStreamResponse(c *core.DiffractLLMChatCompletionStreamResponse) *OpenAIChatCompletionStreamResponse {
	if c == nil {
		return nil
	}
	out := &OpenAIChatCompletionStreamResponse{DiffractLLMChatCompletionStreamResponse: *c}
	out.Usage = ToOpenAIChatCompletionUsage(c.Usage)
	return out
}

// INBOUND half. Lives here, not in decodeChatChunk, so the scan loop keeps
// only its own concerns and all four converters sit together.
// and chunk.Raw - while the conversion sits beside its three siblings.
func (c *OpenAIChatCompletionStreamResponse) ToDMChatCompletionStreamResponse() *core.DiffractLLMChatCompletionStreamResponse {
	out := c.DiffractLLMChatCompletionStreamResponse
	out.Usage = c.Usage.ToDMChatCompletionUsage()

	out.Type = c.streamEventType()
	return &out
}

// objectChatCompletionChunk is what OpenAI stamps on every streamed frame.
const objectChatCompletionChunk = "chat.completion.chunk"

// Classifies from OpenAI's wire, not from IR fields. Terminal frame = usage
// with empty choices; finish_reason is NOT it, and arrives a chunk earlier.
func (c *OpenAIChatCompletionStreamResponse) streamEventType() core.ChatStreamEventType {
	switch {
	case c.Object != objectChatCompletionChunk:
		return core.StreamEventError
	case len(c.Choices) == 0 && c.Usage != nil:
		return core.StreamEventComplete
	default:
		return core.StreamEventDelta
	}
}
```


`OpenAIChatCompletionUsage` therefore has two jobs, not one: `ToDMChatCompletionUsage` on the way in,
`ToOpenAIChatCompletionUsage` on the way out. It is no longer decode-only.

### `content` on the way IN — solved by the same type

The string-or-array split exists on the **request** side too, and it is the more
common case by far: the single most frequent client body in existence is

```json
{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}
```

`core.ChatContent` (§0) fixes both directions at once, which is why there is no
provider-side message type here at all. `OpenAIChatCompletionRequest.Messages` stays
`[]core.DiffractLLMChatMessage` and now accepts either form.

---

## 4. `internal/providers/openai/openai.go`

```go
package openai

import (
	"encoding/json"
	"io"
	"net/http"

	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

const chatCompletionPath = "/v1/chat/completions"

// dataPrefix, doneMarker and usageKey live in stream.go, beside the only loop
// that reads them.

// Holds a reference to the SHARED transport, never builds one. The atomic
// swap lives inside it, so a hot reload needs no change here.
type OpenAIProvider struct {
	Transport *dataplane.DiffractLLMTransport
	Logger    *zap.Logger
}

func (p *OpenAIProvider) ProviderName() core.Provider { return core.ProviderOpenAI }

func (p *OpenAIProvider) ChatCompletion(
	rctx *core.DiffractLLMContext,
	req *core.DiffractLLMChatCompletionRequest,
	cred *core.Credential,
) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {

	body, dErr := p.buildBody(req, cred, false)
	if dErr != nil {
		return nil, dErr
	}

	result, dErr := p.Transport.ServeHTTP(rctx, &dataplane.DiffractLLMTransportRequest{
		Method:  http.MethodPost,
		Path:    chatCompletionPath,
		Body:    body,
		Headers: p.headers(cred, false),
	}, cred)
	if dErr != nil {
		return nil, dErr
	}

	respBody, err := io.ReadAll(result.Body)
	result.Body.Close()
	if err != nil {
		return nil, core.NewUpstreamError(string(core.ProviderOpenAI), chatCompletionPath,
			result.Status, "reading response", err)
	}

	if result.Status != http.StatusOK {
		return nil, ParseOpenAIError(result.Status, respBody)
	}

	// Decode OpenAI's wire shape, then convert. NOT a direct unmarshal into
	// the IR: the usage vocabulary differs (prompt_tokens vs input_tokens) and
	// a direct decode silently yields a zero-token, zero-cost request. See §3c.
	var wire OpenAIChatCompletionResponse
	if err := sonic.Unmarshal(respBody, &wire); err != nil {
		return nil, core.NewUpstreamError(string(core.ProviderOpenAI), chatCompletionPath,
			result.Status, "unmarshalling response", err)
	}

	// Raw is json:"-", so unmarshal skips it - set it here, where the bytes
	// are, and the converter's lift carries it into the IR.
	wire.Raw = respBody
	return wire.ToDMChatCompletionResponse(), nil
}

// buildBody is the one place the passthrough decision is made.
func (p *OpenAIProvider) buildBody(
	req *core.DiffractLLMChatCompletionRequest,
	cred *core.Credential,
	stream bool,
) ([]byte, *core.DiffractLLMError) {

	model := cred.CheckModelAlias(req.Model)

	// Same-dialect passthrough: forward the caller's original bytes with only
	// the model rewritten, so a parameter the IR does not model yet survives
	// the trip. See patchRaw - this is about fidelity, not saving a marshal.
	if len(req.Raw) > 0 {
		body, err := patchRaw(req.Raw, model, stream)
		if err != nil {
			return nil, core.NewInvalidRequestBody("patching passthrough body", err)
		}
		return body, nil
	}

	wire := ToOpenAIChatCompletionRequest(req, model)
	if stream {
		wire.Stream = &stream
		// Force include_usage or the stream is unbillable. MERGE - replacing the
		// struct would reset the caller's include_obfuscation.
		if wire.StreamOptions == nil {
			wire.StreamOptions = &core.ChatStreamOptions{}
		}
		wire.StreamOptions.IncludeUsage = &stream
	}

	body, err := sonic.Marshal(wire)
	if err != nil {
		return nil, core.NewInternalError("openai", "marshalling chat request", err)
	}
	return body, nil
}

// headers applies auth. The provider owns this because only it knows the
// credential's shape - azure would set api-key here instead.
func (p *OpenAIProvider) headers(cred *core.Credential, stream bool) map[string]string {
	h := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + cred.APIKey,
	}
	if stream {
		h["Accept"] = "text/event-stream"
		h["Cache-Control"] = "no-cache"
	}
	return h
}

// patchRaw rewrites only model and stream_options, forwarding the caller's own
// bytes so an OpenAI param the IR does not model yet still reaches the provider.
func patchRaw(raw json.RawMessage, model string, stream bool) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := sonic.Unmarshal(raw, &m); err != nil {
		return nil, err
	}

	name, err := sonic.Marshal(model)
	if err != nil {
		return nil, err
	}
	m["model"] = name

	if stream {
		// MERGE, do not replace. Overwriting the object discards whatever else
		// the caller put there - a deliberate include_obfuscation:false for
		// bandwidth, or any key OpenAI adds later. Only include_usage is ours
		// to force.
		opts := map[string]json.RawMessage{}
		if existing, ok := m["stream_options"]; ok {
			if err := sonic.Unmarshal(existing, &opts); err != nil {
				return nil, err
			}
		}
		opts["include_usage"] = json.RawMessage(`true`)
		merged, err := sonic.Marshal(opts)
		if err != nil {
			return nil, err
		}
		m["stream_options"] = merged
	}
	return sonic.Marshal(m)
}
```

### Streaming — `openai.go`

The method itself stays thin: build, call, hand the body to the scanner. Every
streaming kind that follows (responses, text completion, transcription) is the
same four steps against a different path.

```go
func (p *OpenAIProvider) ChatCompletionStream(
	rctx *core.DiffractLLMContext,
	req *core.DiffractLLMChatCompletionRequest,
	cred *core.Credential,
) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError) {

	body, dErr := p.buildBody(req, cred, true)
	if dErr != nil {
		return nil, dErr
	}

	result, dErr := p.Transport.ServeHTTP(rctx, &dataplane.DiffractLLMTransportRequest{
		Method:      http.MethodPost,
		Path:        chatCompletionPath,
		Body:        body,
		Headers:     p.headers(cred, true),
		IsStreaming: true,
	}, cred)
	if dErr != nil {
		return nil, dErr
	}

	if result.Status != http.StatusOK {
		respBody, _ := io.ReadAll(result.Body)
		result.Body.Close()
		return nil, ParseOpenAIError(result.Status, respBody)
	}

	// passthrough is decided ONCE here, not per chunk.
	return scanChatSSE(rctx, result.Body, rctx.SDKProvider == core.ProviderOpenAI), nil
}
```

---

## 4b. `internal/providers/openai/stream.go`

The scan loop lives in its own file, called by `ChatCompletionStream` rather
than written inside it.

Not for cross-provider reuse — that stays speculative until azure is actually
written. For cross-KIND reuse inside this package: deferred-work C3 lists eight
more request kinds, and responses, text completion and transcription all stream
through this identical loop. Inline in one method, kind #2 copy-pastes it and
kind #5 fixes a bug in only one of the copies. The subtle parts that make that
expensive are all here: the 1MB buffer ceiling, `sc.Bytes()` being reused
between iterations, the passthrough branch, and usage detection.

```go
package openai

import (
	"bufio"
	"bytes"
	"io"

	"diffractllm/internal/core"

	"github.com/bytedance/sonic"
)

var (
	dataPrefix = []byte("data:")
	doneMarker = []byte("[DONE]")

	// usageKey is scanned for before parsing a chunk on the passthrough path.
	// Package-level so the slice is not rebuilt per line.
	usageKey = []byte(`"usage"`)
)

// Turns an SSE body into IR chunks and owns the body, closing it on every exit.
// The 256 buffer is backpressure: a slow client stalls this goroutine, not the heap.
func scanChatSSE(
	rctx *core.DiffractLLMContext,
	body io.ReadCloser,
	passthrough bool,
) <-chan *core.DiffractLLMChatCompletionStreamResponse {

	out := make(chan *core.DiffractLLMChatCompletionStreamResponse, 256)
	ctx := rctx.Context()

	go func() {
		defer close(out)
		defer body.Close()

		sc := bufio.NewScanner(body)
		// A single chunk can exceed bufio's 64KB default - a tool call with a
		// large argument blob will do it.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 || line[0] == ':' {
				continue // comment / keepalive
			}
			payload := bytes.TrimSpace(bytes.TrimPrefix(line, dataPrefix))
			if len(payload) == 0 {
				continue
			}
			if bytes.Equal(payload, doneMarker) {
				return
			}

			chunk := decodeChatChunk(payload, passthrough)
			if chunk == nil {
				continue // one bad line is not a reason to kill the stream
			}

			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

// Owns only the scan-loop concerns: passthrough, usage sniff, chunk.Raw.
// Conversion lives in ToDMChatCompletionStreamResponse. nil means skip.
func decodeChatChunk(payload []byte, passthrough bool) *core.DiffractLLMChatCompletionStreamResponse {
	if passthrough {
		// The client speaks this dialect, so the provider's own bytes are
		// already the right answer. Skip the unmarshal AND the re-marshal in
		// the writer - roughly 2-5us per chunk, on a stream that can be 2000
		// chunks long.
		chunk := &core.DiffractLLMChatCompletionStreamResponse{
			Type: core.StreamEventDelta,
			// sc.Bytes() is reused between iterations, so this must copy.
			ChatResponseEnvelope: core.ChatResponseEnvelope{
				Raw: append([]byte(nil), payload...),
			},
		}

		// Cheap pre-filter: `"usage"` cannot appear inside a JSON string value,
		// so a match is always the real usage chunk.
		if bytes.Contains(payload, usageKey) {
			var wire OpenAIChatCompletionStreamResponse
			if sonic.Unmarshal(payload, &wire) == nil && wire.Usage != nil {
				chunk.Usage = wire.Usage.ToDMChatCompletionUsage()
				chunk.Type = core.StreamEventComplete
			}
		}
		return chunk
	}

	// Full decode: OpenAIChatCompletionStreamResponse shadows usage, everything else lands in
	// the embedded IR chunk, and the converter maps the one field that differs.
	var wire OpenAIChatCompletionStreamResponse
	if err := sonic.Unmarshal(payload, &wire); err != nil {
		return nil
	}
	return wire.ToDMChatCompletionStreamResponse()
}
```

---

## 5. `internal/providers/openai/errors.go`

```go
package openai

import (
	"diffractllm/internal/core"
	"errors"
	"net/http"

	"github.com/bytedance/sonic"
)

type OpenAIErrorResponse struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
		Param   string `json:"param"`
	} `json:"error"`
}

// ParseOpenAIError classifies by STATUS, not by body: a gateway in front of
// OpenAI can return HTML on a 502, and the body is then unparseable.
func ParseOpenAIError(status int, body []byte) *core.DiffractLLMError {
	var e OpenAIErrorResponse
	_ = sonic.Unmarshal(body, &e)

	msg := string(body)
	if e.Error != nil && e.Error.Message != "" {
		msg = e.Error.Message
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return core.NewUpstreamAuth(string(core.ProviderOpenAI), "", msg)
	case http.StatusTooManyRequests:
		return core.NewUpstreamRateLimit(string(core.ProviderOpenAI), "", 0)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return core.NewUpstreamTimeout(string(core.ProviderOpenAI), "", errors.New(msg))
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return core.NewUpstreamUnavailable(string(core.ProviderOpenAI), "", errors.New(msg))
	default:
		out := core.NewUpstreamError(string(core.ProviderOpenAI), "", status, msg, nil)
		if e.Error != nil {
			out.ProviderErrorType = e.Error.Type
			out.ProviderErrorCode = e.Error.Code
		}
		return out
	}
}
```

---

## 6. `internal/server/registry.go`

```go
package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/providers"
	"net/http"
)

// Maps one inbound (sdk, path) to a kind and its conversions. The func fields
// are why the generic handler needs no per-SDK switch.
type RouteDescriptor struct {
	SDK         core.Provider
	Path        string
	Method      string
	RequestKind core.RequestKind

	// NewRequest allocates an empty provider-shaped request to unmarshal into.
	NewRequest func() providers.ProviderRequest

	// ToDiffract converts that into the IR.
	ToDiffract func(req any, rctx *core.DiffractLLMContext) any

	// FromDiffract converts the response back into the SDK's dialect.
	FromDiffract func(res any) any

	// Returns the SSE event NAME and the payload. Anthropic emits
	// "event: message_delta"; openai emits none, hence the two return values.
	FromDiffractStream func(chunk *core.DiffractLLMChatCompletionStreamResponse) (event string, payload any)
	FromDiffractError  func(err *core.DiffractLLMError) any
}

var routeDescriptors = []RouteDescriptor{
	{
		SDK:         core.ProviderOpenAI,
		Path:        "/v1/chat/completions",
		Method:      http.MethodPost,
		RequestKind: core.ChatRequest,

		NewRequest: func() providers.ProviderRequest { return &openai.OpenAIChatCompletionRequest{} },

		ToDiffract: func(req any, rctx *core.DiffractLLMContext) any {
			return req.(*openai.OpenAIChatCompletionRequest).ToDMChatCompletionRequest(rctx)
		},
		// Raw first: when the provider already produced this dialect its own
		// bytes are correct and cost no marshal. Otherwise CONVERT - returning
		// the IR would emit input_tokens/output_tokens to a client reading
		// prompt_tokens, which is undefined in every OpenAI SDK.
		FromDiffract: func(res any) any {
			r, ok := res.(*core.DiffractLLMChatCompletionResponse)
			if !ok {
				return res
			}
			if len(r.Raw) > 0 {
				return r.Raw
			}
			return openai.ToOpenAIChatCompletionResponse(r)
		},
		FromDiffractStream: func(c *core.DiffractLLMChatCompletionStreamResponse) (string, any) {
			// No event names in openai's SSE.
			if len(c.Raw) > 0 {
				return "", c.Raw
			}
			return "", openai.ToOpenAIChatCompletionStreamResponse(c)
		},
		FromDiffractError: func(err *core.DiffractLLMError) any { return err },
	},
}

// There is no descriptor lookup table: routes register CONCRETE paths and
// close over their own descriptor, so per request there is no map lookup, no
// key concat, no PathValue extraction, and no "not found" branch.
```

(`openai` joins the import block.)

---

## 7. `internal/server/routes.go`

```go
package server

import "net/http"

// Handler uses net/http.ServeMux. Go 1.22+ method patterns are all a gateway
// needs; routing is ~200ns against an upstream call of 200ms-60s.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	for i := range routeDescriptors {
		d := &routeDescriptors[i] // address of the slice element, not the loop copy
		pattern := d.Method + " /" + string(d.SDK) + d.Path
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			s.handleRequest(w, r, d)
		})
	}

	mux.HandleFunc("GET /healthz", s.health)
	return mux
}
```

---

## 8. `internal/server/handlers.go`

The ordering change vs provider-design.md is the point of this section:

```
acquire rctx -> AUTHENTICATE (headers only) -> read body -> parse -> Modelkey
             -> PreCall hooks (model access, budget) -> Resolve -> dispatch
```

Authentication is identity ("who are you", 401); model access and budget are
authorization ("what may you do", 403/400). Splitting them means an
unauthenticated caller never causes a body read, a buffer, or a JSON parse.

```go
package server

import (
	"bytes"
	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"
	"diffractllm/internal/providers"
	"io"
	"net/http"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

// Authenticator is the pre-body gate. Satisfied by governance's virtual key
// hook, which reads only request headers.
type Authenticator interface {
	Authenticate(rctx *core.DiffractLLMContext) *core.DiffractLLMError
}

// PriceSource is the catalog slice pricing needs. Interface at the consumer,
// same pattern as governance.ModelLookup; *modelcatalog.ModelCatalog satisfies
// it directly.
type PriceSource interface {
	ResolvePrice(virtualKeyID string, key core.CatalogKey, selectorKey string) *core.Pricing
}

type Server struct {
	ctxPool      *core.DiffractLLMContextPool
	auth         Authenticator
	hooks        *core.HookEngine
	engine       *dataplane.SelectionEngine
	transport    *dataplane.DiffractLLMTransport
	catalog      PriceSource
	logger       *zap.Logger
	maxBodyBytes int64
}

var reqErrKey = core.DiffractLLMContextKey("server.request_error")

// handleRequest receives the descriptor its route was registered with. There
// is no lookup, so there is no not-found path.
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request, desc *RouteDescriptor) {
	rctx := s.ctxPool.Acquire(r.Context(), r, w)
	defer func() {
		s.hooks.RunPostCallHooks(rctx)
		s.logRequest(rctx)
		s.ctxPool.Release(rctx)
	}()

	rctx.SDKProvider = desc.SDK
	rctx.RequestKind = desc.RequestKind

	// --- authenticate FIRST: headers only, zero body work for bad callers ---
	if dErr := s.auth.Authenticate(rctx); dErr != nil {
		writeErr(rctx, dErr)
		return
	}

	// --- body, read ONCE, pre-sized ---
	body, err := s.readBody(rctx)
	if err != nil {
		writeErr(rctx, err)
		return
	}
	rctx.BodyBytes = body

	// --- dialect -> IR ---
	wire := desc.NewRequest()
	if err := sonic.Unmarshal(body, wire); err != nil {
		writeErr(rctx, core.NewInvalidRequestBody("parsing request", err))
		return
	}
	irReq := desc.ToDiffract(wire, rctx)

	// --- routing identity: THIS is the producer rctx.Modelkey never had ---
	model := modelOf(irReq)
	provider, bare := core.ParseModelString(model, rctx.SDKProvider)
	rctx.RequestedModel = model
	rctx.Modelkey = core.CatalogKey{
		Provider:  provider,
		ModelName: bare,
		ModelType: desc.RequestKind.ModelType(),
	}

	// --- authorization hooks (model access, budget): need identity + model ---
	if dErr := s.hooks.RunPreCallHooks(rctx); dErr != nil {
		writeErr(rctx, dErr)
		return
	}

	// --- data plane picks provider + credential ---
	cred, dErr := s.engine.Resolve(rctx)
	if dErr != nil {
		writeErr(rctx, dErr)
		return
	}

	provInstance := providers.Get(rctx.Modelkey.Provider)
	if provInstance == nil {
		writeErr(rctx, core.NewInternalError("handler", "provider not registered", nil))
		return
	}

	// --- dispatch by kind ---
	switch desc.RequestKind {
	case core.ChatRequest:
		s.handleChat(rctx, desc, provInstance, cred,
			irReq.(*core.DiffractLLMChatCompletionRequest), wire.IsStreaming())
	default:
		writeErr(rctx, core.NewInternalError("handler", "unknown request kind", nil))
	}
}

// readBody pre-sizes from Content-Length instead of letting io.ReadAll double
// seven times, and rejects oversized bodies with 413 instead of truncating.
func (s *Server) readBody(rctx *core.DiffractLLMContext) ([]byte, *core.DiffractLLMError) {
	size := rctx.Request.ContentLength
	if size <= 0 || size > s.maxBodyBytes {
		size = 64 << 10
	}
	buf := bytes.NewBuffer(make([]byte, 0, size))
	if _, err := buf.ReadFrom(io.LimitReader(rctx.Request.Body, s.maxBodyBytes)); err != nil {
		return nil, core.NewInvalidRequestBody("reading body", err)
	}
	if int64(buf.Len()) >= s.maxBodyBytes {
		var probe [1]byte
		if n, _ := rctx.Request.Body.Read(probe[:]); n > 0 {
			return nil, newPayloadTooLarge(s.maxBodyBytes)
		}
	}
	return buf.Bytes(), nil
}

func (s *Server) handleChat(
	rctx *core.DiffractLLMContext,
	desc *RouteDescriptor,
	prov providers.Provider,
	cred *core.Credential,
	req *core.DiffractLLMChatCompletionRequest,
	stream bool,
) {
	// Same-dialect fast path. Raw was already set by ToDiffract; clear it when
	// the destination differs, so the provider marshals instead.
	if rctx.SDKProvider != rctx.Modelkey.Provider {
		req.Raw = nil
	}

	if stream {
		s.streamChat(rctx, desc, prov, cred, req)
		return
	}

	out, dErr := prov.ChatCompletion(rctx, req, cred)
	if dErr != nil {
		writeErr(rctx, dErr)
		return
	}

	rctx.Usage = out.Usage
	s.price(rctx)
	rctx.JSON(http.StatusOK, desc.FromDiffract(out))
}

func (s *Server) streamChat(
	rctx *core.DiffractLLMContext,
	desc *RouteDescriptor,
	prov providers.Provider,
	cred *core.Credential,
	req *core.DiffractLLMChatCompletionRequest,
) {
	if _, ok := rctx.Writer.(http.Flusher); !ok {
		writeErr(rctx, core.NewInternalError("handler", "client does not support streaming", nil))
		return
	}

	ch, dErr := prov.ChatCompletionStream(rctx, req, cred)
	if dErr != nil {
		writeErr(rctx, dErr)
		return
	}

	rctx.SetHeader("Content-Type", "text/event-stream")
	rctx.SetHeader("Cache-Control", "no-cache")
	rctx.SetHeader("Connection", "keep-alive")
	rctx.SetHeader("X-Accel-Buffering", "no") // stop nginx buffering the stream
	rctx.Writer.WriteHeader(http.StatusOK)
	rctx.ResponseStatus = http.StatusOK
	rctx.Flush()

	ctx := rctx.Context()

	for chunk := range ch {
		// A client that hung up must be told apart from a stream that finished.
		select {
		case <-ctx.Done():
			rctx.StreamAborted = true
			drain(ch) // let the provider goroutine finish and close
			return
		default:
		}

		if chunk.Type == core.StreamEventError {
			// Pass the event name through, never hardcode "". Anthropic errors
			// carry one; hardcoding loses it the day that converter lands.
			event, payload := desc.FromDiffractError(chunk.Error)
			writeSSE(rctx, event, payload)
			return
		}
		// Two facts on two DIFFERENT chunks: finish_reason arrives before usage,
		// so reading it inside the usage branch always saw empty choices.
		if chunk.Usage != nil {
			rctx.Usage = chunk.Usage
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != nil {
			rctx.StreamFinishReason = derefFinish(chunk.Choices[0].FinishReason)
		}

		rctx.StreamChunks++
		event, payload := desc.FromDiffractStream(chunk)
		writeSSE(rctx, event, payload)
	}

	rctx.Write([]byte("data: [DONE]\n\n"))
	rctx.Flush()
	rctx.RequestCompleted = true

	s.price(rctx)
}

// drain empties the channel so the provider goroutine is not blocked on a send
// forever. Without it, a client disconnect leaks a goroutine and an upstream
// connection per abandoned stream. The goroutine ALSO selects on ctx.Done(), so
// this returns almost immediately and the upstream body closes on its defer.
func drain(ch <-chan *core.DiffractLLMChatCompletionStreamResponse) {
	for range ch {
	}
}

// price is the only place cost is computed - providers never price, matching
// bifrost's split where CalculateCost lives outside the provider path.
// It reads rctx.Usage and nothing else, so it does not care whether usage came
// from a parsed body, a translated stream chunk, or a sniffed passthrough line.
func (s *Server) price(rctx *core.DiffractLLMContext) {
	if rctx.Usage == nil {
		return
	}
	pricing := s.catalog.ResolvePrice(rctx.VirtualKeyID, rctx.Modelkey, "")
	if pricing == nil {
		s.logger.Warn("no pricing row", zap.String("model", rctx.Modelkey.SlashKey()))
		return
	}
	rctx.Cost = core.CalculateCost(*pricing, *rctx.Usage)
}

// --- request log: one structured line per request, the "easy logging" story ---

// logRequest emits the single per-request record. It reads ONLY fields already
// on rctx - no threading, no parallel type - and runs before Release so pooled
// state is still valid.
func (s *Server) logRequest(rctx *core.DiffractLLMContext) {
	fields := []zap.Field{
		zap.String("event", "request"),
		zap.String("sdk", string(rctx.SDKProvider)),
		zap.String("kind", string(rctx.RequestKind)),
		zap.String("model", rctx.Modelkey.SlashKey()),
		zap.String("requested_model", rctx.RequestedModel),
		zap.Int("response_status", rctx.ResponseStatus),
		zap.Int("upstream_status", rctx.UpstreamStatus),
		zap.Duration("ttfb", rctx.TTFB),
		zap.Int("response_bytes", rctx.ResponseBytes),
		zap.Bool("completed", rctx.RequestCompleted),
		zap.Duration("hooks_pre_call", rctx.HookLog.PreCallTotal),
		zap.Duration("hooks_post_provider", rctx.HookLog.PostProviderTotal),
		zap.Duration("hooks_post_call", rctx.HookLog.PostCallTotal),
	}

	if rctx.VirtualKeyID != "" {
		fields = append(fields,
			zap.String("virtual_key_id", rctx.VirtualKeyID),
			zap.String("client_id", rctx.ClientID),
			zap.String("budget_ref", rctx.BudgetRef),
		)
	}
	if cred := rctx.SelectedCredential; cred != nil {
		fields = append(fields,
			zap.String("credential_id", cred.ID),
			zap.String("credential_name", cred.Name),
		)
	}

	if rctx.StreamChunks > 0 || rctx.StreamAborted {
		fields = append(fields,
			zap.Int32("stream_chunks", rctx.StreamChunks),
			zap.String("finish_reason", string(rctx.StreamFinishReason)),
			zap.Bool("stream_aborted", rctx.StreamAborted),
		)
	}
	if u := rctx.Usage; u != nil {
		fields = append(fields,
			zap.Int64("input_tokens", u.InputTokens),
			zap.Int64("output_tokens", u.OutputTokens),
			zap.Int64("cached_input_tokens", u.CachedInputTokens),
			zap.Int64("reasoning_tokens", u.ReasoningTokens),
			zap.Float64("cost", rctx.Cost),
		)
	}
	if v, ok := rctx.Get(reqErrKey); ok {
		if err, isErr := v.(*core.DiffractLLMError); isErr {
			fields = append(fields,
				zap.String("error_code", string(err.Code)),
				zap.String("error_category", string(err.ErrorCategory)),
				zap.String("error_message", err.Message),
			)
		}
	}

	s.logger.Info("request complete", fields...)
}

var (
	sseData = []byte("data: ")
	sseEnd  = []byte("\n\n")
	sseEvt  = []byte("event: ")
)

// Emits one SSE frame. Always via rctx.Write so ResponseBytes stays accurate;
// a []byte payload is the passthrough path and skips the marshal.
func writeSSE(rctx *core.DiffractLLMContext, event string, payload any) {
	if event != "" { // anthropic-style named events; openai sends none
		rctx.Write(sseEvt)
		rctx.Write([]byte(event))
		rctx.Write(sseEnd[:1])
	}

	rctx.Write(sseData)
	switch v := payload.(type) {
	case []byte:
		rctx.Write(v)
	default:
		b, err := sonic.Marshal(v)
		if err != nil {
			return
		}
		rctx.Write(b)
	}
	rctx.Write(sseEnd)

	rctx.Flush() // without this the client sees nothing until the buffer fills
}

// writeErr renders a typed error in the client's dialect, records the status,
// and stashes the error where logRequest will find it.
func writeErr(rctx *core.DiffractLLMContext, err *core.DiffractLLMError) {
	rctx.ResponseStatus = err.StatusCode
	rctx.Overwrite(reqErrKey, err)
	rctx.JSON(err.StatusCode, err)
}
```

---

## 8b. Small helpers, plus the governance change

```go
// --- internal/server ---

func newPayloadTooLarge(limit int64) *core.DiffractLLMError {
	return &core.DiffractLLMError{
		ErrorCategory: core.ErrorCategoryClient,
		Code:          core.CodeInvalidRequestBody,
		Message:       "request body exceeds max_body_size",
		Type:          "invalid_request_error",
		StatusCode:    http.StatusRequestEntityTooLarge,
		Component:     "validator",
		Details: map[string]interface{}{
			"internal_detail": fmt.Sprintf("body limit is %d bytes", limit),
		},
	}
}

// modelOf pulls the model name out of whichever IR request this is. A tiny
// switch rather than an interface method, because the IR types are data and
// should not grow behaviour for the server's convenience.
func modelOf(req any) string {
	switch r := req.(type) {
	case *core.DiffractLLMChatCompletionRequest:
		return r.Model
	case *core.DiffractLLMCompletionRequest:
		return r.Model
	}
	return ""
}

// derefFinish flattens the response's *FinishReason for rctx.
func derefFinish(f *core.FinishReason) core.FinishReason {
	if f == nil {
		return ""
	}
	return *f
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"ok"}`)
}

// Execute; the separate name documents WHERE it runs in the request lifecycle
// (before the body is read).
func (a *VirutalkeyHook) Authenticate(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	return a.Execute(rctx)
}
```

```go
// internal/governance/hooks.go - updated registration
func RegisterHooks(engine *core.HookEngine, logger *zap.Logger, governance *Governance, catalog ModelLookup) {
	// NOTE: the virtual-key auth hook is NOT registered here anymore. It runs
	// earlier, as the server's Authenticator, before the request body is read -
	// an unauthenticated caller must not cause buffering or JSON parsing.
	engine.AddPreCallHook(NewModelAccessHook(catalog, logger))
	engine.AddPreCallHook(NewBudgetCheckHook(governance.BudgetCache, logger))
	engine.AddPreProviderHook(&dummyBudgetHook{logger: logger})
	engine.AddPostProviderHook(&dummyUsageHook{logger: logger})
	engine.AddPostCallHook(&dummyAuditHook{logger: logger})
}
```

Trade recorded: auth duration no longer lands in `HookLog.PreCallTotal`
(it is not a PreCall run anymore). The hook's own `auth ok` debug line covers
it; adding a fifth HookEngine phase for one hook was rejected as machinery.

---

## 9. Boot wiring

```go
package server

type Deps struct {
	Hooks        *core.HookEngine
	Auth         Authenticator
	Engine       *dataplane.SelectionEngine
	Transport    *dataplane.DiffractLLMTransport
	Catalog      PriceSource
	Logger       *zap.Logger
	MaxBodyBytes int64 // from config.ServerConfig.MaxBodySize << 10
}

func New(deps Deps) *Server {
	return &Server{
		ctxPool:      core.NewDiffractLLMContextPool(),
		auth:         deps.Auth,
		hooks:        deps.Hooks,
		engine:       deps.Engine,
		transport:    deps.Transport,
		catalog:      deps.Catalog,
		logger:       deps.Logger,
		maxBodyBytes: deps.MaxBodyBytes,
	}
}

// Rebuilds every provider client after the plane mutates. NOTHING CALLS THIS
// YET - pair it with plane.UpsertProviderConfigs when the admin API lands.
func (s *Server) SyncUpstreams(upstreams map[core.Provider]*core.Upstream) {
	s.transport.Replace(upstreams)
}
```

main-side sketch:

```go
cfg := config.GlobalConfig()

logger := logging.New(cfg.Observability.LogLevel)

dbSource, _ := dbstore.NewDBSource(logger)
dbSource.Init()
upstreamList, creds, _ := dbSource.Load()

upstreams := make(map[core.Provider]*core.Upstream, len(upstreamList))
for _, up := range upstreamList {
	upstreams[up.Provider] = up
}

plane := providerplane.NewProviderPlane(upstreamList, creds)
engine := dataplane.NewSelectionEngine(plane, logger) // per dataplane-design.md

transport := dataplane.NewTransport(cfg.Upstream, upstreams, logger)

hooks := core.NewHookEngine(logger)
catalog := modelcatalog.NewModelCatalog(dbSource.GetStore(), *cfg.ModelCatalog, logger)
governance.RegisterHooks(hooks, logger, gov, catalog)

srv := server.New(server.Deps{
	Hooks:        hooks,
	Auth:         gov.NewVirtualkeyAuthenticator(), // the hook, exposed
	Engine:       engine,
	Transport:    transport,
	Catalog:      catalog,
	Logger:       logger,
	MaxBodyBytes: int64(cfg.ServerConfig.MaxBodySize) << 10,
})

http.ListenAndServe(fmt.Sprintf(":%d", cfg.ServerConfig.Port), srv.Handler())
```

---

## Build order

1. `dataplane/transport.go` — depends only on `core`.
2. `providers/providers.go` — interface, map, helpers.
3. `providers/openai/` — in this order, each buildable alone:
   `types.go` (wire shapes) → `chat.go` (codec, no I/O — testable immediately,
   see Verification #1 and #2) → `response.go` (usage codec + the one-field response shadow — see Verification #0) → `errors.go` → `stream.go` (scan loop, testable
   against a `strings.Reader`) → `openai.go` (needs the transport from step 1).
4. `server/` — registry, routes, handlers, wiring; governance two-liner above.

## Verification

Carried forward from provider-design.md, plus five for the fixes:

0. **Usage actually converts** — the one that would have caught the §3c bug, and
   it needs no network. Feed a real OpenAI body with
   `{"usage":{"prompt_tokens":1200,"completion_tokens":50,"total_tokens":1250,
   "prompt_tokens_details":{"cached_tokens":1000}}}` through
   `ToDMChatCompletionResponse` and assert `InputTokens == 1200`,
   `OutputTokens == 50`, `CachedInputTokens == 1000`. Then feed the result to
   `CalculateCost` and assert it is **non-zero** — the failure mode was a
   structurally valid, zero-token, zero-cost request. Assert the same for the
   streaming usage chunk via `decodeChatChunk`, on both the passthrough and the
   convert path.
0b. **Cached tokens are not double-subtracted** — with the numbers above, cost
   must equal 200 uncached + 1000 cached + 50 output, NOT 200 uncached with the
   cached portion free. Pricing subtracts; the converter must not.
0d. **Cache-write tokens bill at the premium rate** — feed a usage block with
   `prompt_tokens_details:{"cached_tokens":1000,"cache_write_tokens":500}` and
   assert `CacheCreationTokens == 500` survives into `CalculateCost`, charged at
   `cacheCreationRate` and NOT at zero. This is the regression test for the gap
   the doc audit found; before it, every cache write was free.
   Assert `image_tokens` → `InputImageTokens` in the same pass.
0e. **`stream_options` is merged, never replaced** — a streaming request with no
   `stream_options` must marshal `{"include_usage":true}` with
   `include_obfuscation` ABSENT, not `false`. A client sending
   `{"include_obfuscation":false}` must get it back untouched with
   `include_usage:true` added. Run against both paths: `buildBody` (struct) and
   `patchRaw` (raw map). Sending an explicit `false` here switches off OpenAI's
   SSE side-channel padding for someone who never asked.
0f. **All four converters round-trip** — chunk through
   `ToDMChatCompletionStreamResponse` → `ToOpenAIChatCompletionStreamResponse` returns the original
   `prompt_tokens`/`completion_tokens`; the terminal chunk's empty `choices`
   array survives both directions; a delta chunk keeps `Type == StreamEventDelta`
   and the terminal one flips to `StreamEventComplete`.
0c. **`ChatContent` accepts both wire forms** — `"content":"Hello"` on a REQUEST
   yields one `ContentText` part (this is the most common client body there is,
   and it does not decode without the type); the array form still decodes to N
   parts; `"content":null` on a tool-call turn yields nil, not a zero-length
   slice. Assert the same on a response, and once nested inside
   `OpenAIChatCompletionResponse` — a custom unmarshaller has to still fire two embeds
   deep, which is not obvious from reading it.
1. **`OpenAIChatCompletionRequest` flattens** — marshal; assert `temperature` top-level.
2. **Round trip, no API key** — real OpenAI body → `ToDMChatCompletionRequest` →
   `ToOpenAIChatCompletionRequest` → marshal → byte-equality. Run with tools,
   `tool_choice`, multimodal content, `response_format`.
3. **Live non-stream call** — `gpt-4o-mini`; content non-empty, usage > 0.
4. **Pricing** — `CalculateCost` non-zero against the `openai/gpt-4o-mini` row.
5. **Passthrough** — body sent upstream is `rctx.BodyBytes` with only `model`
   changed; `ToOpenAIChatCompletionRequest` never called.
6. **Streaming** — `delta…delta → complete`, final chunk carries `Usage`,
   `[DONE]` last.
7. **Errors** — stub 401/429/500 maps correctly; non-JSON body does not panic.
8. **SSRF guard** — endpoint resolving to `127.0.0.1` / `169.254.169.254` /
   `10.x` refused unless `AllowPrivateNetwork`; hostname resolving to a blocked
   IP refused too.
9. **Retry** — 503×2 then 200 succeeds; **500 not retried**; `Retry-After: 2`
   honoured.
10. **Retry re-sends the body** byte-identically.
11. **Response cap** errors past `MaxResponseBytes`; streams unaffected.
12. **Timeout excludes streams** — 2s unary cap kills a 3s unary call, not a
    30s stream.
13. **No redirect followed** — 302 surfaces as-is, credential not relayed.

New, for the fixes:

14. **Auth precedes body work** — a request with a garbage API key and a 32MB
    body returns 401 and the handler never allocates a conversation-sized
    buffer (assert via a counting `io.Reader` or by observing no parse error
    variant fires).
15. **Oversize body → 413**, not silent truncation: `Content-Length` honest and
    dishonest (chunked) variants both probed.
16. **Hot swap** — `ServeHTTP` through a stub server, then
    `Replace` with a different proxy/upstream; the NEXT call uses the new
    client, and a Replace containing an invalid proxy logs and keeps the old
    entry for that provider.
17. **Retry overwrites upstream headers** — first attempt returns
    `x-request-id: a`, retried attempt `x-request-id: b`; assert `b` survives
    in rctx metadata (regression guard for the `Set` duplicate-key bug).
18. **Request log smoke** — one successful call and one failing call emit one
    `request complete` line each, carrying vk/model/status/cost/error_code.

## Performance validation

Unchanged in shape from provider-design.md; restated for completeness.

Once per request — noise (route ~100ns, pool acquire ~20ns, sonic parse/marshal
5–50µs, hooks + resolve ~200ns). Gateway overhead outside the marshals: under
1µs against a 200ms–60s upstream call. The pre-sizing fix removes ~6 of 7 body
allocations. The neutral `TransportResult` adds zero cost — it renames what was
already being copied conceptually.

Per chunk — the term that scales. Measured on the real types (Ryzen 7745HX):

```
BenchmarkChunkUnmarshal-16     1436 ns/op    686 B/op   5 allocs/op
BenchmarkChunkRoundTrip-16     2116 ns/op    984 B/op   7 allocs/op   ← translating
BenchmarkChunkPassthrough-16    426 ns/op    256 B/op   1 allocs/op   ← copy + usage scan
```

2,000-chunk stream: 4.2ms translating, **0.85ms passthrough**. Passthrough
triggers automatically on `SDKProvider == provider` — always-correct condition,
no config flag.

Accepted costs, unchanged: one goroutine (~8KB) + 256-slot channel (~2KB) per
stream (~100MB at 10k concurrent — pull-based iterator deferred, A2);
one `chunk.Raw` copy per chunk; one `map[string]json.RawMessage` per
passthrough request in `patchRaw`.

Still the setting that dwarfs everything: `MaxIdleConnsPerHost` (default 2 =
fresh TLS handshakes under load). Set. Verified in §1.
