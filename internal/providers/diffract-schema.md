# DiffractLLM internal schema

Status: proposed. Scope: the intermediate representation only — no translation
functions, no provider code, no transport.

## Purpose

Every request arrives in some SDK's dialect and leaves in some provider's. The
schema here is the shape in the middle, so N dialects and M providers cost N+M
translations instead of N×M.

It is deliberately **not** OpenAI's schema renamed. LiteLLM took that route and
inherited OpenAI's quirks as its permanent contract; anything OpenAI does not
model has nowhere to live. It is also not a maximal union of every provider —
that grows without bound and every field becomes optional.

The rule: **model what a request means, carry what it does not.** Concepts every
provider has get typed fields; everything else rides in `Extra`.

---

## 1. Design rules

These are load-bearing. Each one exists because of a specific failure.

### 1.1 One usage type, and it is `core.Usage`

`core.Usage` already exists (`pricing.go:660`) with 48 fields and is what
`CalculateCost` consumes. Every response kind embeds **that** type.

`kind-speech.go` currently declares its own:

```go
type DiffractLLMSpeechUsage struct {
	InputTokens  int32
	OutputTokens int32
	TotalTokens  int32
}
```

Three `int32` fields against the 48 `int64`/`float64` fields pricing needs. A
speech response carrying that cannot be billed for audio seconds, cached tokens
or characters — the pricing engine simply cannot read it. **That type is deleted**
and replaced with `core.Usage`.

Consequence: a per-kind response only populates the fields its modality uses.
Everything else stays zero, and `CalculateCost` multiplies zero by the rate.

### 1.2 Raw bytes travel with every request

```go
Raw []byte
```

When the client's dialect and the destination provider share a wire format —
OpenAI SDK to OpenAI or Azure — the pipeline forwards `Raw` with only the model
field rewritten. No unmarshal into typed fields, no re-marshal, and nothing the
schema does not model is lost.

Bifrost has the same field (`BifrostChatRequest.RawRequestBody`) behind a context
flag. Here it is the default whenever dialects match, because for the first
providers that is always the correct answer and it is also the fastest one.

`Raw` is `json:"-"` — it is a pipeline concern, never serialised.

### 1.3 `Extra` on every request and response

```go
Extra map[string]any `json:"-"`
```

Anything the schema does not model. Without it, a provider parameter the schema
has not caught up with is silently dropped: the request succeeds while ignoring
what the caller asked for, which is worse than failing.

It is `json:"-"` because it is merged into the provider payload at construction
time, not serialised as a nested object.

**Cost, stated honestly:** a `map[string]any` allocates and forces reflection at
merge time. It is nil on the raw fast path and nil for the common typed request,
so the cost lands only on requests that actually carry unmodelled fields.

### 1.4 Pointer only where absent differs from zero

`Temperature *float64` — because `0` is a valid temperature and means something
different from "not set", and forwarding `temperature: 0` when the caller omitted
it changes the provider's behaviour.

`Stream bool` — because `false` and absent are the same request.

Applied per field, not per struct. Gratuitous pointers cost an allocation each
and a nil check at every use.

### 1.5 `omitzero`, not `omitempty`, for structs

`encoding/json` ignores `omitempty` on a non-pointer struct, so a struct field
tagged `omitempty` always serialises as `{}`. Go 1.24's `omitzero` is the one
that works. The module is on 1.25.

### 1.6 Routing identity does not live in the payload

`kind-speech.go` currently has:

```go
type DiffractLLMSpeechRequest struct {
	Provider Provider
	Model    string
	...
}
```

`Provider` is routing identity and already lives on `rctx.Modelkey` — the data
plane sets it, and it can change during resolution when a bare model name is
weighted across providers. A copy inside the payload is a second source of truth
that goes stale.

`Model` **does** stay, because the value that goes on the wire is the
alias-resolved upstream name, which differs from `rctx.Modelkey.ModelName`.

---

## 2. File layout

One file per request kind, matching the existing `kind-speech.go` convention.

```
internal/core/
  kind-types.go          RequestKind, shared enums, DiffractLLMContextKey
  kind-shared.go         Message, Content, Tool, ToolCall, StreamEvent  (new)
  kind-chat.go           chat + completion                             (new)
  kind-responses.go      responses API                                 (new)
  kind-embedding.go      embeddings                                    (new)
  kind-image.go          image generation + edit                       (new)
  kind-speech.go         text to speech                                (rewrite)
  kind-transcription.go  speech to text                                (new)
  kind-moderation.go     moderation                                    (new)
  kind-models.go         model listing                                 (new)
```

Existing types reused, not redefined: `core.Provider`, `core.CatalogKey`,
`core.ModelType`, `core.Capability`, `core.Usage`, `core.ServiceTier`,
`core.DiffractLLMError`, `core.DiffractLLMContext`.

---

## 3. `kind-types.go` — request kinds

`RequestKind` today holds one constant (`SpeechRequest`). It becomes the full set
and is what the descriptor registry keys on.

```go
package core

// RequestKind identifies the operation shape. It is the API surface the client
// called, distinct from ModelType, which is what the catalog says the model is.
// They usually agree; when they do not, the catalog wins for pricing and the
// request kind wins for payload construction.
type RequestKind string

const (
	ChatRequest          RequestKind = "chat"
	CompletionRequest    RequestKind = "completion"
	ResponsesRequest     RequestKind = "responses"
	EmbeddingRequest     RequestKind = "embedding"
	ImageGenRequest      RequestKind = "image_generation"
	ImageEditRequest     RequestKind = "image_edit"
	SpeechRequest        RequestKind = "speech"
	TranscriptionRequest RequestKind = "transcription"
	ModerationRequest    RequestKind = "moderation"
	ModelsRequest        RequestKind = "models"
)

// ModelType returns the catalog type this kind prices against. Chat and
// responses both price as their own mode; the catalog files each model under
// exactly one, which is why the price key never includes the request kind.
func (k RequestKind) ModelType() ModelType {
	switch k {
	case ChatRequest:
		return ModelTypeChat
	case CompletionRequest:
		return ModelTypeCompletion
	case ResponsesRequest:
		return ModelTypeResponses
	case EmbeddingRequest:
		return ModelTypeEmbedding
	case ImageGenRequest:
		return ModelTypeImageGeneration
	case ImageEditRequest:
		return ModelTypeImageEdit
	case SpeechRequest:
		return ModelTypeAudioSpeech
	case TranscriptionRequest:
		return ModelTypeAudioTranscription
	case ModerationRequest:
		return ModelTypeModeration
	default:
		return ModelTypeUnknown
	}
}
```

---

## 4. `kind-shared.go` — types used by more than one kind

```go
package core

// Role is the author of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer" // newer OpenAI models; system for the rest
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentType discriminates a ContentPart.
type ContentType string

const (
	ContentText       ContentType = "text"
	ContentImageURL   ContentType = "image_url"
	ContentInputAudio ContentType = "input_audio"
	ContentFile       ContentType = "file"
	ContentRefusal    ContentType = "refusal"
	ContentThinking   ContentType = "thinking" // anthropic reasoning blocks

	// ContentServerToolUse and ContentServerToolResult record a tool the
	// PROVIDER ran inside the turn. They are informational - the work is
	// already done - but they must be captured, because they are billed.
	ContentServerToolUse    ContentType = "server_tool_use"
	ContentServerToolResult ContentType = "server_tool_result"
)

// ContentPart is one element of a message's content.
//
// A flat struct with a discriminator rather than an interface: an interface
// costs a heap allocation and a type switch per part, and a long conversation
// carries hundreds. Unused fields are nil pointers, which cost one word each.
type ContentPart struct {
	Type ContentType `json:"type"`

	Text *string `json:"text,omitempty"` // ContentText, ContentRefusal, ContentThinking

	// Signature is the opaque proof a provider attaches to a reasoning block.
	// Anthropic rejects a follow-up turn whose thinking block comes back
	// without it, so it must round-trip verbatim.
	//
	// It lives on the part, not on the Message, because a response may carry
	// several thinking blocks and each has its own signature. A single field on
	// the Message could not say which signature belonged to which block.
	Signature *string `json:"signature,omitempty"` // ContentThinking

	// ContentImageURL. URL may be an https link or a data: URI.
	ImageURL    *string `json:"image_url,omitempty"`
	ImageDetail *string `json:"image_detail,omitempty"` // auto | low | high

	// ContentInputAudio. Data is base64; Format is wav | mp3.
	AudioData   *string `json:"audio_data,omitempty"`
	AudioFormat *string `json:"audio_format,omitempty"`

	// ContentFile. Exactly one of ID or Data.
	FileID   *string `json:"file_id,omitempty"`
	FileName *string `json:"file_name,omitempty"`
	FileData *string `json:"file_data,omitempty"` // base64

	// ContentServerToolUse / ContentServerToolResult. ServerToolName is the
	// class that ran ("web_search"); Payload is the provider's query or result
	// block, forwarded verbatim because its shape is provider-specific and we
	// only need to count it, not read it.
	ServerToolName ServerToolType  `json:"server_tool_name,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`

	// CacheControl marks a prefix boundary for providers that support explicit
	// prompt caching. Nil means the provider's default behaviour.
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// CacheControl is anthropic-shaped because anthropic is the only provider that
// takes it explicitly; OpenAI caches automatically and ignores it.
type CacheControl struct {
	Type string `json:"type"`          // "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "5m" | "1h"
}

// Message is one turn.
//
// Content is ALWAYS a []ContentPart, even for a plain text turn. The wire
// formats accept either a bare string or an array, but carrying both shapes in
// the IR would force every adapter, token counter and hook to branch on which
// one is set - and nothing in the type system could stop a caller setting both
// or neither. One representation costs roughly 88 bytes on a text-only turn and
// removes that branch everywhere, permanently.
type Message struct {
	Role    Role          `json:"role"`
	Content []ContentPart `json:"content,omitempty"`

	Name string `json:"name,omitempty"` // optional participant name

	// Assistant turns.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Refusal   *string    `json:"refusal,omitempty"`

	// Tool turns.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// TextMessage builds the common single-text-part turn, so constructing one is
// not three lines at every call site.
func TextMessage(role Role, text string) Message {
	return Message{Role: role, Content: []ContentPart{{Type: ContentText, Text: &text}}}
}

// Text concatenates the message's text parts, skipping images, audio, thinking
// and refusals. Adapters that flatten a turn to a string - anthropic's system
// prompt, for one - use this instead of walking the slice themselves.
func (m *Message) Text() string {
	if len(m.Content) == 1 && m.Content[0].Type == ContentText && m.Content[0].Text != nil {
		return *m.Content[0].Text // the common turn: no builder, no allocation
	}
	var b strings.Builder
	for i := range m.Content {
		if m.Content[i].Type == ContentText && m.Content[i].Text != nil {
			b.WriteString(*m.Content[i].Text)
		}
	}
	return b.String()
}

// ToolKind separates the two things that arrive in a "tools" array. They are
// not variations on one idea - they differ in WHO RUNS THE CODE.
type ToolKind string

const (
	// ToolKindFunction: the caller runs it. The model returns a ToolCall, the
	// caller executes it, and the result comes back as a RoleTool message.
	ToolKindFunction ToolKind = "function"

	// ToolKindServer: the PROVIDER runs it, inside the same turn. Nothing comes
	// back for the caller to execute; the model just uses the result. There is
	// no schema because we never call it - and it is billed separately, which
	// is what makes it a routing and accounting concern, not just a payload one.
	ToolKindServer ToolKind = "server"
)

// Tool is something the model may use.
//
// Flat, not OpenAI's {"type":"function","function":{...}} envelope. Of the three
// providers in scope, only OpenAI nests: anthropic and gemini are both flat, so
// carrying the envelope in the IR would mean two of three adapters begin by
// unwrapping it, and every read is tool.Function.Name behind a nil check.
// OpenAI's adapter adds the envelope on the way out - one place, one direction.
type Tool struct {
	Kind ToolKind `json:"kind"`

	// Function tools (Kind == ToolKindFunction).
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // JSON Schema, never parsed
	Strict      *bool           `json:"strict,omitempty"`

	// Server tools (Kind == ToolKindServer).
	Server *ServerTool `json:"server,omitempty"`
}

// ServerToolType is the class of provider-executed tool. The class is neutral -
// each provider package maps it to its own wire identifier, which is dated on
// anthropic ("web_search_20250305") and undated on openai ("web_search").
//
// Measured against bifrost's inventory, seven classes are served by both
// providers, which is why a neutral class is worth having at all.
type ServerToolType string

const (
	ServerToolWebSearch       ServerToolType = "web_search"
	ServerToolWebFetch        ServerToolType = "web_fetch"
	ServerToolCodeInterpreter ServerToolType = "code_interpreter"
	ServerToolComputer        ServerToolType = "computer"
	ServerToolFileSearch      ServerToolType = "file_search"
	ServerToolMemory          ServerToolType = "memory"
	ServerToolMCP             ServerToolType = "mcp"
)

// ServerTool describes a provider-executed tool. Three fields.
//
// An earlier draft also typed MaxUses, AllowedDomains and BlockedDomains on a
// guess that both providers share them. That guess was not verified, so they
// were cut - they live in Config until the openai and anthropic adapters are
// both written and the overlap is measured. Promoting a field later is one
// commit; typing it now on speculation is how a schema accumulates fields
// nobody reads.
//
// Bifrost took the other route: 21 variant-specific fields inlined onto one
// 25-field ChatTool struct (chatcompletions.go:396), which then needed custom
// MarshalJSON, UnmarshalJSON and a normalizeShape() that nils 15 fields on every
// tool of every request, because nothing in the type system stops
// Type="computer_20251124" from also carrying Function.
type ServerTool struct {
	Type ServerToolType `json:"type"`

	// Version pins the provider's exact wire identifier. Empty is the normal
	// case and means "whatever this provider currently calls it", supplied by
	// the provider's own table. Setting it is an explicit statement about which
	// provider serves the request - see §4a.3.
	Version string `json:"version,omitempty"`

	// Config is the provider-specific settings, forwarded verbatim and never
	// parsed: max_uses and allowed_domains for web_search, display_width_px for
	// computer use, vector_store_ids for file_search, server URL and auth for
	// mcp. Raw bytes because we route on the class, not on the settings.
	Config json.RawMessage `json:"config,omitempty"`
}

// ToolCall is the model asking for a function to be run. It is always complete:
// a ToolCall that exists has a name and fully assembled arguments.
//
// Note this is the inverse of Tool. Tool DESCRIBES a function the model may
// call - name, description, and a JSON Schema for its parameters. ToolCall
// INVOKES one - an id, the chosen name, and concrete argument values. They
// travel in opposite directions and share no fields beyond the name.
// A ToolCall is ALWAYS a function call. Server-tool invocations are not
// ToolCalls - the provider already ran them, so there is nothing to execute.
// They arrive as ContentServerToolUse / ContentServerToolResult parts instead.
// The rule is: ToolCall means you must act; a server-tool part means it is done,
// record it.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Arguments is the model's JSON as a string, not a parsed object. Providers
	// do not guarantee it is valid JSON, and parsing it here would only mean
	// re-serialising it downstream.
	Arguments string `json:"arguments"`
}

// ToolCallDelta is a FRAGMENT of a tool call arriving over a stream. It is a
// separate type from ToolCall on purpose.
//
// A streamed tool call does not arrive whole - the id and name come in the
// first fragment, then the arguments arrive a few characters at a time. Worse,
// when the model calls several functions at once the fragments INTERLEAVE:
//
//	{"index":0,"id":"call_A","function":{"name":"get_weather","arguments":""}}
//	{"index":0,"function":{"arguments":"{\"ci"}}
//	{"index":1,"id":"call_B","function":{"name":"get_time","arguments":""}}
//	{"index":0,"function":{"arguments":"ty\":\"Chennai\"}"}}
//
// Index says which call a fragment belongs to. Without it the fragments
// concatenate into garbage. Reassembly - one argument buffer per index - is
// what StreamState holds, and it is the only thing that turns these into
// ToolCall values.
//
// Sharing one type between the assembled and fragment forms would leave Index
// dead on every completed call and Name optional on every one of them. Two
// types make each shape mean exactly one thing.
type ToolCallDelta struct {
	// Index is the slot in the final ToolCalls slice this fragment belongs to.
	Index int `json:"index"`

	// ID and Name arrive on the first fragment for a given Index and are nil on
	// every fragment after it.
	ID   *string `json:"id,omitempty"`
	Name *string `json:"name,omitempty"`

	// Arguments is a partial JSON string. Append it to the buffer at Index;
	// it is only parseable once the stream closes.
	Arguments string `json:"arguments,omitempty"`
}

// FinishReason is why generation stopped.
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	FinishRefusal       FinishReason = "refusal"
	FinishError         FinishReason = "error"
)

// ResponseFormat requests structured output.
type ResponseFormat struct {
	Type       string          `json:"type"` // text | json_object | json_schema
	Name       string          `json:"name,omitempty"`
	Schema     json.RawMessage `json:"schema,omitempty"`
	Strict     *bool           `json:"strict,omitempty"`
}

// StreamEventType is the kind of a streaming chunk. One vocabulary for every
// request kind, so the server's SSE writer does not branch per endpoint.
type StreamEventType string

const (
	StreamDelta    StreamEventType = "delta"    // incremental content
	StreamComplete StreamEventType = "complete" // final chunk, carries Usage
	StreamError    StreamEventType = "error"    // in-band failure
)

// Metadata is attached to every response for observability and billing. It is a
// value, not a pointer: it is always present and a pointer would be an
// allocation per response for no benefit.
type Metadata struct {
	Provider     Provider      `json:"provider"`
	Model        string        `json:"model"`         // upstream name actually called
	CredentialID string        `json:"credential_id"` // which key served it
	RequestID    string        `json:"request_id,omitempty"`
	TTFB         time.Duration `json:"ttfb,omitempty"`
	Latency      time.Duration `json:"latency,omitempty"`
	ChunkIndex   int64         `json:"chunk_index,omitempty"` // streaming only

	// Dropped names request parameters the destination provider does not
	// support and which were therefore not sent. Nil in the common case; the
	// caller needs to know when the gateway silently ignored what they asked
	// for. See §15.4.
	Dropped []string `json:"dropped_params,omitempty"`
}
```

---

---

## 4a. Accounting for server-side tools

A server tool runs on the provider's machine, inside the turn. That makes it
invisible to everything the gateway normally counts — it is not a request we
proxied, and it does not show up in token counts. It is billed anyway. If we do
not capture it, we eat the cost.

Measured against the live feed (`reference/litellm/model_prices_and_context_window.json`):

| pricing key | rows carrying it |
|---|---|
| `search_context_cost_per_query` | 189 |
| `input_cost_per_query` | 44 |
| `ocr_cost_per_page` | 11 |
| `annotation_cost_per_page` | 4 |
| `code_interpreter_cost_per_session` | 3 |

`core.Pricing` already has every one of these (`pricing.go:231-240`), and
`CalculateCost` already consumes them (`pricing.go:752-758`):

```go
total += float64(u.Queries) * rate(p.InputCostPerQuery)
total += float64(u.SearchQueries) * p.searchQueryRate(u.SearchContextSize)
total += float64(u.Pages) * rate(p.OCRCostPerPage)
total += float64(u.CodeSessions) * rate(p.CodeInterpreterCostPerSession)
```

So nothing in the pricing engine needs to change. **The only missing piece is
populating those fields from the provider's response** — and that is an adapter
job, not a schema one.

### 4a.1 Two billing paths, not one

The feed splits server tools into two groups, and they bill completely
differently. This is the part that is easy to get wrong.

**Path A — priced on the model's own row.** Web search, OCR, per-query search.
189 models carry `search_context_cost_per_query` directly:

```json
"anthropic/claude-sonnet-4-5": {
  "search_context_cost_per_query": {
    "search_context_size_low": 0.01,
    "search_context_size_medium": 0.01,
    "search_context_size_high": 0.01
  }
}
```

The adapter counts the invocations into the response's own `core.Usage` and it
prices correctly with no extra work:

```go
usage.SearchQueries = a.Usage.ServerToolUse.WebSearchRequests
usage.SearchContextSize = "medium" // or the tier the request asked for
```

**Path B — priced under a DIFFERENT catalog key.** Code interpreter is not a
field on `gpt-4o`. It is its own row:

```json
"openai/container": { "code_interpreter_cost_per_session": 0.03, "mode": "chat" },
"azure/container":  { "code_interpreter_cost_per_session": 0.03, "mode": "chat" }
```

This is the trap. Setting `usage.CodeSessions = 1` on a `gpt-4o` response and
calling `CalculateCost(gpt4oPricing, usage)` bills it at **zero** — `gpt-4o`'s
row has no `code_interpreter_cost_per_session`, so `rate()` returns 0 and the
line silently vanishes. The request looks correctly priced and is short by
$0.03 every time.

### 4a.2 Pricing it is billing's job, not the schema's

The schema's only duty is to record that it happened: `Usage.CodeSessions = 1`.
Which catalog row that prices against is a billing concern, and it stays in the
billing hook:

```go
cost := CalculateCost(modelPricing, res.Usage)

// Code interpreter is priced on its own row, not the model's. Three rows in the
// feed carry code_interpreter_cost_per_session, and none of them is a chat
// model - so this second lookup is the only thing that prices it above zero.
if res.Usage.CodeSessions > 0 {
	key := CatalogKey{Provider: prov, ModelName: "container", ModelType: ModelTypeChat}
	if p, ok := catalog.Pricing(key); ok {
		cost += CalculateCost(p, Usage{CodeSessions: res.Usage.CodeSessions})
	}
}
```

An earlier draft put an `AuxUsage []AuxUsage` field on the response to carry the
second key. It was cut: the response would have been describing how to bill
itself, which is the billing layer's job, and it bought nothing that this lookup
does not already do.

### 4a.3 Server tools are a routing filter

The wire name for a server tool differs per provider, and the caller does not
pick the provider — the data plane does, after the request arrives. A virtual
key pooling 70% openai / 30% anthropic means the same request needs
`"web_search"` on one path and `"web_search_20250305"` on the other, and the
caller could not have written either one correctly.

So the neutral class lives in `core` and every provider package owns its own
names:

```go
// providers/provider.go — the contract
type ServerToolSupport struct {
	Type    core.ServerToolType // neutral class
	Wire    string              // what this provider calls it today
	Accepts []string            // older or newer raw versions it still takes
}

type ServerToolProvider interface {
	ServerTools() []ServerToolSupport
}
```

```go
// providers/anthropic — the ONLY file that knows these strings exist
func (p *Provider) ServerTools() []ServerToolSupport {
	return []ServerToolSupport{
		{core.ServerToolWebSearch, "web_search_20250305", []string{"web_search_20260209"}},
		{core.ServerToolWebFetch, "web_fetch_20250910", []string{"web_fetch_20260209", "web_fetch_20260309"}},
		{core.ServerToolCodeInterpreter, "code_execution_20260120", []string{"code_execution_20250522"}},
		{core.ServerToolComputer, "computer_20250124", []string{"computer_20251124"}},
		{core.ServerToolMCP, "mcp_toolset", nil},
	}
}
```

`register()` builds both directions once at boot — forward for building a
payload, reverse for resolving a pinned version:

```go
forward[provider][Type] = Wire   // portable path
reverse[Wire] = provider         // pinned path
reverse[v] = provider            // for each v in Accepts
```

Adding Gemini, or MCP, is one method and a few table rows. Nothing in `core`
ever learns a provider's string.

#### The rule

> **Every server tool in a request must be supported by the destination.
> Providers that cannot serve all of them are removed from the candidate set.**

That is the whole thing. It is not a translation rule, it is a *filter*, and it
runs in the loop that already filters candidates on credential validity, model
access and health:

```go
for _, cred := range bucket {
	if !cred.CheckValidity() || !cred.CheckModel(key.ModelName) {
		continue
	}
	// Server-tool support is one more predicate, not a special case.
	if !providers.SupportsAll(cred.Provider, req.ServerTools) {
		continue
	}
	candidates = append(candidates, cred)
}
```

Worked through:

```
tools: [web_search, file_search]            (both portable)
  anthropic → has web_search, no file_search → filtered out
  openai    → has both                       → survives
  route: openai
```

```
tools: [web_search version:"web_search_20250305"]
  reverse["web_search_20250305"] = anthropic
  → only anthropic survives the filter
```

```
tools: [web_search version:"web_search_20250305", file_search]
  anthropic → no file_search  → out
  openai    → does not accept "web_search_20250305" → out
  → nothing survives → 400
```

#### Why a filter and not a drop

Dropping an unsupported tool and continuing produces a **200 with a stale
answer**: the model never searched, the caller sees success, and nothing in the
response says the request did something other than what was asked. That is the
worst of the available failures, and it is the one bifrost ships — its OpenAI
serializer detects anthropic server tools structurally and silently discards
them (`openai/types.go:629`).

Making support a routing predicate removes the case entirely. A request either
gets everything it asked for, or it never leaves the gateway.

#### Errors

One new code in `errors.go`:

```go
CodeUnsupportedTool ErrorCode = "unsupported_tool"
```

All three cases fire at **admission** — before a credential is chosen, before
any upstream call, so no tokens are spent and no partial state exists. Each
message names the tool, the providers considered, and why each was rejected, so
the caller fixes it without guessing.

**1. No permitted provider serves the combination** — 400

```
server tools cannot be satisfied by any provider this key permits
  requested: web_search, file_search
  anthropic: does not support file_search
  openai:    supported, but no healthy credential
```

**2. Pins requiring different providers** — 400

```
server tools require conflicting providers
  "web_search_20250305" -> anthropic
  "file_search"         -> openai
```

**3. Unknown version string** — 400

```
unknown server tool version "web_search_20991231"
  known versions for web_search:
    openai:    web_search
    anthropic: web_search_20250305, web_search_20260209
```

Case 3 is deliberately strict rather than forward-compatible. Bifrost chose the
opposite — unknown types pass through so a tool anthropic shipped yesterday is
not blocked (`anthropic/utils.go:43`) — but that only works because it never
routes a server tool across providers. Once support decides routing, an
unrecognised string has no provider to resolve to, and guessing would put us
back in the silent-200 case.

## 5. `kind-chat.go` — chat and completion

```go
package core

// DiffractLLMChatRequest is the neutral chat request.
type DiffractLLMChatRequest struct {
	Model    string     `json:"model"` // upstream name, alias already applied
	Messages []Message  `json:"messages"`
	Params   ChatParams `json:"params,omitzero"`

	// Raw is the client's original body. Non-nil means the dialect matched the
	// destination and the provider forwards these bytes with only the model
	// rewritten; Messages and Params are then not populated.
	Raw []byte `json:"-"`

	// Extra carries request fields the schema does not model, merged into the
	// provider payload at construction.
	Extra map[string]any `json:"-"`
}

// IsRaw reports whether this request takes the passthrough path.
func (r *DiffractLLMChatRequest) IsRaw() bool { return len(r.Raw) > 0 }

// ChatParams is everything that is not the conversation itself.
//
// Pointers only where absent differs from zero: temperature 0 is a real request
// and must not be sent when the caller omitted it. Stream is a plain bool
// because false and absent are the same request.
type ChatParams struct {
	MaxCompletionTokens *int     `json:"max_completion_tokens,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	TopP                *float64 `json:"top_p,omitempty"`
	TopK                *int     `json:"top_k,omitempty"`
	FrequencyPenalty    *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64 `json:"presence_penalty,omitempty"`
	Seed                *int64   `json:"seed,omitempty"`
	Stop                []string `json:"stop,omitempty"`
	N                   *int     `json:"n,omitempty"`

	Stream        bool `json:"stream,omitempty"`
	StreamUsage   bool `json:"stream_usage,omitempty"` // stream_options.include_usage
	LogProbs      bool `json:"logprobs,omitempty"`
	TopLogProbs   *int `json:"top_logprobs,omitempty"`

	Tools             []Tool  `json:"tools,omitempty"`
	ToolChoice        *string `json:"tool_choice,omitempty"` // none|auto|required|<name>
	ParallelToolCalls *bool   `json:"parallel_tool_calls,omitempty"`

	ResponseFormat  *ResponseFormat `json:"response_format,omitempty"`
	ReasoningEffort *string         `json:"reasoning_effort,omitempty"` // minimal|low|medium|high
	ServiceTier     ServiceTier     `json:"service_tier,omitempty"`     // drives pricing tier

	User *string `json:"user,omitempty"`
}

// DiffractLLMChatResponse is the neutral chat response.
type DiffractLLMChatResponse struct {
	ID      string       `json:"id"`
	Created int64        `json:"created"`
	Choices []ChatChoice `json:"choices"`

	// Usage is core.Usage, the type CalculateCost consumes. No per-kind usage
	// struct: one that pricing cannot read is worse than none.
	Usage Usage `json:"usage,omitzero"`

	Metadata Metadata `json:"metadata,omitzero"`

	// Raw is the provider's untouched response body on the passthrough path.
	// The server writes it directly; Choices is then not populated and Usage is
	// extracted by a targeted parse for billing.
	Raw []byte `json:"-"`

	Extra map[string]any `json:"-"`
}

func (r *DiffractLLMChatResponse) IsRaw() bool { return len(r.Raw) > 0 }

type ChatChoice struct {
	Index        int          `json:"index"`
	Message      Message      `json:"message,omitzero"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	LogProbs     *LogProbs    `json:"logprobs,omitempty"`
}

type LogProbs struct {
	Content []TokenLogProb `json:"content,omitempty"`
	Refusal []TokenLogProb `json:"refusal,omitempty"`
}

type TokenLogProb struct {
	Token       string         `json:"token"`
	LogProb     float64        `json:"logprob"`
	Bytes       []byte         `json:"bytes,omitempty"`
	TopLogProbs []TokenLogProb `json:"top_logprobs,omitempty"`
}

// DiffractLLMChatStreamChunk is one streaming event.
//
// Delta is MessageDelta, not Message. A chunk carries fragments - a few
// characters of text, a partial argument string - where a Message carries
// finished content. Reusing Message here would mean every field on it is
// "maybe complete, maybe not", and the reassembly code could not tell the
// difference. Usage is populated only on the Complete event.
type DiffractLLMChatStreamChunk struct {
	Type    StreamEventType `json:"type"`
	ID      string          `json:"id,omitempty"`
	Created int64           `json:"created,omitempty"`

	Index        int          `json:"index"`
	Delta        MessageDelta `json:"delta,omitzero"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`

	Usage    Usage             `json:"usage,omitzero"` // Complete only
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"` // StreamError only

	Raw []byte `json:"-"` // passthrough: the provider's SSE line, forwarded as is
}

// MessageDelta is the incremental half of a Message.
//
// Every field is a fragment. Content is the next few characters of text, not a
// finished string; ToolCalls are ToolCallDelta values, not ToolCall values.
// Role appears on the first chunk only.
type MessageDelta struct {
	Role Role `json:"role,omitempty"` // first chunk only

	Content *string `json:"content,omitempty"` // text fragment
	Refusal *string `json:"refusal,omitempty"`

	// Thinking and Signature stream separately: the summary arrives in pieces,
	// the signature lands whole on the block's final fragment.
	Thinking  *string `json:"thinking,omitempty"`
	Signature *string `json:"signature,omitempty"`

	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

// DiffractLLMCompletionRequest is the legacy /v1/completions shape. It is a
// separate type rather than a chat request with one message: the wire contract
// takes a raw prompt with no roles, and flattening it into a message loses the
// distinction on the way back out.
type DiffractLLMCompletionRequest struct {
	Model  string     `json:"model"`
	Prompt []string   `json:"prompt"` // the API accepts a string or an array
	Params ChatParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type DiffractLLMCompletionResponse struct {
	ID      string             `json:"id"`
	Created int64              `json:"created"`
	Choices []CompletionChoice `json:"choices"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type CompletionChoice struct {
	Index        int          `json:"index"`
	Text         string       `json:"text"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	LogProbs     *LogProbs    `json:"logprobs,omitempty"`
}
```

---

## 6. `kind-responses.go` — the Responses API

Kept separate from chat rather than mapped onto it, for two structural reasons:

1. **It is an item list, not a message list.** Chat carries `[]Message`.
   Responses carries a heterogeneous `[]ResponseItem` — messages, reasoning
   blocks, function calls, function outputs, server-tool calls — and the *same*
   list type goes in as comes out. Bifrost models 26 item types
   (`responses.go:1187`); a `[]Message` can represent exactly one of them.
2. **It is stateful.** `previous_response_id` chains turns server-side, which
   chat has no equivalent for.

The catalog also files 83 models under `responses` mode with zero overlap into
`chat`, so collapsing the two would break pricing as well.

Everything below was checked field-by-field against
`reference/bifrost/core/schemas/responses.go`. Where a decision differs from
Bifrost's, the reason is stated at that field.

```go
package core

// ResponseItemType discriminates a ResponseItem.
//
// The upstream API enumerates 26 types. These seven are the ones the gateway
// must understand to route, correlate, bill and round-trip. Every other type is
// carried whole in Payload - the gateway does not need to understand an item to
// forward it, only to not lose it.
type ResponseItemType string

const (
	ItemMessage        ResponseItemType = "message"
	ItemReasoning      ResponseItemType = "reasoning"
	ItemFunctionCall   ResponseItemType = "function_call"
	ItemFunctionOutput ResponseItemType = "function_call_output"
	ItemServerToolCall ResponseItemType = "server_tool_call" // web_search_call, code_interpreter_call, mcp_call, ...
	ItemApprovalRequest ResponseItemType = "approval_request" // mcp_approval_request - BLOCKS, needs a reply
	ItemReference      ResponseItemType = "item_reference"

	// ItemUnmodelled is any of the remaining 19 types. Type identity lives in
	// PayloadType and the bytes live in Payload; nothing else is populated.
	ItemUnmodelled ResponseItemType = "unmodelled"
)

// ItemStatus is the per-item lifecycle state. Typed rather than a bare string
// so it cannot drift from ResponseStatus, which shares three of its values.
type ItemStatus string

const (
	ItemInProgress   ItemStatus = "in_progress"
	ItemCompleted    ItemStatus = "completed"
	ItemIncomplete   ItemStatus = "incomplete"
	ItemInterpreting ItemStatus = "interpreting"
	ItemFailed       ItemStatus = "failed"
)

// ResponseItem is the unit of BOTH input and output.
//
// One type for both directions, because that is how the API works: a continued
// turn sends the previous turn's output items straight back as input. Two types
// would put a conversion in the middle of the one path that has to be lossless.
type ResponseItem struct {
	Type   ResponseItemType `json:"type"`
	ID     string           `json:"id,omitempty"`
	Status ItemStatus       `json:"status,omitempty"`

	// --- ItemMessage ---

	Role    Role          `json:"role,omitempty"`
	Content []ContentPart `json:"content,omitempty"`

	// Phase marks an assistant message as intermediate commentary or the final
	// answer: "commentary" | "final_answer". It looks cosmetic and is not -
	// bifrost's note at responses.go:1221 records that dropping it on
	// gpt-5.3-codex+ history replay "causes significant performance
	// degradation", because the model re-reads its own scratch work as
	// conclusions.
	Phase *string `json:"phase,omitempty"`

	// Refusals are NOT an item type here. Upstream has both a "refusal" item
	// type and a "refusal" content-block type; the content block is the one
	// that actually carries model refusals, and ContentPart already models it
	// as ContentRefusal. Keeping both would put one concept in two homes - the
	// defect fixed for reasoning signatures in section 4 and not worth
	// reintroducing.

	// --- ItemReasoning ---

	Summary []ReasoningSummary `json:"summary,omitempty"`

	// EncryptedContent is the opaque reasoning blob. It MUST be echoed back on
	// the next turn or the model loses its reasoning chain - the same contract
	// as ContentPart.Signature on the chat path, and the same bug if dropped.
	//
	// It is only returned when the request asked for it via
	// Include: ["reasoning.encrypted_content"]. Without that, a stateless
	// multi-turn conversation degrades silently.
	//
	// Note gpt-oss models do NOT use this shape: they return reasoning_text
	// content blocks inside a message instead (responses.go:1247). Those land in
	// Content as ContentThinking parts, not here.
	EncryptedContent *string `json:"encrypted_content,omitempty"`

	// --- ItemFunctionCall and ItemFunctionOutput ---

	// CallID is the correlation id, and it is present on BOTH the call and the
	// output - bifrost's field comment at responses.go:1626 reads "Common call
	// ID for tool calls and outputs". It is NOT the same as ID: a function_call
	// item carries id="fc_..." (the item) and call_id="call_..." (the
	// correlation), and function_call_output references the latter.
	//
	// An earlier draft nested these in a *ToolCall, which put three id fields on
	// one item - ID, Call.ID and CallID - two of them meaning the same thing,
	// and dropped call_id from the call entirely. That breaks tool calling: the
	// caller cannot match its result back.
	CallID string `json:"call_id,omitempty"`

	Name string `json:"name,omitempty"` // ItemFunctionCall

	// Arguments is json.RawMessage, not string, because the wire type is not
	// consistent: function_call serialises arguments as a JSON STRING, while
	// tool_search_call serialises the same field as a JSON OBJECT. Bifrost
	// declared it *string and had to add a shadow-decode in UnmarshalJSON
	// (responses.go:1280-1295) after the mismatch "silently drops the item
	// mid-stream and hangs streaming clients". Raw bytes accept both.
	Arguments json.RawMessage `json:"arguments,omitempty"`

	Output *string `json:"output,omitempty"` // ItemFunctionOutput

	// --- ItemServerToolCall ---

	// The provider ran this itself. Informational - there is nothing to execute -
	// but recorded because it is billed; see 4a.
	ServerToolName ServerToolType `json:"server_tool_name,omitempty"`

	// --- ItemApprovalRequest ---

	// An approval request is NOT informational and does not belong with the
	// server-tool calls above: the turn is BLOCKED until the caller answers.
	// Lumping them together would leave a caller unable to tell "the provider
	// did this" from "the provider is waiting on you", and the request would
	// hang.
	//
	// The reply is sent back as an input item of the same type with Approve set.
	ApprovalID string `json:"approval_id,omitempty"`
	Approve    *bool  `json:"approve,omitempty"`
	Reason     *string `json:"reason,omitempty"`

	// --- ItemReference ---
	//
	// A reference names a stored item by its own ID field. There is no separate
	// ref id on the wire - {"type":"item_reference","id":"msg_..."} - so ID
	// above carries it and nothing else is set.

	// --- ItemUnmodelled, and only ItemUnmodelled ---

	// PayloadType is the upstream type string for an item the schema does not
	// model: "local_shell_call", "compaction", "additional_tools", ...
	PayloadType string `json:"payload_type,omitempty"`

	// Payload is that item's original bytes, forwarded unchanged.
	//
	// It is populated ONLY when Type is ItemUnmodelled, and is never a duplicate
	// of a field set above. An earlier draft had it hold both "the parts of a
	// modelled item we do not read" and "any unmodelled item", which left two
	// authorities for the same item and no rule for which wins on the way out -
	// the exact "only one of these can be non-nil at a time" landmine called out
	// on bifrost's ChatMessage in section 2.1.
	//
	// Bifrost reached the same conclusion independently: its rawPreserved field
	// is gated by isRawPreservedItem(), which names exactly three types
	// (responses.go:1268) and nothing else.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ReasoningSummary is one summary block. A struct rather than a bare string
// because the block carries its own type discriminator upstream
// (ResponsesReasoningSummary at responses.go:2111), and a []string would drop it.
type ReasoningSummary struct {
	Type string `json:"type"` // "summary_text"
	Text string `json:"text"`
}

type DiffractLLMResponsesRequest struct {
	Model string `json:"model"`

	// Input is items, not messages. See ResponseItem.
	Input []ResponseItem `json:"input"`

	// Instructions is the system prompt as its own field in this API rather
	// than an item with a system role.
	Instructions *string `json:"instructions,omitempty"`

	// PreviousResponseID continues a server-side conversation. This is the
	// field that makes responses a distinct kind rather than a chat variant.
	PreviousResponseID *string `json:"previous_response_id,omitempty"`
	Store              *bool   `json:"store,omitempty"`

	// Include opts into data the response omits by default. The value that
	// matters operationally is IncludeReasoningEncrypted: without it a stateless
	// caller cannot continue a reasoning conversation, and the degradation is
	// silent.
	Include []string `json:"include,omitempty"`

	Params ResponsesParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// Include values, as constants rather than loose strings at every call site.
const (
	IncludeReasoningEncrypted = "reasoning.encrypted_content"
	IncludeWebSearchSources   = "web_search_call.action.sources"
	IncludeCodeOutputs        = "code_interpreter_call.outputs"
	IncludeFileSearchResults  = "file_search_call.results"
	IncludeOutputLogProbs     = "message.output_text.logprobs"
)

type ResponsesParams struct {
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
	MaxToolCalls    *int     `json:"max_tool_calls,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
	TopLogProbs     *int     `json:"top_logprobs,omitempty"`

	Stream bool `json:"stream,omitempty"`
	// Background runs the response asynchronously; the caller polls or
	// reconnects by id rather than holding the connection open.
	Background bool `json:"background,omitempty"`

	Tools             []Tool  `json:"tools,omitempty"`
	ToolChoice        *string `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool   `json:"parallel_tool_calls,omitempty"`

	Reasoning *ReasoningConfig `json:"reasoning,omitempty"`
	Text      *TextConfig      `json:"text,omitempty"`

	ServiceTier    ServiceTier `json:"service_tier,omitempty"`
	Truncation     *string     `json:"truncation,omitempty"` // auto | disabled
	PromptCacheKey *string     `json:"prompt_cache_key,omitempty"`
	User           *string     `json:"user,omitempty"`
}

// ReasoningConfig is a struct, not loose fields, because the wire shape nests
// and because the settings are only meaningful together.
type ReasoningConfig struct {
	Effort  *string `json:"effort,omitempty"`  // none | minimal | low | medium | high
	Summary *string `json:"summary,omitempty"` // auto | concise | detailed

	// MaxTokens is the thinking budget. Optional on OpenAI and REQUIRED by
	// Anthropic when reasoning is enabled - bifrost's field comment at
	// responses.go:1015 says so outright, and an Anthropic reasoning request
	// without it is a 400. The adapter fills it from the catalog when the caller
	// omits it, the same way max_tokens is filled in section 16 T6.
	MaxTokens *int `json:"max_tokens,omitempty"`
}

// TextConfig is where the Responses API puts structured output, rather than
// chat's top-level response_format.
type TextConfig struct {
	Format    *ResponseFormat `json:"format,omitempty"`
	Verbosity *string         `json:"verbosity,omitempty"` // low | medium | high
}

// ResponseStatus is the lifecycle state of the whole response.
type ResponseStatus string

const (
	ResponseQueued     ResponseStatus = "queued"
	ResponseInProgress ResponseStatus = "in_progress"
	ResponseCompleted  ResponseStatus = "completed"
	ResponseIncomplete ResponseStatus = "incomplete"
	ResponseFailed     ResponseStatus = "failed"
)

type DiffractLLMResponsesResponse struct {
	ID        string         `json:"id"`
	CreatedAt int64          `json:"created_at"`
	Status    ResponseStatus `json:"status"`

	// IncompleteReason is why generation stopped. Status alone says a response
	// is incomplete but not why, so "hit the token cap" and "content filtered"
	// would be indistinguishable - the same information chat carries in
	// FinishReason, which is why it reuses that type.
	//
	// From incomplete_details.reason: "max_output_tokens" -> FinishLength,
	// "content_filter" -> FinishContentFilter.
	IncompleteReason FinishReason `json:"incomplete_reason,omitempty"`

	Output []ResponseItem `json:"output"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// ResponsesDeltaTarget says WHAT a streamed fragment is a fragment OF.
//
// This field exists because the upstream stream has 55 event types
// (responses.go, ResponsesStreamResponseType) and five of them carry plain text
// that lands in completely different places:
//
//	response.output_text.delta                -> the answer
//	response.reasoning_summary_text.delta     -> the reasoning summary
//	response.function_call_arguments.delta    -> a tool call's arguments
//	response.mcp_call_arguments.delta         -> an MCP call's arguments
//	response.code_interpreter_call_code.delta -> generated code
//
// Collapsing them to one Delta string makes reasoning text indistinguishable
// from answer text. It is the same defect as sharing one type between ToolCall
// and ToolCallDelta, and it is fixed the same way: name the target.
type ResponsesDeltaTarget string

const (
	DeltaOutputText       ResponsesDeltaTarget = "output_text"
	DeltaReasoningSummary ResponsesDeltaTarget = "reasoning_summary"
	DeltaFunctionArgs     ResponsesDeltaTarget = "function_arguments"
	DeltaRefusal          ResponsesDeltaTarget = "refusal"
	DeltaCode             ResponsesDeltaTarget = "code"
)

type DiffractLLMResponsesStreamChunk struct {
	Type     StreamEventType `json:"type"`
	ID       string          `json:"id,omitempty"`
	Sequence int64           `json:"sequence,omitempty"`

	// ItemIndex and ItemID identify which output item this event concerns. The
	// API interleaves items, so a flat delta is not sufficient.
	ItemIndex int    `json:"item_index"`
	ItemID    string `json:"item_id,omitempty"`

	// Target and Delta together are one fragment.
	Target ResponsesDeltaTarget `json:"target,omitempty"`
	Delta  *string              `json:"delta,omitempty"`

	// Item is populated on output_item.added and output_item.done, where a whole
	// item boundary is announced rather than a fragment.
	Item *ResponseItem `json:"item,omitempty"`

	// Status and IncompleteReason arrive on the terminal event.
	Status           ResponseStatus `json:"status,omitempty"`
	IncompleteReason FinishReason   `json:"incomplete_reason,omitempty"`

	Usage    Usage             `json:"usage,omitzero"`
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"`

	Raw []byte `json:"-"`
}
```

### What the 55 upstream events collapse to

Most are progress notifications with no content — `in_progress`, `searching`,
`interpreting`, `queued`, `ping`. They emit nothing, for the same reason
`message_start` emits nothing on the Anthropic chat path: a `StreamDelta` carries
content, and empty frames confuse a client that is not expecting them.

| upstream | IR chunk |
|---|---|
| `response.created`, `.in_progress`, `.queued`, `.ping` | none |
| `response.output_item.added` / `.done` | `StreamDelta` with `Item` set, no `Delta` |
| `response.output_text.delta` | `StreamDelta`, `Target: DeltaOutputText` |
| `response.reasoning_summary_text.delta` | `StreamDelta`, `Target: DeltaReasoningSummary` |
| `response.function_call_arguments.delta`, `.mcp_call_arguments.delta`, `.custom_tool_call_input.delta` | `StreamDelta`, `Target: DeltaFunctionArgs` |
| `response.code_interpreter_call_code.delta` | `StreamDelta`, `Target: DeltaCode` |
| `response.refusal.delta` | `StreamDelta`, `Target: DeltaRefusal` |
| `*.in_progress`, `*.searching`, `*.interpreting`, `*_part.added/.done`, `*.done` | none |
| `response.web_search_call.completed`, `.code_interpreter_call.completed`, `.mcp_call.completed` | `StreamDelta` with `Item` — where server-tool usage is counted |
| `response.completed` / `.incomplete` / `.failed` | `StreamComplete` with `Usage`, `Status`, `IncompleteReason` |
| `error` | `StreamError` |

---

## 7. `kind-embedding.go`

Embeddings vary more between providers than any other kind, so this schema keeps
the caller's exact shape rather than normalising it. An adapter that can see
what was actually sent picks its endpoint directly; one that has to re-derive
intent from a slice length is guessing.

```go
package core

type DiffractLLMEmbeddingRequest struct {
	Model string          `json:"model"`
	Input EmbeddingInput  `json:"input"`
	Params EmbeddingParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// EmbeddingInput preserves the four shapes the wire accepts. Exactly one is set.
//
// The wire field is a bare JSON value with no discriminator - it may be a
// string, an array of strings, an array of token ids, or an array of those - so
// the inbound adapter sniffs the shape and records which one it found. Keeping
// all four means the outbound adapter never has to reconstruct what the caller
// meant:
//
//	Text       -> bedrock titan (inputText, single string only)
//	Texts      -> cohere (texts, always an array), openai batch
//	Embedding  -> pre-tokenized single input, so a caller that already
//	              tokenized does not pay for it twice
//	Embeddings -> pre-tokenized batch
//
// Gemini reads it to choose embedContent over batchEmbedContents; titan reads
// it to know whether it must fan out into N calls.
type EmbeddingInput struct {
	Text       *string  `json:"text,omitempty"`
	Texts      []string `json:"texts,omitempty"`
	Embedding  []int32  `json:"embedding,omitempty"`
	Embeddings [][]int32 `json:"embeddings,omitempty"`
}

// Shape reports which variant is set, so callers switch instead of running four
// nil checks in order.
func (e *EmbeddingInput) Shape() EmbeddingInputShape {
	switch {
	case e.Text != nil:
		return InputText
	case e.Texts != nil:
		return InputTexts
	case e.Embedding != nil:
		return InputTokens
	case e.Embeddings != nil:
		return InputTokenBatch
	}
	return InputEmpty
}

type EmbeddingInputShape uint8

const (
	InputEmpty EmbeddingInputShape = iota
	InputText
	InputTexts
	InputTokens
	InputTokenBatch
)

// Count returns how many vectors this input will produce, which is what an
// adapter needs to decide between a single-item endpoint and a batch one.
func (e *EmbeddingInput) Count() int {
	switch e.Shape() {
	case InputText, InputTokens:
		return 1
	case InputTexts:
		return len(e.Texts)
	case InputTokenBatch:
		return len(e.Embeddings)
	}
	return 0
}

// Validate enforces the one-of invariant.
//
// This runs at ADMISSION, not in MarshalJSON. Bifrost enforces the same rule
// inside EmbeddingInput.MarshalJSON (embedding.go:47), which means a malformed
// input is only caught at serialisation - after routing, after a credential has
// been picked, mid-request. Checking it on the way in fails the request before
// anything has been spent.
func (e *EmbeddingInput) Validate() error {
	if e.Shape() == InputEmpty {
		return errors.New("embedding input is empty")
	}
	set := 0
	if e.Text != nil {
		set++
	}
	if e.Texts != nil {
		set++
	}
	if e.Embedding != nil {
		set++
	}
	if e.Embeddings != nil {
		set++
	}
	if set > 1 {
		return errors.New("embedding input must set exactly one of: text, texts, embedding, embeddings")
	}
	return nil
}

type EmbeddingParams struct {
	// Dimensions truncates the output vector where the model supports it.
	Dimensions     *int    `json:"dimensions,omitempty"`
	EncodingFormat *string `json:"encoding_format,omitempty"` // float | base64 | int8 | uint8 | binary | ubinary

	// InputType is required by cohere (search_document | search_query |
	// classification | clustering) and is gemini's taskType. OpenAI has no
	// equivalent and ignores it. A cohere request without it does not error -
	// it returns worse vectors, silently - which is why it is modelled rather
	// than left in Extra.
	InputType *string `json:"input_type,omitempty"`

	Truncate *string `json:"truncate,omitempty"` // NONE | START | END
	User     *string `json:"user,omitempty"`
}

type DiffractLLMEmbeddingResponse struct {
	Data []Embedding `json:"data"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// EmbeddingFormat discriminates the vector representation.
//
// The discriminator exists from day one even though openai only ever produces
// Float and Base64. If it were added later, every consumer that reads an
// embedding would have to start branching - the ContentStr disease, which cost
// bifrost 1,243 call sites. With the switch present up front, adding cohere or
// bedrock fills a field and changes no existing code.
type EmbeddingFormat string

const (
	EmbeddingFloat   EmbeddingFormat = "float"
	EmbeddingFloat2D EmbeddingFormat = "float_2d"
	EmbeddingBase64  EmbeddingFormat = "base64"
	EmbeddingInt8    EmbeddingFormat = "int8"  // int8, binary
	EmbeddingUint8   EmbeddingFormat = "uint8" // uint8, ubinary
)

// Embedding holds one vector. Exactly one payload field is set, named by Format.
//
// The five variants mirror bifrost's EmbeddingStruct (embedding.go:133) because
// they are genuinely different data, not encodings of one thing:
//
//   - int8 is not a compressed float - it is a different numeric type, four
//     times smaller, and the entire reason cohere and voyage offer it.
//     Representing it as float would inflate it back to the size the caller
//     asked to avoid.
//   - Float2D covers providers that return nested arrays. Nothing in scope
//     emits it, but a decode that cannot represent the shape fails the whole
//     response rather than one field.
//   - Uint32 is []int32 rather than []uint8, matching bifrost's
//     EmbeddingInt32Array - bedrock populates that one
//     (bedrock/embedding.go:266), and a narrower type would truncate.
//
// Float is float64, not float32. float32 is lossless for openai, whose vectors
// are float32 underneath, but it imposes a narrowing on every other provider
// and on any future one, and a gateway that quietly changes the numbers it
// forwards is not transparent. Bifrost states the same reason on the same
// struct: "Embedding responses preserve provider precision in normalized API
// output."
//
// Base64 stays as raw bytes so a passthrough never decodes and re-encodes.
//
// The wire shape is a bare JSON value at the "embedding" key, not an object -
// the adapter emits whichever field Format names. Bifrost reaches the same
// result with a nested EmbeddingStruct plus a custom MarshalJSON; a flat struct
// with a discriminator needs no marshaller. "object": "embedding" is not
// carried, because it is a constant the adapter writes.
type Embedding struct {
	Index  int             `json:"index"`
	Format EmbeddingFormat `json:"format"`

	Float   []float64   `json:"float,omitempty"`
	Float2D [][]float64 `json:"float_2d,omitempty"`
	Base64  []byte      `json:"base64,omitempty"`
	Int8    []int8      `json:"int8,omitempty"`
	Uint32  []int32     `json:"uint32,omitempty"`
}

// Dimensions returns the vector length regardless of representation, so code
// checking that the model honoured the requested Dimensions does not switch.
func (e *Embedding) Dimensions() int {
	switch e.Format {
	case EmbeddingFloat:
		return len(e.Float)
	case EmbeddingFloat2D:
		if len(e.Float2D) == 0 {
			return 0
		}
		return len(e.Float2D[0])
	case EmbeddingInt8:
		return len(e.Int8)
	case EmbeddingUint8:
		return len(e.Uint32)
	}
	return 0 // Base64 length depends on the element width the provider used
}
```

Two deliberate differences from Bifrost's version, both about *where* things
happen rather than what is modelled:

1. **The one-of check runs at admission, not in `MarshalJSON`.** Same invariant,
   caught before a credential is chosen instead of mid-request.
2. **`Shape()` and `Count()` are methods, not four nil checks at every call
   site.** The adapter asks the question it actually has — "one or many?" — and
   the four-field representation stays an implementation detail of the answer.

---

## 8. `kind-image.go` — generation and edit

```go
package core

type DiffractLLMImageRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`

	// Edit only. Image is the source; Mask marks the region to replace.
	Image []ImageInput `json:"image,omitempty"`
	Mask  *ImageInput  `json:"mask,omitempty"`

	Params ImageParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// ImageParams fields double as pricing selectors: Size and Quality are exactly
// what core.SelectorSet keys on, so they must survive translation intact or the
// request prices against the base row instead of its variant.
type ImageParams struct {
	N       *int    `json:"n,omitempty"`
	Size    *string `json:"size,omitempty"`    // 1024x1024, 1536x1024, ...
	Quality *string `json:"quality,omitempty"` // low|medium|high|hd|standard|auto
	Style   *string `json:"style,omitempty"`   // vivid | natural
	Steps   *int    `json:"steps,omitempty"`   // stability, bedrock

	ResponseFormat  *string `json:"response_format,omitempty"`  // url | b64_json
	Background      *string `json:"background,omitempty"`       // transparent|opaque|auto
	OutputFormat    *string `json:"output_format,omitempty"`    // png|jpeg|webp
	OutputCompress  *int    `json:"output_compression,omitempty"`
	Moderation      *string `json:"moderation,omitempty"`
	User            *string `json:"user,omitempty"`
	Stream          bool    `json:"stream,omitempty"`
}

// Selectors builds the pricing selector set from the request parameters. This
// is the bridge between a request and BasePricingSnapshot.find - without it an
// image request cannot be priced against its size or quality variant.
func (p *ImageParams) Selectors() map[string]string {
	values := make(map[string]string, 3)
	if p.Size != nil {
		values["size"] = *p.Size
	}
	if p.Quality != nil {
		values["quality"] = *p.Quality
	}
	if p.Steps != nil {
		values["steps"] = strconv.Itoa(*p.Steps)
	}
	return values
}

// ImageInput is an image supplied by the caller. Exactly one field is set.
type ImageInput struct {
	URL      *string `json:"url,omitempty"`
	Base64   *string `json:"b64,omitempty"`
	FileID   *string `json:"file_id,omitempty"`
	MimeType string  `json:"mime_type,omitempty"`
	Filename string  `json:"filename,omitempty"`
}

type DiffractLLMImageResponse struct {
	Created int64        `json:"created"`
	Data    []ImageData  `json:"data"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type ImageData struct {
	URL           *string `json:"url,omitempty"`
	Base64        *string `json:"b64_json,omitempty"`
	RevisedPrompt *string `json:"revised_prompt,omitempty"`
}
```

---

## 9. `kind-speech.go` — rewrite

The current file loses billing data and duplicates routing identity. Replacement:

```go
package core

type DiffractLLMSpeechRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
	Voice string `json:"voice"`

	Params SpeechParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type SpeechParams struct {
	Instructions   *string  `json:"instructions,omitempty"`
	ResponseFormat *string  `json:"response_format,omitempty"` // mp3|opus|aac|flac|wav|pcm
	Speed          *float32 `json:"speed,omitempty"`
	Stream         bool     `json:"stream,omitempty"`
	StreamFormat   *string  `json:"stream_format,omitempty"` // sse | audio
}

// DiffractLLMSpeechResponse carries raw audio; Audio is always the provider's
// bytes, so there is no passthrough distinction to draw here.
type DiffractLLMSpeechResponse struct {
	Audio       []byte `json:"-"`
	ContentType string `json:"content_type"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`
}

type DiffractLLMSpeechStreamChunk struct {
	Type  StreamEventType `json:"type"`
	Audio []byte          `json:"-"`

	Usage    Usage             `json:"usage,omitzero"` // Complete only
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"`
}
```

Changes from the current file: `Provider` removed (routing identity lives on
`rctx.Modelkey`), `DiffractLLMSpeechUsage` deleted in favour of `core.Usage`,
`StreamingRequest` moved into `SpeechParams.Stream` with a json tag, and
`ChunkIndex`/`Latency` folded into the shared `Metadata`.

Billing note: TTS prices on `InputCharacters`, so translation must set
`Usage.InputCharacters = len(req.Input)` — the provider does not return it.

---

## 10. `kind-transcription.go`

```go
package core

type DiffractLLMTranscriptionRequest struct {
	Model string `json:"model"`

	// File is the audio. Bytes for an upload, FileID for a stored reference.
	File     []byte `json:"-"`
	Filename string `json:"filename,omitempty"`
	FileID   *string `json:"file_id,omitempty"`

	Params TranscriptionParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type TranscriptionParams struct {
	Language       *string  `json:"language,omitempty"`
	Prompt         *string  `json:"prompt,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	ResponseFormat *string  `json:"response_format,omitempty"` // json|text|srt|vtt|verbose_json
	Granularities  []string `json:"timestamp_granularities,omitempty"`
	Stream         bool     `json:"stream,omitempty"`
	Translate      bool     `json:"translate,omitempty"` // /translations rather than /transcriptions
}

type DiffractLLMTranscriptionResponse struct {
	Text     string             `json:"text"`
	Language *string            `json:"language,omitempty"`
	Duration *float64           `json:"duration,omitempty"` // seconds; also the billing unit
	Segments []TranscriptSegment `json:"segments,omitempty"`
	Words    []TranscriptWord    `json:"words,omitempty"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type TranscriptSegment struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type TranscriptWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
```

Billing note: transcription prices on `InputAudioSeconds`. When the provider
returns `Duration`, translation copies it into `Usage.InputAudioSeconds`; when it
does not, the request cannot be priced by duration and falls back to whatever
token fields the response carries.

---

## 11. `kind-moderation.go`

```go
package core

type DiffractLLMModerationRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type DiffractLLMModerationResponse struct {
	ID      string             `json:"id"`
	Results []ModerationResult `json:"results"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// ModerationResult keeps categories as maps rather than a struct per category:
// providers add categories regularly, and a typed struct would silently drop
// every new one.
type ModerationResult struct {
	Flagged        bool               `json:"flagged"`
	Categories     map[string]bool    `json:"categories"`
	CategoryScores map[string]float64 `json:"category_scores"`
}
```

---

## 12. `kind-models.go`

```go
package core

// DiffractLLMModelsRequest has no body; it exists so the descriptor registry
// has a request type for GET /v1/models like every other kind.
type DiffractLLMModelsRequest struct {
	Provider Provider `json:"provider,omitempty"` // empty means every permitted provider
}

// DiffractLLMModelsResponse is served from the catalog and the virtual key's
// allow-lists, not by calling the provider - the gateway already knows which
// models a key may use, and asking upstream would return models the caller
// cannot reach.
type DiffractLLMModelsResponse struct {
	Models   []ModelInfo `json:"data"`
	Metadata Metadata    `json:"metadata,omitzero"`
}

type ModelInfo struct {
	ID         string     `json:"id"` // provider/model as the client addresses it
	Provider   Provider   `json:"provider"`
	ModelType  ModelType  `json:"model_type"`
	OwnedBy    string     `json:"owned_by,omitempty"`
	Created    int64      `json:"created,omitempty"`
	Capability Capability `json:"-"`
	Limits     ModelLimits `json:"limits,omitzero"`
}
```

---

## 13. What this schema deliberately does not have

**No `Provider` field on request payloads.** Routing identity is `rctx.Modelkey`,
set by the admission hook and possibly changed by the data plane. A second copy
goes stale.

**No per-kind usage type.** One `core.Usage`, because that is what
`CalculateCost` consumes.

**No interface-typed content.** `ContentPart` is a flat struct with a
discriminator. An interface costs an allocation and a type assertion per part,
and a long conversation carries hundreds.

**No `Fallbacks` field.** Bifrost carries fallback targets inside the request;
here fallback is the data plane's job, driven by virtual-key weights and the
metrics engine, and belongs nowhere near the payload.

**No streaming types for embeddings, moderation or models.** Those endpoints do
not stream.

**No realtime, video, rerank, search, OCR, vector store or 3D.** The catalog has
models for all of them, but no provider in scope serves them. `RequestKind` and
`ModelType` already have the constants; the payload types get written when a
provider needs them.

---

## 14. Verification

The schema is data, so the tests are about the guarantees it claims.

`internal/core/kind_test.go`:

- **Round trip.** Each request and response type marshals and unmarshals to an
  equal value, including the `omitzero` struct fields — a struct tagged
  `omitempty` would emit `{}` and this is what catches it.
- **`Raw` never serialises.** Marshal a request with `Raw` set and assert the
  output contains no base64 blob.
- **Pointer semantics.** `Temperature: Ptr(0.0)` survives a round trip as a
  present zero, not an absent field; `Temperature: nil` is omitted entirely.
- **`Message.Text()`** returns the text for a single-part message, the
  concatenation for a multi-part one, and empty for a tool-call-only assistant
  turn. Also assert it skips image, audio and thinking parts rather than
  stringifying them.
- **`ToolCallDelta` reassembly.** Feed the interleaved four-fragment sequence
  from the doc comment through `StreamState` and assert it yields exactly two
  `ToolCall` values with `{"city":"Chennai"}` and `{"tz":"IST"}`. Concatenating
  without honouring `Index` produces invalid JSON, and this is the test that
  catches it.
- **`RequestKind.ModelType()`** maps every declared kind, and an unknown kind
  yields `ModelTypeUnknown` rather than a wrong type.
- **`ImageParams.Selectors()`** produces exactly the keys
  `BasePricingSnapshot.find` expects — `size`, `quality`, `steps` — and omits
  absent ones. Feed it `size=1024x1024, quality=low` and assert the canonical key
  matches what the catalog stored for `low/1024-x-1024/gpt-image-1`. This is the
  guard that keeps image requests billing against their variant rather than the
  base row.
- **`Usage` reaches pricing.** Build each response type with a populated `Usage`
  and assert `CalculateCost` returns a non-zero cost — the regression guard for
  reintroducing a per-kind usage struct.
- **Server-tool billing, path A.** `Usage{SearchQueries: 3, SearchContextSize:
  "medium"}` against a pricing row carrying `search_context_cost_per_query`
  yields exactly `3 × 0.01`. 189 feed rows depend on this line.
- **Server-tool billing, path B — the one that loses money.** Assert that
  `Usage{CodeSessions: 1}` priced against **`gpt-4o`'s** row returns **0**, and
  that the billing hook's second lookup against `openai/container` returns
  `0.03`. The first half is the bug, the second is the fix.
- **`Tool` is flat.** Marshal a `ToolKindFunction` tool and assert the JSON has
  `name` at the top level and no `function` object — the guard against OpenAI's
  envelope creeping back into the IR.
- **Server-tool routing filter.** Given credentials for both providers and
  `tools: [web_search, file_search]`, assert the candidate set is `[openai]` —
  anthropic is filtered out for lacking `file_search`. Then assert
  `tools: [{web_search, version: "web_search_20250305"}]` yields `[anthropic]`.
- **The filter fails closed.** `tools: [web_search_20250305, file_search]`
  leaves no candidate and returns `CodeUnsupportedTool` at admission — assert no
  upstream call was made. This is the guard against the silent-200: if anyone
  changes the filter to a drop, a request that cannot be served will start
  returning 200 and this test is what catches it.
- **Unknown version.** `version: "web_search_20991231"` returns
  `CodeUnsupportedTool` listing the known versions per provider, not a nil-map
  panic and not a pass-through.
- **The forward and reverse tables agree.** For every `ServerToolSupport` a
  provider declares, assert `reverse[Wire]` and `reverse[v]` for each `v` in
  `Accepts` resolve back to that provider, and that no two providers claim the
  same raw string. A collision would make pinned routing pick arbitrarily.
- **Responses round-trips its own output.** Take a `DiffractLLMResponsesResponse`
  whose `Output` holds a message, a reasoning item with `EncryptedContent`, and
  a `function_call`; feed that slice straight back as the next request's `Input`
  and assert every field survives byte-identical. This is the whole reason
  `ResponseItem` is one type for both directions, and the test that catches
  anyone splitting it.
- **`EncryptedContent` is never dropped.** Marshal a reasoning item and assert
  the blob is present; then assert that a request without
  `Include: ["reasoning.encrypted_content"]` is flagged, since the API returns
  nothing to round-trip and the multi-turn degradation is otherwise silent.
- **`IncompleteReason` is populated.** `incomplete_details.reason =
  "max_output_tokens"` maps to `FinishLength`, `"content_filter"` to
  `FinishContentFilter`. Assert a `ResponseIncomplete` status never arrives with
  an empty reason — that combination is the "why did it stop" blind spot.
- **Delta targets stay distinct.** Feed a stream that interleaves
  `response.output_text.delta` and `response.reasoning_summary_text.delta` for
  the same item, and assert the answer text and the reasoning summary reassemble
  into two separate strings. Collapsing `Target` would silently merge them, and
  reasoning text would appear in the user-visible answer.
- **Anthropic reasoning gets a budget.** A `ResponsesRequest` with
  `Reasoning.Effort` set and `MaxTokens` nil, routed to Anthropic, must have
  `MaxTokens` filled from the catalog before the payload is built — Anthropic
  400s without it.

---

# Part II — Translation

Everything above is data. This part is how a request moves through it, and what
you have to write to add a provider.

## 15. Extension model

### 15.1 Two independent axes

```
   INBOUND DIALECTS                              OUTBOUND PROVIDERS
   (what the client speaks)                      (what we speak upstream)

   openai   ──┐                              ┌── openai
   anthropic ─┤                              ├── azure
   gemini   ──┼──►  core.DiffractLLM*  ──────┼── anthropic
   cohere   ──┤     (the IR, Part I)         ├── gemini
   ...      ──┘                              └── bedrock
```

Adding a dialect is `N+1`. Adding a provider is `M+1`. Neither touches the
other, and neither touches the IR. That is the whole point of having an IR at
all — the alternative is `N×M`, and it is why every gateway that skipped this
step ends up with `if provider == "x"` scattered through its handlers.

**The IR changes only when a concept is genuinely new.** Anthropic's prompt
caching added `CacheControl` to `ContentPart`, because caching is a real concept
that several providers now have. Anthropic's `anthropic-version` header did not,
because that is transport, not meaning.

### 15.2 What you write to add Anthropic

Four files, nothing else:

```
internal/providers/anthropic/
  provider.go     Provider impl: endpoint, auth headers, http call
  chat.go         IR → messages payload, messages response → IR
  stream.go       anthropic SSE events → core.DiffractLLMChatStreamChunk
  errors.go       anthropic error body → core.DiffractLLMError
```

Plus **one line** in the provider registry, and **one row** per inbound route in
the descriptor table. No change to `internal/core`, `internal/dataplane`,
`internal/providerplane`, `internal/governance`, `internal/modelcatalog`, or the
server. That is the test of whether the seam is in the right place.

### 15.3 Capability interfaces, not one fat interface

```go
// internal/providers/provider.go
package providers

// Provider is what every provider implements: identity and transport.
type Provider interface {
	Name() core.Provider

	// Endpoint builds the upstream URL for a request kind. Azure needs the
	// deployment name and api-version, anthropic is a fixed path, bedrock
	// encodes the model in the path - so the provider owns URL construction.
	Endpoint(kind core.RequestKind, model string, cred *core.Credential, up *core.Upstream) (string, error)

	// Authorize stamps credentials onto the outbound request. It runs AFTER
	// the body is set, because SigV4 signs the body.
	Authorize(req *http.Request, cred *core.Credential) error
}

// ChatProvider is implemented only by providers that serve chat.
type ChatProvider interface {
	BuildChat(*core.DiffractLLMChatRequest, *Target) ([]byte, *core.DiffractLLMError)
	ParseChat([]byte, *Target) (*core.DiffractLLMChatResponse, *core.DiffractLLMError)
}

type ChatStreamProvider interface {
	ParseChatEvent(event []byte, st *StreamState) (*core.DiffractLLMChatStreamChunk, bool, *core.DiffractLLMError)
}

type EmbeddingProvider interface{ ... }
type ImageProvider interface{ ... }
type SpeechProvider interface{ ... }
```

One interface per kind rather than a twenty-method `Provider`. Anthropic serves
chat and nothing else; with a fat interface it would stub eighteen methods that
return "unsupported". Here it implements two interfaces and the absence *is* the
answer.

The type assertions happen **once at boot**, not per request:

```go
type Entry struct {
	Provider   Provider
	Chat       ChatProvider
	ChatStream ChatStreamProvider
	Embedding  EmbeddingProvider
	Image      ImageProvider
	Speech     SpeechProvider
}

func register(p Provider) {
	e := &Entry{Provider: p}
	e.Chat, _ = p.(ChatProvider)
	e.ChatStream, _ = p.(ChatStreamProvider)
	e.Embedding, _ = p.(EmbeddingProvider)
	// ...
	registry[p.Name()] = e
}
```

Request time is one map lookup and one nil check. `if e.Chat == nil` is the
"provider does not serve this kind" branch, and it is a 405, not a panic.

Adding Anthropic is then literally:

```go
register(anthropic.New())   // the one line
```

### 15.4 Lossiness policy

Every translation is lossy in one direction or the other. Three cases, three
rules:

| Case | Rule |
|---|---|
| Concept exists on both sides, different shape | Translate. The cost is CPU, not information. |
| Concept exists only on the source | **Drop, and record it in `Metadata.Dropped`.** Never ignore silently. |
| Concept required by the destination, absent from the IR | Fill from the catalog, never from a hardcoded constant. |

The third is the interesting one, and Anthropic forces it immediately:
`max_tokens` is **required** on `/v1/messages`, while OpenAI's
`max_completion_tokens` is optional. A hardcoded `4096` truncates a model that
can emit 64k. The catalog already knows — `core.ModelLimits.MaxOutputTokens` —
so that is where the default comes from.

---

## 16. Worked example: OpenAI SDK in, Anthropic out

Anthropic is the right example precisely because it agrees with OpenAI on almost
nothing: no system role, no `tool` role, required `max_tokens`, tool arguments as
an object rather than a string, a different stop vocabulary, different auth
headers, a different streaming event model, and a usage object whose input count
means something different.

### 16.0 Setup

The gateway has one credential registered for `anthropic`. The virtual key routes
model `claude-sonnet-4-5` to it. The client is a stock **OpenAI SDK** pointed at
the gateway.

### Step 1 — Wire in

```http
POST /openai/v1/chat/completions HTTP/1.1
Authorization: Bearer dfl_sk_live_9a3f...
Content-Type: application/json
```

```json
{
  "model": "claude-sonnet-4-5",
  "messages": [
    { "role": "system", "content": "You are terse." },
    { "role": "user", "content": "Weather in Chennai?" },
    { "role": "assistant", "content": null,
      "tool_calls": [{ "id": "call_1", "type": "function",
        "function": { "name": "get_weather", "arguments": "{\"city\":\"Chennai\"}" } }] },
    { "role": "tool", "tool_call_id": "call_1", "content": "{\"temp_c\":34}" }
  ],
  "tools": [{ "type": "function", "function": {
      "name": "get_weather", "description": "Current weather",
      "parameters": { "type": "object", "properties": { "city": { "type": "string" } } } }}],
  "temperature": 0.7,
  "frequency_penalty": 0.5,
  "seed": 42,
  "stream": true
}
```

### Step 2 — Route, and kind detection

The path `/:sdk/v1/chat/completions` is matched by the descriptor registry.
Detection is a **map lookup, not body sniffing** — the URL already says what this
is:

```go
desc := GetDescriptor("openai", "/v1/chat/completions")
// desc.SDK         = core.OpenAI
// desc.RequestKind = core.ChatRequest
// desc.NewRequest  = func() any { return &openai.ChatRequest{} }
// desc.ToDiffract  = openai.ChatToDiffract
```

The body is read **once** into `rctx.BodyBytes` and never re-read. `rctx` comes
from `DiffractLLMContextPool`, so this costs no allocation on a warm gateway.

```go
rctx.SDKProvider = core.OpenAI
rctx.RequestKind = core.ChatRequest
```

### Step 3 — Parse, and identify the model

The SDK-shaped struct is unmarshalled; only the model string is needed for
routing:

```go
provider, model := core.ParseModelString("claude-sonnet-4-5", core.OpenAI)
```

There is no known provider prefix on the string, so the **routing key stays
type-erased on the provider axis**:

```go
rctx.Modelkey = core.CatalogKey{ModelName: "claude-sonnet-4-5", ModelType: core.ModelTypeChat}
```

`Provider` is deliberately empty. A bare model name is a *weighted* request: the
data plane decides which provider serves it, and pinning one here would defeat
that. Had the client sent `anthropic/claude-sonnet-4-5`, `ParseModelString` would
have filled it in and the data plane would take the explicit path instead.

### Step 4 — Governance

`ModelAccessHook` checks the virtual key's allow/block lists against
`rctx.Modelkey`; the budget hook checks spend. Both operate on the **key**, never
on the payload — which is why neither has to know this is a chat request.

### Step 5 — The data plane picks a credential

```go
sel := engine.Resolve(rctx.Modelkey, rctx.VirtualKeyPolicy)
// weighted across the VK's providers → anthropic
// round-robin across anthropic credentials → cred #2
rctx.SelectedCredential = sel
rctx.Modelkey.Provider = core.Anthropic
```

**This is the moment the fast path is decided.** Source dialect is `openai`,
destination provider is `anthropic`:

```go
if rctx.SDKProvider == rctx.Modelkey.Provider {
	req.Raw = rctx.BodyBytes    // passthrough: zero translation
} else {
	req.Raw = nil               // typed path: translate
}
```

Had the model resolved to OpenAI or Azure, translation would stop here and the
original bytes would go upstream with only the model field rewritten. It resolved
to Anthropic, so we translate.

### Step 6 — SDK dialect → IR

`openai.ChatToDiffract` produces:

```go
&core.DiffractLLMChatRequest{
	Model: "claude-sonnet-4-5",
	Messages: []core.Message{
		core.TextMessage(core.RoleSystem, "You are terse."),
		core.TextMessage(core.RoleUser, "Weather in Chennai?"),
		{Role: core.RoleAssistant, ToolCalls: []core.ToolCall{{
			ID: "call_1", Type: "function",
			Function: core.ToolCallFunction{Name: "get_weather", Arguments: `{"city":"Chennai"}`},
		}}},
		{Role: core.RoleTool, ToolCallID: "call_1",
			Content: []core.ContentPart{{Type: core.ContentText, Text: ptr(`{"temp_c":34}`)}}},
	},
	Params: core.ChatParams{
		Temperature:      ptr(0.7),
		FrequencyPenalty: ptr(0.5),
		Seed:             ptr(int64(42)),
		Stream:           true,
		Tools: []core.Tool{{
			Kind: core.ToolKindFunction,
			Name: "get_weather", Description: "Current weather",
			Parameters: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
		}},
	},
}
```

This stage is nearly a field copy, because the IR is OpenAI-*compatible* by
design — that is the dialect most traffic arrives in. The expensive reshaping all
lives on the outbound side, which is correct: it runs only when the dialects
differ.

`Parameters` stayed a `json.RawMessage`. The JSON Schema is opaque to the gateway
and re-parsing it would be pure waste.

### Step 7 — IR → Anthropic wire

`anthropic.BuildChat` performs six named transformations. Each is a real
incompatibility, not a stylistic preference.

**T1 — the system message becomes a top-level field.** Anthropic rejects
`role: "system"` inside the messages array outright.

```go
for i := range req.Messages {
	if req.Messages[i].Role == core.RoleSystem || req.Messages[i].Role == core.RoleDeveloper {
		system = append(system, req.Messages[i].Text())
		continue
	}
	// ...
}
```

Multiple system messages are joined rather than dropped: OpenAI permits several,
Anthropic takes one string.

**T2 — content parts map one-to-one onto Anthropic blocks.** This one is nearly
free, and that is the payoff for §4's decision that `Message.Content` is always
`[]ContentPart`: `text` → `text`, `image_url` → `image` with the base64 split out
of the data URI, `thinking` → `thinking` with its signature. Had the IR kept a
string/array union, every adapter would begin by normalising which of the two was
set.

**T3 — `tool_calls` become `tool_use` blocks, and arguments become an object.**

```go
// OpenAI: arguments is a STRING containing JSON.
// Anthropic: input is a JSON OBJECT.
// json.RawMessage embeds the bytes as-is; unmarshalling into map[string]any
// and re-marshalling would allocate once per key for no gain.
blocks = append(blocks, anthropicBlock{
	Type:  "tool_use",
	ID:    tc.ID,
	Name:  tc.Function.Name,
	Input: json.RawMessage(tc.Function.Arguments),
})
```

**T4 — the `tool` role does not exist; results are user messages.** And
consecutive results must **merge into one**, because Anthropic requires strict
user/assistant alternation:

```go
// Two tool results in a row become ONE user message with two tool_result
// blocks. Emitting two user messages back to back is a 400 from anthropic.
if msg.Role == core.RoleTool {
	if last := len(out) - 1; last >= 0 && out[last].Role == "user" && lastWasToolResult {
		out[last].Content = append(out[last].Content, toolResultBlock(msg))
		continue
	}
	out = append(out, anthropicMessage{Role: "user", Content: []anthropicBlock{toolResultBlock(msg)}})
	lastWasToolResult = true
}
```

**T5 — `parameters` is renamed; nothing else moves.** The IR's `Tool` is already
flat, so Anthropic's adapter is a rename — `Parameters` → `input_schema` — and
the OpenAI adapter is the one that pays, re-wrapping into
`{"type":"function","function":{…}}` on the way out. That is the flattening from
§4 earning its keep: the envelope is added once, in the one place that wants it,
instead of being stripped in every place that does not.

Server tools take a different route entirely and are covered in §4a.3 — they are
forwarded intact within a dialect family, remapped without `Config` across
families, and dropped-and-recorded when the destination has no such class.

**T6 — `max_tokens` is required, so it comes from the catalog.**

```go
maxTokens := 4096
if req.Params.MaxCompletionTokens != nil {
	maxTokens = *req.Params.MaxCompletionTokens
} else if meta, ok := catalog.Lookup(key); ok && meta.Limits.MaxOutputTokens > 0 {
	maxTokens = int(meta.Limits.MaxOutputTokens)
}
```

The catalog is consulted, not a constant. Hardcoding 4096 would cap a model that
can emit 64k, and the failure mode is silent truncation — the worst kind.

**Dropped:** `frequency_penalty` and `seed` have no Anthropic equivalent.

```go
target.Dropped = append(target.Dropped, "frequency_penalty", "seed")
```

They come back on `Metadata.Dropped`. The caller asked for determinism via `seed`
and did not get it; they are entitled to know that.

Result on the wire:

```http
POST https://api.anthropic.com/v1/messages HTTP/1.1
x-api-key: sk-ant-api03-...
anthropic-version: 2023-06-01
content-type: application/json
```

```json
{
  "model": "claude-sonnet-4-5",
  "system": "You are terse.",
  "max_tokens": 64000,
  "temperature": 0.7,
  "stream": true,
  "messages": [
    { "role": "user",      "content": [{ "type": "text", "text": "Weather in Chennai?" }] },
    { "role": "assistant", "content": [{ "type": "tool_use", "id": "call_1",
                                         "name": "get_weather", "input": { "city": "Chennai" } }] },
    { "role": "user",      "content": [{ "type": "tool_result", "tool_use_id": "call_1",
                                         "content": "{\"temp_c\":34}" }] }
  ],
  "tools": [{ "name": "get_weather", "description": "Current weather",
              "input_schema": { "type": "object", "properties": { "city": { "type": "string" } } } }]
}
```

Note what happened to the client's `Authorization: Bearer dfl_sk_...`: **nothing**.
The virtual key authenticates the caller to the gateway and is never forwarded.
The provider credential is stamped by `Authorize`, and Anthropic's is `x-api-key`
— a different header entirely, which is exactly why auth belongs to the provider
and not to a shared middleware.

### Step 8 — Anthropic streams back

Anthropic's SSE model has no OpenAI counterpart: six event types where OpenAI has
one.

```
event: message_start        {"message":{"id":"msg_01...","usage":{"input_tokens":412,"cache_read_input_tokens":256}}}
event: content_block_start  {"index":0,"content_block":{"type":"text","text":""}}
event: content_block_delta  {"index":0,"delta":{"type":"text_delta","text":"34"}}
event: content_block_delta  {"index":0,"delta":{"type":"text_delta","text":"°C."}}
event: content_block_stop   {"index":0}
event: message_delta        {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":18}}
event: message_stop
```

`ParseChatEvent` collapses these into the IR's three-event vocabulary. It needs a
small amount of carried state, because Anthropic splits across events what OpenAI
puts in one:

```go
// StreamState is per-request, allocated once, never per event. Anthropic
// reports input tokens on message_start and output tokens on message_delta,
// so the complete Usage cannot be assembled until both have arrived.
type StreamState struct {
	ID         string
	Usage      core.Usage
	StopReason string
	BlockTypes []string // index → "text" | "tool_use", set by content_block_start
	ToolIDs    []string
}
```

Mapping:

| Anthropic event | IR chunk |
|---|---|
| `message_start` | none emitted; stash ID and input usage in `StreamState` |
| `content_block_start` (text) | none; record the block type |
| `content_block_start` (tool_use) | `StreamDelta`, `Delta.ToolCalls[0]` carrying id + name, empty args |
| `content_block_delta` / `text_delta` | `StreamDelta`, `Delta.Content = &text` |
| `content_block_delta` / `input_json_delta` | `StreamDelta`, `Delta.ToolCalls[0].Function.Arguments = partial` |
| `content_block_stop` | none |
| `message_delta` | none; stash `stop_reason` and output usage |
| `message_stop` | `StreamComplete` with the assembled `Usage` and `FinishReason` |

Four of the eight emit nothing. That is correct, not a gap: the IR's contract is
that a `StreamDelta` carries content, and forwarding empty chunks would make the
OpenAI client see `delta: {}` frames it does not expect.

**Stop-reason translation** — entirely different vocabularies:

```go
func finishReason(s string) core.FinishReason {
	switch s {
	case "end_turn", "stop_sequence":
		return core.FinishStop
	case "max_tokens":
		return core.FinishLength
	case "tool_use":
		return core.FinishToolCalls
	case "refusal":
		return core.FinishRefusal
	default:
		return core.FinishStop
	}
}
```

### Step 9 — Usage, and the mapping that silently costs money

```go
// Anthropic's input_tokens EXCLUDES cached tokens.
// OpenAI's prompt_tokens INCLUDES them.
// core.CalculateCost subtracts CachedInputTokens from InputTokens, so it
// expects the OpenAI convention. Copying anthropic's input_tokens straight
// across undercharges every cache hit.
usage.InputTokens = a.Usage.InputTokens + a.Usage.CacheReadInputTokens
usage.CachedInputTokens = a.Usage.CacheReadInputTokens
usage.CacheCreationTokens = a.Usage.CacheCreationInputTokens
usage.OutputTokens = a.Usage.OutputTokens
```

Cache-*creation* tokens are **not** folded into `InputTokens`. `CalculateCost`
bills them separately at `cacheCreationRate`; adding them to the input count
would bill them twice.

Worked through with the numbers above — 412 uncached, 256 cache-read, 18 output:

- `InputTokens = 412 + 256 = 668`, `CachedInputTokens = 256`
- `CalculateCost` computes `uncachedInput = 668 - 256 = 412` → billed at the full input rate ✓
- `256` billed at the cache-read rate ✓ (roughly a tenth)
- `18` billed at the output rate ✓

Copy `input_tokens` naively and you bill 412 at the full rate plus 256 at the read
rate — never charging for 256 tokens of real input. On a caching-heavy workload
that is most of the bill.

Anthropic's 1-hour cache maps to `Usage.CacheLongTTL = true`, which selects the
higher creation rate in `cacheCreationRate`.

### Step 10 — IR → OpenAI SDK shape, out

The client is an OpenAI SDK, so the descriptor's `FromDiffractStream` runs and the
gateway emits OpenAI-shaped SSE:

```
data: {"id":"msg_01...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"34"}}]}
data: {"id":"msg_01...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"°C."}}]}
data: {"id":"msg_01...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],
       "usage":{"prompt_tokens":668,"completion_tokens":18,"total_tokens":686,
                "prompt_tokens_details":{"cached_tokens":256}}}
data: [DONE]
```

The SDK never learns it talked to Anthropic. Meanwhile `rctx` carries the truth —
`Provider: anthropic`, the credential ID, TTFB, the real `core.Usage` — into the
post-response hooks for billing and the metrics engine.

### The whole path

```
POST /openai/v1/chat/completions
        │
        ▼
  descriptor lookup ──► SDKProvider=openai, RequestKind=chat        [map lookup]
        │
        ▼
  openai.ChatToDiffract ─────────────► core.DiffractLLMChatRequest   [field copy]
        │
        ▼
  governance: VK allow-list, budget            [operates on CatalogKey only]
        │
        ▼
  dataplane.Resolve ──► anthropic, credential #2
        │
        ├── SDKProvider == destination? ──► forward rctx.BodyBytes, skip 6 steps
        │
        ▼  (openai ≠ anthropic)
  anthropic.BuildChat ───────────────► POST /v1/messages             [T1..T6]
        │                               x-api-key + anthropic-version
        ▼
  anthropic SSE ──► ParseChatEvent ──► core.DiffractLLMChatStreamChunk
        │                               + core.Usage (cache-inclusive)
        ▼
  openai.ChatFromDiffractStream ─────► chat.completion.chunk SSE
        │
        ▼
  post-response hooks: CalculateCost(pricing, usage) → budget, metrics
```

Two translations, never `N×M`. And when the client's dialect happens to match the
destination, the branch at step 5 skips both of them entirely.

---

## 17. Adding Gemini, sketched

Same four files, a different set of incompatibilities — listed here only to show
the seam holds for a provider that is stranger than Anthropic:

| Concept | Gemini shape | Handled by |
|---|---|---|
| messages | `contents[]`, role is `user` / **`model`** | T-equivalent of T1/T4 |
| message body | `parts[]`, never a bare string | `ContentPart` maps directly |
| system prompt | `systemInstruction`, a top-level `Content` | same as T1 |
| params | nested under `generationConfig` | envelope only |
| tools | `functionDeclarations[]`, OpenAPI-subset schema | schema needs pruning, not reshaping |
| tool call | `functionCall{name,args}` as a part, `args` is an object | same as T3 |
| tool result | `functionResponse` part, role `user` | same as T4 |
| auth | `?key=` query param **or** OAuth bearer | `Authorize` |
| stop reason | `STOP` / `MAX_TOKENS` / `SAFETY` / `RECITATION` | `finishReason` switch |
| usage | `usageMetadata{promptTokenCount, candidatesTokenCount, cachedContentTokenCount}` | `promptTokenCount` **includes** cached — maps straight across, unlike Anthropic |
| streaming | JSON array chunks over `streamGenerateContent?alt=sse` | `ParseChatEvent` |

Nothing on that list requires a new IR field. The one thing that would is
Gemini's `safetySettings`, which has no OpenAI or Anthropic counterpart — and
that is exactly the case `Extra` exists for, until enough providers have it to
justify promoting it to a typed field.

---

## 18. Notes on the current source

`internal/core/kind-speech.go` and `kind-types.go` as they stand were written as
placeholders. §3 and §9 above are the replacements, and the differences that
matter are:

1. `RequestKind` grows from one constant to ten, and gains `ModelType()`.
2. `DiffractLLMSpeechUsage` is deleted; `core.Usage` takes its place, because
   the pricing engine cannot read a three-field `int32` struct.
3. `Provider` leaves the speech request — routing identity is `rctx.Modelkey`,
   and the data plane mutates it during resolution.
4. `StreamingRequest` becomes `SpeechParams.Stream`, with a json tag.
5. `ChunkIndex` and `Latency` move into the shared `Metadata`.

### Decisions taken on `kind-shared.go`

Recorded here because each was argued and settled, and each will look like it
could be "simplified" back later:

1. **`Message.Content` is always `[]ContentPart`.** The string/array union was
   rejected: it forces every adapter, counter and hook to branch on which of two
   fields is set, and nothing in the type system prevents both or neither. Cost
   is ~88 bytes on a text turn; the branch would have been permanent.
2. **`Tool` is flat.** OpenAI's `{"type":"function","function":{…}}` envelope is
   added by OpenAI's adapter on the way out, not carried in the IR. Two of three
   in-scope providers are flat already.
3. **Reasoning signatures live on the part, not the Message.** A response can
   carry several thinking blocks, each with its own signature; one field on the
   Message could not preserve the pairing, and a mispaired signature is a
   rejected follow-up turn.
4. **`ToolCall` and `ToolCallDelta` are separate types.** One type would leave
   `Index` dead on every assembled call and `Name` optional on every one of
   them. The split forced `DiffractLLMChatStreamChunk.Delta` onto `MessageDelta`,
   which is the split immediately paying for itself.
5. **Server tools are a `ToolKind`, not a parallel array**, with the variant
   nested behind one `*ServerTool` pointer rather than inlined — see §4a.3 for
   why Bifrost's inlined form needs custom marshallers and a normalize pass.
   `ServerTool` is three fields: `Type`, `Version`, `Config`. `MaxUses`,
   `AllowedDomains` and `BlockedDomains` were typed in a draft on an unverified
   guess about provider overlap and were cut back into `Config`; promote them
   only once both adapters are written and the overlap is measured.
6. **Server-tool support is a routing predicate, not a translation step.** The
   destination must serve every requested tool or it is not a candidate. The
   alternative — drop the unsupported tool and continue — returns 200 with an
   answer the model produced without ever running the tool, which is the worst
   failure mode available and the one Bifrost ships.
7. **Unknown version strings are rejected, not forwarded.** Bifrost forwards
   them for forward-compatibility, which is correct for a design where server
   tools never cross providers. Once support decides routing, an unrecognised
   string has no provider to resolve to.
8. **No `AuxUsage` field.** A code-interpreter session prices against its own
   catalog row, but the response should not carry instructions for how to bill
   itself — the billing hook does the second lookup off `Usage.CodeSessions`.
