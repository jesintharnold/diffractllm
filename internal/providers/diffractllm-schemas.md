# DiffractLLM schemas

The complete internal representation. Every request and response for every
endpoint passes through these types.

Each section states what Bifrost does, what it costs, and what we do instead.
Bifrost is used two ways and they are never mixed: as a **record of what the
provider APIs actually require** (reliable — they have shipped against the
edges), and as a **design counter-example** (its unions and 22 custom
marshallers are what this schema avoids).

---

## 0. Rules

Every type below follows these. A change that violates one is wrong.

**R1 — One home per concept.** No value representable in two fields. The bug it
prevents: a writer sets one, a reader checks the other, and they silently
disagree.

**R2 — No custom `MarshalJSON`/`UnmarshalJSON` in `core`.** This is a
performance rule. The module uses `bytedance/sonic`, whose speed comes from a
compiled codec generated per type; a type implementing `json.Marshaler` opts out
of that path. Bifrost has 22 such methods on its hottest types. We have zero —
every invariant it enforces at marshal time we enforce in `Validate()` at
admission, which also means a malformed request fails *before* a credential is
picked rather than mid-flight.

**R3 — Discriminator plus flat fields, never a nested union.** A `Type` field the
compiler and the reader both see, not an invariant living in a comment.

**R4 — Pointer only where absent differs from zero.** `Temperature *float64`
because `0` is a real request; `Stream bool` because false and absent are the
same.

**R5 — `Raw []byte` and `Extra map[string]any` on every request and response**,
both `json:"-"`. `Raw` is the same-dialect fast path — forward the caller's bytes
untouched. `Extra` is "we did not model it, we did not lose it".

**R6 — Routing identity never lives in the payload.** Provider and model key are
on `rctx.Modelkey`; the data plane mutates them during resolution. A copy in the
payload goes stale.

**R7 — Unmodelled data is preserved, never invented.** `Payload
json.RawMessage` carries what we do not parse — only for things we did *not*
model, never duplicating a field we did set.

**R8 — Everything a price depends on must reach `core.Usage`.** The pricing
engine reads one type. A response that cannot fill it cannot be billed.

### File layout

```
internal/core/
  kind-types.go            RequestKind, ModelType mapping, context keys
  kind-shared.go           Message, ContentPart, Tool, ToolCall, Metadata, Annotation
  kind-chat-completion.go  chat + text completion
  kind-responses.go        responses API
  kind-embedding.go        embeddings
  kind-image.go            generation, edit, variation
  kind-speech.go           text to speech
  kind-transcription.go    speech to text
  kind-moderation.go       moderation
  kind-models.go           model listing
```

---

## 1. `kind-shared.go`

Types used by two or more kinds.

```go
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---------- roles ----------

type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer" // newer OpenAI models; system elsewhere
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ---------- content ----------

type ContentType string

const (
	ContentText             ContentType = "text"
	ContentRefusal          ContentType = "refusal"
	ContentThinking         ContentType = "thinking"
	ContentImageURL         ContentType = "image_url"
	ContentInputAudio       ContentType = "input_audio"
	ContentFile             ContentType = "file"
	ContentServerToolUse    ContentType = "server_tool_use"
	ContentServerToolResult ContentType = "server_tool_result"
)

// MediaRef is the payload of a non-text content part.
//
// These seven fields are nil on text, refusal and thinking parts, which are the
// overwhelming majority. Inline on ContentPart they cost 56 bytes on every part;
// behind one pointer they cost 8, and a text part allocates no MediaRef at all.
// ContentPart drops from 136 to 88 bytes - a 20-turn conversation goes from
// ~2.7 KB of part headers to ~1.8 KB.
type MediaRef struct {
	URL      *string `json:"url,omitempty"`    // ContentImageURL: https or a data: URI
	Detail   *string `json:"detail,omitempty"` // auto | low | high
	Data     *string `json:"data,omitempty"`   // base64 for audio and file
	Format   *string `json:"format,omitempty"` // wav | mp3, or a mime type
	FileID   *string `json:"file_id,omitempty"`
	FileName *string `json:"file_name,omitempty"`
}

// CacheControl marks an explicit prompt-cache breakpoint. Anthropic is the only
// provider that takes it directly; OpenAI caches automatically and ignores it.
type CacheControl struct {
	Type string `json:"type"`          // "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "5m" | "1h" -> Usage.CacheLongTTL
}

// ContentPart is one element of a message's content.
//
// A flat struct with a discriminator rather than an interface: an interface
// costs a heap allocation and a type switch per part, and a long conversation
// carries hundreds.
type ContentPart struct {
	Type ContentType `json:"type"`

	// Text carries ContentText, ContentRefusal and ContentThinking. It stays a
	// direct field rather than moving into MediaRef because Message.Text()
	// reads it on every part, and an indirection on the hottest accessor is not
	// worth eight bytes.
	Text *string `json:"text,omitempty"`

	// Signature is the opaque proof a provider attaches to a reasoning block.
	// Anthropic rejects a follow-up turn whose thinking block returns without
	// it, so it must round-trip verbatim.
	//
	// It lives on the part, not on the Message, because a response may carry
	// several thinking blocks each with its own signature; one field on the
	// Message could not say which belonged to which.
	Signature *string `json:"signature,omitempty"`

	Media *MediaRef `json:"media,omitempty"`

	// ContentServerToolUse / ContentServerToolResult: the provider ran a tool
	// inside the turn. Informational, but recorded because it is billed.
	ServerToolName ServerToolType  `json:"server_tool_name,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`

	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ---------- messages ----------

// Message is one turn.
//
// Content is ALWAYS a []ContentPart, even for plain text. The wire accepts
// either a bare string or an array, but carrying both shapes forces every
// adapter, token counter and hook to branch on which is set, and nothing in the
// type system can stop a caller setting both or neither.
//
// Bifrost carries both (ChatMessageContent{ContentStr, ContentBlocks}) and pays
// for it: 1,243 references across 169 files, with the same fifteen-line branch
// pasted three times inside a single anthropic converter
// (anthropic/chat.go:698, :725, :775). One representation costs ~88 bytes on a
// text turn and removes that branch permanently.
type Message struct {
	Role    Role          `json:"role"`
	Content []ContentPart `json:"content,omitempty"`

	Name string `json:"name,omitempty"` // optional participant name

	// Assistant turns.
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	Annotations []Annotation `json:"annotations,omitempty"`

	// Tool turns.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// There is deliberately no Message.Refusal field. A refusal is a ContentPart of
// type ContentRefusal - one home (R1). MessageDelta.Refusal exists because a
// delta is a flat fragment and is never scanned as content.

// TextMessage builds the common single-text-part turn.
func TextMessage(role Role, text string) Message {
	return Message{Role: role, Content: []ContentPart{{Type: ContentText, Text: &text}}}
}

// Text concatenates the message's text parts, skipping media, thinking and
// refusals. Adapters that flatten a turn to a string - anthropic's system
// prompt, for one - use this instead of walking the slice.
func (m *Message) Text() string {
	if len(m.Content) == 1 && m.Content[0].Type == ContentText && m.Content[0].Text != nil {
		return *m.Content[0].Text // common turn: no builder, no allocation
	}
	var b strings.Builder
	for i := range m.Content {
		if m.Content[i].Type == ContentText && m.Content[i].Text != nil {
			b.WriteString(*m.Content[i].Text)
		}
	}
	return b.String()
}

// Annotation is a citation attached to assistant output - a URL a web_search
// grounded on, a file span a file_search matched. Without it the gateway counts
// that a server tool ran but discards what it produced.
type Annotation struct {
	Type       string  `json:"type"` // url_citation | file_citation
	StartIndex int     `json:"start_index,omitempty"`
	EndIndex   int     `json:"end_index,omitempty"`
	URL        *string `json:"url,omitempty"`
	Title      *string `json:"title,omitempty"`
	FileID     *string `json:"file_id,omitempty"`
}

// ---------- tools ----------

// ToolKind separates the two things that arrive in a "tools" array. They differ
// in WHO RUNS THE CODE.
type ToolKind string

const (
	// ToolKindFunction: the caller runs it. The model returns a ToolCall, the
	// caller executes it, the result comes back as a RoleTool message.
	ToolKindFunction ToolKind = "function"

	// ToolKindServer: the PROVIDER runs it, inside the turn. Nothing comes back
	// for the caller to execute, and it is billed separately.
	ToolKindServer ToolKind = "server"
)

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
// Only Type and Version are typed; every setting is provider-shaped and rides
// in Config, forwarded verbatim. Bifrost inlines 21 variant fields onto one
// 25-field ChatTool (chatcompletions.go:396) and then needs custom
// MarshalJSON, UnmarshalJSON and a normalizeShape() that nils 15 fields on
// every tool of every request, because nothing stops Type="computer_20251124"
// from also carrying Function. Nesting the variant behind one pointer needs
// none of that.
type ServerTool struct {
	Type ServerToolType `json:"type"`

	// Version pins the provider's exact wire identifier. Empty is the normal
	// case and means "whatever this provider currently calls it", supplied by
	// the provider's own table. Setting it is an explicit statement about which
	// provider serves the request.
	Version string `json:"version,omitempty"`

	// Config is max_uses and allowed_domains for web_search, display_width_px
	// for computer use, vector_store_ids for file_search, server URL and auth
	// for mcp. Raw bytes: we route on the class, not on the settings.
	Config json.RawMessage `json:"config,omitempty"`
}

// Tool is something the model may use.
//
// Flat, not OpenAI's {"type":"function","function":{...}} envelope. Of the
// providers in scope only OpenAI nests; anthropic and gemini are both flat, so
// carrying the envelope would mean two of three adapters begin by unwrapping it
// and every read is tool.Function.Name behind a nil check. OpenAI's adapter adds
// the envelope on the way out - one place, one direction.
type Tool struct {
	Kind ToolKind `json:"kind"`

	// Kind == ToolKindFunction.
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // JSON Schema, never parsed
	Strict      *bool           `json:"strict,omitempty"`

	// Kind == ToolKindServer.
	Server *ServerTool `json:"server,omitempty"`
}

// Validate is the check bifrost performs with normalizeShape() on every marshal.
// Ours runs once, at admission.
func (t *Tool) Validate() error {
	switch t.Kind {
	case ToolKindFunction:
		if t.Name == "" {
			return errors.New("function tool requires a name")
		}
		if t.Server != nil {
			return errors.New("function tool must not carry server config")
		}
	case ToolKindServer:
		if t.Server == nil {
			return errors.New("server tool requires server config")
		}
	default:
		return fmt.Errorf("unknown tool kind %q", t.Kind)
	}
	return nil
}

// ToolCall is the model asking for a function to be run. Always complete: a
// ToolCall that exists has a name and fully assembled arguments.
//
// This is the inverse of Tool. Tool DESCRIBES a function - name, description,
// a JSON Schema. ToolCall INVOKES one - an id, the chosen name, concrete
// arguments. They travel in opposite directions and share only the name.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Arguments is the model's JSON as a string, not a parsed object. Providers
	// do not guarantee it is valid JSON, and parsing here would only mean
	// re-serialising downstream.
	Arguments string `json:"arguments"`
}

// ToolCallDelta is a FRAGMENT of a tool call arriving over a stream. A separate
// type from ToolCall on purpose.
//
// A streamed tool call does not arrive whole - id and name come first, then
// arguments a few characters at a time - and when the model calls several
// functions at once the fragments INTERLEAVE:
//
//	{"index":0,"id":"call_A","function":{"name":"get_weather","arguments":""}}
//	{"index":0,"function":{"arguments":"{\"ci"}}
//	{"index":1,"id":"call_B","function":{"name":"get_time","arguments":""}}
//	{"index":0,"function":{"arguments":"ty\":\"Chennai\"}"}}
//
// Index says which call a fragment belongs to; without it they concatenate into
// invalid JSON. Sharing one type with ToolCall would leave Index dead on every
// completed call and Name optional on every one of them - which is exactly what
// bifrost's ChatAssistantMessageToolCall does (chatcompletions.go:1471).
type ToolCallDelta struct {
	Index int `json:"index"`

	// ID and Name arrive on the first fragment for a given Index and are nil on
	// every fragment after it.
	ID   *string `json:"id,omitempty"`
	Name *string `json:"name,omitempty"`

	// Arguments is a partial JSON string. Append it to the buffer at Index.
	Arguments string `json:"arguments,omitempty"`
}

// ---------- reasoning ----------

// ReasoningConfig is shared by chat and responses.
//
// A struct rather than loose fields because the wire shape nests and because
// the settings are only meaningful together.
type ReasoningConfig struct {
	// Enabled is needed by providers that require reasoning be turned off
	// explicitly rather than by omission.
	Enabled *bool `json:"enabled,omitempty"`

	Effort  *string `json:"effort,omitempty"`  // none | minimal | low | medium | high
	Summary *string `json:"summary,omitempty"` // auto | concise | detailed (responses)

	// MaxTokens is the thinking budget. Optional on OpenAI and REQUIRED by
	// anthropic whenever thinking is enabled - a request without it is a 400.
	// Bifrost's field carries the same note: "required for anthropic"
	// (chatcompletions.go:310). The adapter fills it from
	// catalog.ModelLimits.MaxOutputTokens when the caller omits it.
	MaxTokens *int `json:"max_tokens,omitempty"`

	// Display is anthropic's thinking.display: summarized | omitted.
	Display *string `json:"display,omitempty"`
}

// ---------- common response bits ----------

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
	Type   string          `json:"type"` // text | json_object | json_schema
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict *bool           `json:"strict,omitempty"`
}

// StreamOptions mirrors the wire's stream_options object.
//
// IncludeUsage is a *bool, not a bool: "not set" and "explicitly false" differ,
// and some SDKs send false deliberately.
type StreamOptions struct {
	IncludeUsage       *bool `json:"include_usage,omitempty"`
	IncludeObfuscation *bool `json:"include_obfuscation,omitempty"`
}

// StreamEventType is the kind of a streaming chunk. One vocabulary for every
// request kind, so the server's SSE writer does not branch per endpoint.
type StreamEventType string

const (
	StreamDelta    StreamEventType = "delta"    // incremental content
	StreamComplete StreamEventType = "complete" // final chunk, carries Usage
	StreamError    StreamEventType = "error"    // in-band failure
)

// Metadata is attached to every response for observability and billing.
//
// A value, not a pointer: it is always present, and a pointer would be one
// allocation per response for nothing.
type Metadata struct {
	Provider     Provider      `json:"provider"`
	Model        string        `json:"model"`         // upstream name actually called
	CredentialID string        `json:"credential_id"` // which key served it
	RequestID    string        `json:"request_id,omitempty"`
	TTFB         time.Duration `json:"ttfb,omitempty"`
	Latency      time.Duration `json:"latency,omitempty"`
	ChunkIndex   int64         `json:"chunk_index,omitempty"` // streaming only

	// Dropped names request parameters the destination does not support and
	// which were therefore not sent. Nil in the common case; the caller needs to
	// know when the gateway silently ignored what they asked for.
	Dropped []string `json:"dropped_params,omitempty"`
}
```

### vs Bifrost

| | Bifrost | DiffractLLM |
|---|---|---|
| message content | `{ContentStr *string, ContentBlocks []Block}` + custom marshal that errors | `[]ContentPart` only |
| refusal | `ChatAssistantMessage.Refusal` **and** a refusal content block | `ContentRefusal` part only |
| reasoning signature | `ToolCall.ExtraContent json.RawMessage` (opaque) | `ContentPart.Signature`, paired with its block |
| tool | 25 fields, 3-way union, `normalizeShape()` on every marshal | 6 fields, `Kind` discriminator, `Validate()` at admission |
| tool call | one type, `Index uint16` always present, `ID`/`Name` optional | `ToolCall` + `ToolCallDelta` |
| content part size | — | 136 → **88 bytes** via `MediaRef` |
| custom marshallers | 22 | **0** |

---

## 2. `kind-chat-completion.go` — chat

```go
package core

// AudioParams configures audio output. Required whenever Modalities includes
// "audio"; without both, an audio-capable model silently returns text only.
type AudioParams struct {
	Format string `json:"format,omitempty"` // wav | mp3 | flac | opus | pcm16
	Voice  string `json:"voice,omitempty"`
}

// Prediction supplies expected output so the provider can skip re-generating
// unchanged text. It is a latency feature, which is why it is modelled rather
// than left in Extra.
type Prediction struct {
	Type    string   `json:"type"` // "content"
	Content []string `json:"content"`
}

type ChatParams struct {
	MaxCompletionTokens *int     `json:"max_completion_tokens,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	TopP                *float64 `json:"top_p,omitempty"`
	TopK                *int     `json:"top_k,omitempty"` // anthropic, gemini
	FrequencyPenalty    *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64 `json:"presence_penalty,omitempty"`
	Seed                *int64   `json:"seed,omitempty"`
	Stop                []string `json:"stop,omitempty"`
	N                   *int     `json:"n,omitempty"`

	// LogitBias steers individual tokens. Dropped silently today.
	LogitBias map[string]float64 `json:"logit_bias,omitempty"`

	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
	LogProbs      bool           `json:"logprobs,omitempty"`
	TopLogProbs   *int           `json:"top_logprobs,omitempty"`

	Tools             []Tool  `json:"tools,omitempty"`
	ToolChoice        *string `json:"tool_choice,omitempty"` // none|auto|required|<name>
	ParallelToolCalls *bool   `json:"parallel_tool_calls,omitempty"`

	ResponseFormat *ResponseFormat  `json:"response_format,omitempty"`
	Reasoning      *ReasoningConfig `json:"reasoning,omitempty"`
	Verbosity      *string          `json:"verbosity,omitempty"` // low | medium | high

	// Modalities is ["text"] or ["text","audio"]. Paired with Audio.
	Modalities []string     `json:"modalities,omitempty"`
	Audio      *AudioParams `json:"audio,omitempty"`

	Prediction *Prediction `json:"prediction,omitempty"`

	// ServiceTier is the tier REQUESTED. The tier that actually served comes
	// back on the response and is what drives pricing - see DiffractLLMChatResponse.
	ServiceTier ServiceTier `json:"service_tier,omitempty"`

	// PromptCacheKey routes the request to a warm cache. Dropping it costs money
	// rather than correctness, which makes it easy to miss.
	PromptCacheKey       *string `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention *string `json:"prompt_cache_retention,omitempty"`

	Store            *bool             `json:"store,omitempty"`
	User             *string           `json:"user,omitempty"`
	SafetyIdentifier *string           `json:"safety_identifier,omitempty"` // replaces User on newer models
	Metadata         map[string]string `json:"metadata,omitempty"`          // caller tagging, echoed back
}

type DiffractLLMChatRequest struct {
	Model    string     `json:"model"` // upstream name, alias already applied
	Messages []Message  `json:"messages"`
	Params   ChatParams `json:"params,omitzero"`

	// Raw is the client's original body. Non-nil means the dialect matched the
	// destination and the provider forwards these bytes with only the model
	// rewritten; Messages and Params are then not populated.
	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

func (r *DiffractLLMChatRequest) IsRaw() bool { return len(r.Raw) > 0 }

// ---------- response ----------

type ChatChoice struct {
	Index        int          `json:"index"`
	Message      Message      `json:"message,omitzero"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	LogProbs     *ChatLogProbs `json:"logprobs,omitempty"`
}

type ChatLogProbs struct {
	Content []TokenLogProb `json:"content,omitempty"`
	Refusal []TokenLogProb `json:"refusal,omitempty"`
}

type TokenLogProb struct {
	Token       string         `json:"token"`
	LogProb     float64        `json:"logprob"`
	Bytes       []byte         `json:"bytes,omitempty"`
	TopLogProbs []TokenLogProb `json:"top_logprobs,omitempty"`
}

type DiffractLLMChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object,omitempty"` // "chat.completion"
	Created int64        `json:"created"`
	Choices []ChatChoice `json:"choices"`

	// ServiceTier is the tier that ACTUALLY served, which can differ from the
	// tier requested - "auto" resolves to flex or priority. Usage.Tier drives
	// CalculateCost, so billing against the requested tier is wrong whenever
	// they diverge.
	ServiceTier       ServiceTier `json:"service_tier,omitempty"`
	SystemFingerprint *string     `json:"system_fingerprint,omitempty"`

	// Usage is core.Usage, the type CalculateCost consumes. No per-kind usage
	// struct: one that pricing cannot read is worse than none.
	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

func (r *DiffractLLMChatResponse) IsRaw() bool { return len(r.Raw) > 0 }

// ---------- stream ----------

// MessageDelta is the incremental half of a Message. Every field is a fragment:
// Content is the next few characters, not a finished string; ToolCalls are
// ToolCallDelta values. Reusing Message here would make every field "maybe
// complete, maybe not".
type MessageDelta struct {
	Role Role `json:"role,omitempty"` // first chunk only

	Content *string `json:"content,omitempty"`
	Refusal *string `json:"refusal,omitempty"`

	// Thinking streams in pieces; Signature lands whole on the block's last
	// fragment.
	Thinking  *string `json:"thinking,omitempty"`
	Signature *string `json:"signature,omitempty"`

	ToolCalls   []ToolCallDelta `json:"tool_calls,omitempty"`
	Annotations []Annotation    `json:"annotations,omitempty"`
}

type DiffractLLMChatStreamChunk struct {
	Type    StreamEventType `json:"type"`
	ID      string          `json:"id,omitempty"`
	Object  string          `json:"object,omitempty"` // "chat.completion.chunk"
	Created int64           `json:"created,omitempty"`

	Index        int          `json:"index"`
	Delta        MessageDelta `json:"delta,omitzero"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`

	Usage    Usage             `json:"usage,omitzero"` // Complete only
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"` // StreamError only

	Raw []byte `json:"-"` // passthrough: the provider's SSE line, as is
}
```

### vs Bifrost

| | Bifrost | DiffractLLM |
|---|---|---|
| reasoning | `ChatReasoning{Enabled, Effort, MaxTokens, Display}` | same, shared with responses |
| usage | `BifrostLLMUsage` — 5 fields + details structs | `core.Usage` — 48 fields, feeds `CalculateCost` directly |
| served tier | `Speed`/`InferenceGeo` as `json:"-"` side channels | `ServiceTier` on the response, typed |
| stream usage | `ChatStreamOptions{IncludeUsage, IncludeObfuscation}` | same |
| citations | `ChatAssistantMessageAnnotation` | `Annotation` |

Bifrost's `ChatParameters` also carries `MCPServers`, `Container`, `TaskBudget`,
`ContextManagement` and `WebSearchOptions`. Those are the **server-tool
surface** and route through `Tool{Kind: ToolKindServer}` here, so they are not
duplicated as parameters.

---

## 3. `kind-chat-completion.go` — text completion

The legacy `/v1/completions` endpoint. A **separate params type**, not
`ChatParams`.

```go
package core

// CompletionParams is not ChatParams. The endpoints diverge on four fields that
// chat does not have and cannot express:
//
//	BestOf   - generate N server-side, return the best one
//	Echo     - echo the prompt back with the completion
//	Suffix   - text after the insertion point (fill-in-the-middle)
//	LogProbs - an INT (how many alternatives), not chat's bool
//
// Reusing ChatParams would silently drop the first three and mistype the
// fourth.
type CompletionParams struct {
	MaxTokens        *int               `json:"max_tokens,omitempty"`
	Temperature      *float64           `json:"temperature,omitempty"`
	TopP             *float64           `json:"top_p,omitempty"`
	N                *int               `json:"n,omitempty"`
	BestOf           *int               `json:"best_of,omitempty"`
	Echo             *bool              `json:"echo,omitempty"`
	Suffix           *string            `json:"suffix,omitempty"`
	LogProbs         *int               `json:"logprobs,omitempty"`
	FrequencyPenalty *float64           `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64           `json:"presence_penalty,omitempty"`
	LogitBias        map[string]float64 `json:"logit_bias,omitempty"`
	Seed             *int64             `json:"seed,omitempty"`
	Stop             []string           `json:"stop,omitempty"`
	Stream           bool               `json:"stream,omitempty"`
	StreamOptions    *StreamOptions     `json:"stream_options,omitempty"`
	User             *string            `json:"user,omitempty"`
}

type DiffractLLMCompletionRequest struct {
	Model  string           `json:"model"`
	Prompt []string         `json:"prompt"` // the API accepts a string or an array
	Params CompletionParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// CompletionLogProbs is a completely different shape from ChatLogProbs.
// Completion reports parallel arrays indexed by token position; chat reports a
// list of per-token objects. Sharing one type would misrepresent both.
type CompletionLogProbs struct {
	TextOffset    []int                `json:"text_offset,omitempty"`
	TokenLogProbs []float64            `json:"token_logprobs,omitempty"`
	Tokens        []string             `json:"tokens,omitempty"`
	TopLogProbs   []map[string]float64 `json:"top_logprobs,omitempty"`
}

type CompletionChoice struct {
	Index        int                 `json:"index"`
	Text         string              `json:"text"`
	FinishReason FinishReason        `json:"finish_reason,omitempty"`
	LogProbs     *CompletionLogProbs `json:"logprobs,omitempty"`
}

type DiffractLLMCompletionResponse struct {
	ID                string             `json:"id"`
	Object            string             `json:"object,omitempty"` // "text_completion"
	Created           int64              `json:"created"`
	Choices           []CompletionChoice `json:"choices"`
	SystemFingerprint *string            `json:"system_fingerprint,omitempty"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type DiffractLLMCompletionStreamChunk struct {
	Type    StreamEventType `json:"type"`
	ID      string          `json:"id,omitempty"`
	Created int64           `json:"created,omitempty"`

	Index        int          `json:"index"`
	Text         string       `json:"text,omitempty"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`

	Usage    Usage             `json:"usage,omitzero"`
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"`

	Raw []byte `json:"-"`
}
```

### vs Bifrost

Bifrost keeps `TextCompletionParameters` separate too, with the same four
distinguishing fields, and a separate `TextCompletionLogProb` — confirming both
splits. Its `TextCompletionInput{PromptStr, PromptArray}` union with a
marshal-time one-of is collapsed here to `Prompt []string`; the string form is
`len == 1`, and `Raw` preserves the caller's exact bytes on the passthrough path.

---

## 4. `kind-responses.go` — the Responses API

Separate from chat for two structural reasons: it is an **item list, not a
message list** (the same list type goes in as comes out — a continued turn sends
the previous turn's output back verbatim), and it is **stateful**
(`previous_response_id` chains turns server-side). The catalog also files 83
models under `responses` mode with zero overlap into `chat`.

```go
package core

// ResponseItemType discriminates a ResponseItem.
//
// The upstream API enumerates 26 types (bifrost responses.go:1187). These eight
// are the ones the gateway must understand to route, bill and round-trip
// correctly; anything else is ItemUnmodelled and rides in Payload. The gateway
// does not need to understand an item to forward it, only to not lose it.
type ResponseItemType string

const (
	ItemMessage         ResponseItemType = "message"
	ItemReasoning       ResponseItemType = "reasoning"
	ItemFunctionCall    ResponseItemType = "function_call"
	ItemFunctionOutput  ResponseItemType = "function_call_output"
	ItemServerToolCall  ResponseItemType = "server_tool_call"
	ItemApprovalRequest ResponseItemType = "approval_request"  // mcp: needs a caller answer
	ItemReference       ResponseItemType = "item_reference"
	ItemUnmodelled      ResponseItemType = "unmodelled"
)

type ItemStatus string

const (
	ItemInProgress   ItemStatus = "in_progress"
	ItemCompleted    ItemStatus = "completed"
	ItemIncomplete   ItemStatus = "incomplete"
	ItemInterpreting ItemStatus = "interpreting"
	ItemFailed       ItemStatus = "failed"
)

// ReasoningSummary is one summary block. Typed rather than a bare string:
// gpt-oss models return reasoning_text blocks where other models return
// summary_text, and the distinction is lost by []string.
type ReasoningSummary struct {
	Type string `json:"type"` // summary_text | reasoning_text
	Text string `json:"text"`
}

// ResponseItem is the unit of BOTH input and output.
//
// One type for both directions because that is how the API works: a continued
// turn sends the previous turn's output items straight back as input. Two types
// would put a conversion in the middle of the one path that has to be lossless.
type ResponseItem struct {
	Type   ResponseItemType `json:"type"`
	ID     string           `json:"id,omitempty"`
	Status ItemStatus       `json:"status,omitempty"`

	// ItemMessage.
	Role    Role          `json:"role,omitempty"`
	Content []ContentPart `json:"content,omitempty"`

	// Phase labels an assistant message as intermediate "commentary" or final
	// "final_answer". Required on gpt-5.3-codex and later during history replay;
	// bifrost's note is that dropping it "causes significant performance
	// degradation" (responses.go:1222).
	Phase *string `json:"phase,omitempty"`

	// ItemReasoning.
	Summary []ReasoningSummary `json:"summary,omitempty"`

	// EncryptedContent is the opaque reasoning blob. It MUST be echoed back on
	// the next turn or the model loses its reasoning chain - the same contract
	// as ContentPart.Signature, and the same bug if dropped.
	//
	// It is only returned when the request asked for it via
	// Include: [IncludeReasoningEncrypted]. Without that, a stateless multi-turn
	// conversation degrades silently.
	EncryptedContent *string `json:"encrypted_content,omitempty"`

	// ItemFunctionCall and ItemFunctionOutput.
	//
	// CallID is the correlation id and appears on BOTH: the call announces it,
	// the output references it. It is distinct from ID, which identifies the
	// item itself ("fc_..." vs "call_...").
	CallID string `json:"call_id,omitempty"`
	Name   string `json:"name,omitempty"`

	// Arguments is json.RawMessage, not string, and must stay that way.
	// function_call.arguments is a JSON STRING but tool_search_call.arguments is
	// a JSON OBJECT. A *string field fails to decode the second, and bifrost's
	// note on exactly this says the failure "silently drops the item mid-stream
	// and hangs streaming clients".
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Output    *string         `json:"output,omitempty"`

	// ItemServerToolCall - the provider ran it. Informational, but recorded
	// because it is billed.
	ServerToolName ServerToolType `json:"server_tool_name,omitempty"`

	// ItemApprovalRequest - mcp asking permission before running a tool. Unlike
	// a server tool call this BLOCKS on the caller, which is why it is its own
	// type rather than folded into ItemServerToolCall.
	ApprovalID string  `json:"approval_id,omitempty"`
	Approve    *bool   `json:"approve,omitempty"`
	Reason     *string `json:"reason,omitempty"`

	// ItemUnmodelled - PayloadType is the provider's original type string,
	// Payload the original bytes. Set ONLY for types not in the list above;
	// never a duplicate of a field already populated (R7).
	PayloadType string          `json:"payload_type,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// ResponseInclude opts into data the response omits by default. Typed rather
// than free strings because omitting IncludeReasoningEncrypted breaks stateless
// multi-turn silently.
type ResponseInclude string

const (
	IncludeReasoningEncrypted ResponseInclude = "reasoning.encrypted_content"
	IncludeWebSearchSources   ResponseInclude = "web_search_call.action.sources"
	IncludeCodeOutputs        ResponseInclude = "code_interpreter_call.outputs"
	IncludeFileSearchResults  ResponseInclude = "file_search_call.results"
	IncludeOutputLogProbs     ResponseInclude = "message.output_text.logprobs"
	IncludeInputImageURL      ResponseInclude = "message.input_image.image_url"
)

// TextConfig is where the Responses API puts structured output, rather than
// chat's top-level response_format.
type TextConfig struct {
	Format    *ResponseFormat `json:"format,omitempty"`
	Verbosity *string         `json:"verbosity,omitempty"` // low | medium | high
}

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
	SafetyIdentifier *string   `json:"safety_identifier,omitempty"`
}

type DiffractLLMResponsesRequest struct {
	Model string `json:"model"`

	// Input is items, not messages.
	Input []ResponseItem `json:"input"`

	// Instructions is the system prompt as its own field rather than an item
	// with a system role.
	Instructions *string `json:"instructions,omitempty"`

	// PreviousResponseID chains one turn to one turn. Conversation binds the
	// response to a server-side conversation object. They are different
	// mechanisms and both exist upstream.
	PreviousResponseID *string `json:"previous_response_id,omitempty"`
	Conversation       *string `json:"conversation,omitempty"`
	Store              *bool   `json:"store,omitempty"`

	Include []ResponseInclude `json:"include,omitempty"`

	Params ResponsesParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

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

	// IncompleteReason is WHY generation stopped. Status alone says a response
	// is incomplete but not why, so "hit the token cap" and "content filtered"
	// would be indistinguishable - the same information chat carries in
	// FinishReason, which is why it reuses that type. Populated from
	// incomplete_details.reason.
	IncompleteReason FinishReason `json:"incomplete_reason,omitempty"`

	Output      []ResponseItem `json:"output"`
	ServiceTier ServiceTier    `json:"service_tier,omitempty"` // the tier that served

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// ResponsesDeltaTarget says WHAT a streamed fragment is a fragment OF.
//
// The upstream stream has 55 event types and six of them carry plain text that
// lands in completely different places:
//
//	response.output_text.delta                -> the answer
//	response.reasoning_summary_text.delta     -> the reasoning summary
//	response.function_call_arguments.delta    -> a tool call's arguments
//	response.code_interpreter_call_code.delta -> generated code
//	response.mcp_call_arguments.delta         -> an mcp call's arguments
//	response.custom_tool_call_input.delta     -> a custom tool's input
//
// Collapsing them to one Delta string makes reasoning text indistinguishable
// from answer text. Same defect as sharing ToolCall with ToolCallDelta, fixed
// the same way: name the target.
type ResponsesDeltaTarget string

const (
	DeltaOutputText       ResponsesDeltaTarget = "output_text"
	DeltaReasoningSummary ResponsesDeltaTarget = "reasoning_summary"
	DeltaFunctionArgs     ResponsesDeltaTarget = "function_arguments"
	DeltaRefusal          ResponsesDeltaTarget = "refusal"
	DeltaCode             ResponsesDeltaTarget = "code"
	DeltaMCPArgs          ResponsesDeltaTarget = "mcp_arguments"
	DeltaCustomTool       ResponsesDeltaTarget = "custom_tool_input"
)

type DiffractLLMResponsesStreamChunk struct {
	Type     StreamEventType `json:"type"`
	ID       string          `json:"id,omitempty"`
	Sequence int64           `json:"sequence,omitempty"`

	// ItemIndex and ItemID identify which output item this event concerns. The
	// API interleaves items, so a flat delta is not sufficient.
	ItemIndex int    `json:"item_index"`
	ItemID    string `json:"item_id,omitempty"`

	Target ResponsesDeltaTarget `json:"target,omitempty"`
	Delta  *string              `json:"delta,omitempty"`

	// Item is populated on output_item.added and output_item.done, where a whole
	// item boundary is announced rather than a fragment.
	Item *ResponseItem `json:"item,omitempty"`

	Status           ResponseStatus `json:"status,omitempty"`
	IncompleteReason FinishReason   `json:"incomplete_reason,omitempty"`

	Usage    Usage             `json:"usage,omitzero"`
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"`

	Raw []byte `json:"-"`
}
```

### The 55 upstream events

| upstream | IR chunk |
|---|---|
| `response.created`, `.in_progress`, `.queued`, `.ping` | none |
| `response.output_item.added` / `.done` | `StreamDelta` with `Item` set |
| `response.output_text.delta` | `StreamDelta`, `DeltaOutputText` |
| `response.reasoning_summary_text.delta` | `StreamDelta`, `DeltaReasoningSummary` |
| `response.function_call_arguments.delta` | `StreamDelta`, `DeltaFunctionArgs` |
| `response.mcp_call_arguments.delta` | `StreamDelta`, `DeltaMCPArgs` |
| `response.custom_tool_call_input.delta` | `StreamDelta`, `DeltaCustomTool` |
| `response.code_interpreter_call_code.delta` | `StreamDelta`, `DeltaCode` |
| `response.refusal.delta` | `StreamDelta`, `DeltaRefusal` |
| `*.in_progress`, `*.searching`, `*.interpreting`, `*_part.*`, `*.done` | none |
| `*_call.completed` (web_search, code_interpreter, mcp) | `StreamDelta` with `Item` — where server-tool usage is counted |
| `response.completed` / `.incomplete` / `.failed` | `StreamComplete` |
| `error` | `StreamError` |

### vs Bifrost

| | Bifrost | DiffractLLM |
|---|---|---|
| item type | one `ResponsesMessage` with `*ResponsesToolMessage` + `*ResponsesReasoning` **embedded pointers** | one flat `ResponseItem` with `Type` |
| the invariant | *"Only one of the following can be non-nil at a time, otherwise the JSON marshalling will override the common fields"* — a comment | a discriminator |
| unmodelled items | `rawPreserved []byte` + custom Marshal/Unmarshal + `isRawPreservedItem()` | `ItemUnmodelled` + `PayloadType` + `Payload` |
| delta target | implicit in 55 event constants | `ResponsesDeltaTarget`, 7 values |
| arguments | `*string`, needs a shadow decode for object-valued cases | `json.RawMessage`, accepts both |

---

## 5. `kind-embedding.go`

```go
package core

import "errors"

// EmbeddingInput preserves the four shapes the wire accepts. Exactly one is set.
//
// The wire field is a bare JSON value with no discriminator, so the inbound
// adapter sniffs the shape and records which it found. Keeping all four means
// the outbound adapter never re-derives what the caller meant:
//
//	Text       -> bedrock titan (inputText, single string only - must fan out)
//	Texts      -> cohere (texts, always an array), openai batch
//	Embedding  -> pre-tokenized single input
//	Embeddings -> pre-tokenized batch
type EmbeddingInput struct {
	Text       *string   `json:"text,omitempty"`
	Texts      []string  `json:"texts,omitempty"`
	Embedding  []int32   `json:"embedding,omitempty"`
	Embeddings [][]int32 `json:"embeddings,omitempty"`
}

type EmbeddingInputShape uint8

const (
	InputEmpty EmbeddingInputShape = iota
	InputText
	InputTexts
	InputTokens
	InputTokenBatch
)

// Shape reports which variant is set, so call sites switch instead of running
// four nil checks in order.
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

// Count returns how many vectors this input produces - what an adapter needs to
// choose between a single-item endpoint and a batch one (gemini's embedContent
// vs batchEmbedContents; titan, which must fan out).
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

// Validate enforces the one-of invariant at ADMISSION.
//
// Bifrost enforces the same rule inside EmbeddingInput.MarshalJSON
// (embedding.go:47), which means a malformed input is only caught at
// serialisation - after routing, after a credential has been picked,
// mid-request. Checking on the way in fails before anything is spent.
func (e *EmbeddingInput) Validate() error {
	set := 0
	for _, ok := range []bool{e.Text != nil, e.Texts != nil, e.Embedding != nil, e.Embeddings != nil} {
		if ok {
			set++
		}
	}
	switch {
	case set == 0:
		return errors.New("embedding input is empty")
	case set > 1:
		return errors.New("embedding input must set exactly one of: text, texts, embedding, embeddings")
	}
	return nil
}

type EmbeddingParams struct {
	Dimensions     *int    `json:"dimensions,omitempty"`      // truncate the output vector
	EncodingFormat *string `json:"encoding_format,omitempty"` // float|base64|int8|uint8|binary|ubinary

	// InputType is required by cohere (search_document | search_query |
	// classification | clustering) and is gemini's taskType. OpenAI has no
	// equivalent and ignores it. A cohere request without it does not error - it
	// returns worse vectors, silently - which is why it is modelled rather than
	// left in Extra.
	InputType *string `json:"input_type,omitempty"`

	Truncate *string `json:"truncate,omitempty"` // NONE | START | END
	User     *string `json:"user,omitempty"`
}

type DiffractLLMEmbeddingRequest struct {
	Model  string          `json:"model"`
	Input  EmbeddingInput  `json:"input"`
	Params EmbeddingParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// EmbeddingFormat discriminates the vector representation.
//
// Present from day one even though openai only produces Float and Base64. Added
// later, every consumer that reads an embedding would have to start branching -
// the ContentStr disease. With the switch present up front, adding cohere or
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
//   - Float2D covers providers that return nested arrays. Nothing in scope
//     emits it, but a decode that cannot represent the shape fails the whole
//     response rather than one field.
//   - Uint32 is []int32 rather than []uint8, matching bifrost's
//     EmbeddingInt32Array - bedrock populates that one
//     (bedrock/embedding.go:266) and a narrower type would truncate.
//
// Float is float64, not float32. float32 is lossless for openai, whose vectors
// are float32 underneath, but it imposes a narrowing on every other provider,
// and a gateway that quietly changes the numbers it forwards is not
// transparent. Bifrost states the same reason on the same struct: "Embedding
// responses preserve provider precision in normalized API output."
//
// "object": "embedding" is not carried - a constant the adapter writes.
type Embedding struct {
	Index  int             `json:"index"`
	Format EmbeddingFormat `json:"format"`

	Float   []float64   `json:"float,omitempty"`
	Float2D [][]float64 `json:"float_2d,omitempty"`
	Base64  []byte      `json:"base64,omitempty"`
	Int8    []int8      `json:"int8,omitempty"`
	Uint32  []int32     `json:"uint32,omitempty"`
}

// Dimensions returns the vector length regardless of representation.
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

type DiffractLLMEmbeddingResponse struct {
	Data []Embedding `json:"data"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}
```

Embeddings do not stream.

---

## 6. `kind-image.go` — generation, edit, variation

Three request kinds, **one** response type. Bifrost reaches the same conclusion
(`BifrostImageVariationResponse = BifrostImageGenerationResponse`).

The critical property here is not the payload — it is that **image pricing
depends on request parameters the provider does not echo back**. `size` and
`quality` are the pricing selectors (`core.SelectorSet`), and OpenAI's response
omits them. Bifrost solves this with `BackfillParams(req)` copying them onto the
response after the fact. We put them in `Usage` at translation time, where
`CalculateCost` already looks.

```go
package core

// ImageRef is one image supplied by the caller. Exactly one source is set.
type ImageRef struct {
	URL      *string `json:"url,omitempty"`
	Base64   *string `json:"b64,omitempty"`
	Bytes    []byte  `json:"-"` // multipart upload
	FileID   *string `json:"file_id,omitempty"`
	MimeType string  `json:"mime_type,omitempty"`
	Filename string  `json:"filename,omitempty"`
}

// ImageParams covers generation, edit and variation. Fields not meaningful for
// a given kind are simply nil - three near-identical param structs would be
// worse, and bifrost carries exactly that (ImageGenerationParameters,
// ImageEditParameters, ImageVariationParameters) with heavy overlap.
type ImageParams struct {
	N *int `json:"n,omitempty"` // 1-10

	// Size and Quality are PRICING SELECTORS as well as parameters. They must
	// survive translation intact or the request prices against the base row
	// instead of its variant. See Selectors().
	Size    *string `json:"size,omitempty"`    // 1024x1024, 1536x1024, auto, ...
	Quality *string `json:"quality,omitempty"` // low|medium|high|auto|hd|standard
	Style   *string `json:"style,omitempty"`   // vivid | natural

	AspectRatio *string `json:"aspect_ratio,omitempty"`

	Background        *string `json:"background,omitempty"`         // transparent|opaque|auto
	OutputFormat      *string `json:"output_format,omitempty"`      // png|jpeg|webp
	OutputCompression *int    `json:"output_compression,omitempty"` // 0-100
	ResponseFormat    *string `json:"response_format,omitempty"`    // url | b64_json
	Moderation        *string `json:"moderation,omitempty"`         // low | auto

	// Diffusion-model controls (stability, bedrock, replicate).
	Seed              *int    `json:"seed,omitempty"`
	NegativePrompt    *string `json:"negative_prompt,omitempty"`
	NumInferenceSteps *int    `json:"num_inference_steps,omitempty"`

	// Edit only.
	EditType      *string `json:"edit_type,omitempty"`      // inpainting|outpainting|background_removal|upscale|...
	InputFidelity *string `json:"input_fidelity,omitempty"` // low | high

	// Streaming. PartialImages requests 0-3 progressive previews.
	Stream        bool `json:"stream,omitempty"`
	PartialImages *int `json:"partial_images,omitempty"`

	User *string `json:"user,omitempty"`
}

// Selectors builds the pricing selector set from the request parameters. This is
// the bridge to BasePricingSnapshot.find - without it an image request prices
// against the base row rather than its size/quality variant, which for
// gpt-image-1 is a 15x error.
func (p *ImageParams) Selectors() map[string]string {
	values := make(map[string]string, 3)
	if p.Size != nil {
		values["size"] = *p.Size
	}
	if p.Quality != nil {
		values["quality"] = *p.Quality
	}
	if p.NumInferenceSteps != nil {
		values["steps"] = strconv.Itoa(*p.NumInferenceSteps)
	}
	return values
}

// DiffractLLMImageRequest serves ImageGenRequest, ImageEditRequest and
// ImageVariationRequest. RequestKind on rctx says which; the fields used differ.
type DiffractLLMImageRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt,omitempty"` // required for gen/edit, absent for variation

	// Images is the source for edit and variation. Mask marks the region to
	// replace on an edit.
	Images []ImageRef `json:"images,omitempty"`
	Mask   *ImageRef  `json:"mask,omitempty"`

	Params ImageParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type ImageData struct {
	Index         int     `json:"index"`
	URL           *string `json:"url,omitempty"`
	Base64        *string `json:"b64_json,omitempty"`
	RevisedPrompt *string `json:"revised_prompt,omitempty"`
	Seed          *int    `json:"seed,omitempty"`
	FinishReason  *string `json:"finish_reason,omitempty"`
}

type DiffractLLMImageResponse struct {
	ID      string      `json:"id,omitempty"`
	Created int64       `json:"created"`
	Data    []ImageData `json:"data"`

	// Served describes what the provider actually produced, which can differ
	// from what was asked (size "auto" resolves to a concrete size). These are
	// what Usage.ImageQuality and the selector lookup must use for billing -
	// the requested values are a fallback when the provider does not report.
	Served ImageServed `json:"served,omitzero"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type ImageServed struct {
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	AspectRatio  string `json:"aspect_ratio,omitempty"`
	Background   string `json:"background,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
}

// DiffractLLMImageStreamChunk carries progressive previews.
type DiffractLLMImageStreamChunk struct {
	Type     StreamEventType `json:"type"`
	ID       string          `json:"id,omitempty"`
	Sequence int64           `json:"sequence,omitempty"`

	// Index is which image (0..N-1); PartialIndex is which preview of it.
	Index        int     `json:"index"`
	PartialIndex *int    `json:"partial_index,omitempty"`
	Base64       *string `json:"b64_json,omitempty"`
	URL          *string `json:"url,omitempty"`

	Served   ImageServed       `json:"served,omitzero"`
	Usage    Usage             `json:"usage,omitzero"` // Complete only
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"`

	Raw []byte `json:"-"`
}
```

**Billing note.** Image usage maps onto `core.Usage` as:

```go
usage.InputImages      = len(req.Images)          // the provider does not report it
usage.ImageQuality     = res.Served.Quality       // selector, drives outputImageRate
usage.OutputImages     = len(res.Data)
usage.InputImageTokens = ...                      // when the provider reports token details
usage.OutputImageTokens= ...
```

Bifrost carries a separate `ImageUsage` with its own `InputTokensDetails` and a
`NumInputImages int json:"-"` backfilled from the request. We have one `Usage`,
so those values go straight into the fields `CalculateCost` already reads.

### vs Bifrost

| | Bifrost | DiffractLLM |
|---|---|---|
| request types | 3 separate request + 3 param structs | 1 request, 1 `ImageParams`, kind on `rctx` |
| response types | 1 (`ImageVariationResponse = ImageGenerationResponse`) | 1 |
| pricing inputs | `BackfillParams(req)` copies size/quality onto the response after the fact | `Served` on the response + `Selectors()` on the request |
| usage | separate `ImageUsage` | `core.Usage` |

---

## 7. `kind-speech.go` — text to speech

```go
package core

// VoiceConfig is one speaker in a multi-speaker synthesis.
type VoiceConfig struct {
	Speaker string `json:"speaker"`
	Voice   string `json:"voice"`
}

// SpeechVoice is either a single voice name or a multi-speaker cast.
//
// Bifrost models the same thing as a two-field union with a custom MarshalJSON
// that errors when both are set (speech.go). Ours uses the slice alone: a single
// voice is Voices[0] with an empty Speaker, and Voice() returns it. One field,
// no invariant to violate, no marshaller.
type SpeechVoice struct {
	Voices []VoiceConfig `json:"voices,omitempty"`
}

// Voice returns the single-voice name, or "" for a multi-speaker cast.
func (s *SpeechVoice) Voice() string {
	if len(s.Voices) == 1 && s.Voices[0].Speaker == "" {
		return s.Voices[0].Voice
	}
	return ""
}

func (s *SpeechVoice) IsMulti() bool { return len(s.Voices) > 1 }

type SpeechParams struct {
	Instructions   *string  `json:"instructions,omitempty"`
	ResponseFormat *string  `json:"response_format,omitempty"` // mp3|opus|aac|flac|wav|pcm
	Speed          *float64 `json:"speed,omitempty"`
	LanguageCode   *string  `json:"language_code,omitempty"`

	Stream       bool    `json:"stream,omitempty"`
	StreamFormat *string `json:"stream_format,omitempty"` // sse | audio

	// WithTimestamps requests character-level alignment in the response.
	WithTimestamps *bool `json:"with_timestamps,omitempty"`
}

type DiffractLLMSpeechRequest struct {
	Model string      `json:"model"`
	Input string      `json:"input"`
	Voice SpeechVoice `json:"voice"`

	Params SpeechParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// InputCharacters is the billing unit for TTS, and it counts RUNES, not bytes.
//
// len(s) on a UTF-8 string overcounts every non-ASCII character - Tamil, Hindi,
// Chinese and emoji all bill 3-4x too high. Bifrost uses
// utf8.RuneCountInString for the same field (speech.go BackfillParams); this
// method exists so no adapter reaches for len().
func (r *DiffractLLMSpeechRequest) InputCharacters() int64 {
	return int64(utf8.RuneCountInString(r.Input))
}

// SpeechAlignment is character-level timing for audio-text synchronisation.
type SpeechAlignment struct {
	Characters       []string  `json:"characters"`
	CharStartTimesMs []float64 `json:"char_start_times_ms"`
	CharEndTimesMs   []float64 `json:"char_end_times_ms"`
}

// DiffractLLMSpeechResponse carries raw audio. Audio is always the provider's
// bytes, so there is no passthrough distinction to draw.
type DiffractLLMSpeechResponse struct {
	Audio       []byte `json:"-"`
	ContentType string `json:"content_type"`

	Alignment           *SpeechAlignment `json:"alignment,omitempty"`
	NormalizedAlignment *SpeechAlignment `json:"normalized_alignment,omitempty"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`
}

type DiffractLLMSpeechStreamChunk struct {
	Type  StreamEventType `json:"type"`
	Audio []byte          `json:"-"`

	Alignment *SpeechAlignment `json:"alignment,omitempty"`

	Usage    Usage             `json:"usage,omitzero"` // Complete only
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"`
}
```

**Billing note.** TTS prices on characters, sometimes on tokens, sometimes on
generated seconds. The adapter sets whichever the model uses:

```go
usage.InputCharacters   = req.InputCharacters()   // NOT len(req.Input)
usage.InputTokens       = ...                     // when reported
usage.OutputSeconds     = ...                     // duration-billed models
```

---

## 8. `kind-transcription.go` — speech to text

```go
package core

type TranscriptionParams struct {
	Language       *string  `json:"language,omitempty"`
	Prompt         *string  `json:"prompt,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	ResponseFormat *string  `json:"response_format,omitempty"` // json|text|srt|vtt|verbose_json|diarized_json
	Granularities  []string `json:"timestamp_granularities,omitempty"` // word, segment
	Include        []string `json:"include,omitempty"`
	FileFormat     *string  `json:"file_format,omitempty"` // optional on openai, required on gemini

	Stream bool `json:"stream,omitempty"`

	// Translate targets /audio/translations rather than /audio/transcriptions.
	Translate bool `json:"translate,omitempty"`
}

// IsPlainText reports whether the requested format produces a non-JSON body.
// The server must not try to parse or re-serialise those.
func (p *TranscriptionParams) IsPlainText() bool {
	if p.ResponseFormat == nil {
		return false
	}
	switch *p.ResponseFormat {
	case "text", "srt", "vtt":
		return true
	}
	return false
}

type DiffractLLMTranscriptionRequest struct {
	Model string `json:"model"`

	File     []byte  `json:"-"` // the audio
	Filename string  `json:"filename,omitempty"` // preserves the format extension
	FileID   *string `json:"file_id,omitempty"`

	Params TranscriptionParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// SegmentKind discriminates the two segment shapes. They are mutually exclusive
// per request and arrive under the same "segments" key upstream.
//
// Bifrost carries both in one struct under one json tag and needs a custom
// MarshalJSON, a custom UnmarshalJSON, and an invented "is_diarized" marker to
// tell them apart on reload - because an empty array decodes as either. Its own
// comment says a mis-decode would "fail or silently corrupt the data". A
// discriminator costs one field and none of that.
type SegmentKind string

const (
	SegmentVerbose  SegmentKind = "verbose"
	SegmentDiarized SegmentKind = "diarized"
)

// TranscriptSegment covers both shapes. Verbose segments have an int id and
// acoustic statistics; diarized segments have a string id and a speaker.
type TranscriptSegment struct {
	Kind SegmentKind `json:"kind"`

	ID    string  `json:"id"` // "3" for verbose, "seg_154" for diarized
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`

	// SegmentDiarized.
	Speaker string `json:"speaker,omitempty"`

	// SegmentVerbose.
	Seek             int     `json:"seek,omitempty"`
	Tokens           []int   `json:"tokens,omitempty"`
	Temperature      float64 `json:"temperature,omitempty"`
	AvgLogProb       float64 `json:"avg_logprob,omitempty"`
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
	NoSpeechProb     float64 `json:"no_speech_prob,omitempty"`
}

type TranscriptWord struct {
	Word    string  `json:"word"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker *string `json:"speaker,omitempty"` // when diarization is on
}

type TranscriptLogProb struct {
	Token   string  `json:"token"`
	LogProb float64 `json:"logprob"`
	Bytes   []int   `json:"bytes,omitempty"`
}

type DiffractLLMTranscriptionResponse struct {
	Text     string   `json:"text"`
	Task     *string  `json:"task,omitempty"`     // "transcribe" | "translate"
	Language *string  `json:"language,omitempty"`

	// Duration in seconds. Also the billing unit for duration-priced models -
	// see the note below.
	Duration *float64 `json:"duration,omitempty"`

	Segments []TranscriptSegment `json:"segments,omitempty"`
	Words    []TranscriptWord    `json:"words,omitempty"`
	LogProbs []TranscriptLogProb `json:"logprobs,omitempty"`

	// ResponseFormat is echoed from the request so the server knows whether to
	// emit JSON or a plain-text body.
	ResponseFormat *string `json:"-"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type DiffractLLMTranscriptionStreamChunk struct {
	Type  StreamEventType `json:"type"`
	Delta *string         `json:"delta,omitempty"`
	Text  string          `json:"text,omitempty"` // full text on Complete

	LogProbs []TranscriptLogProb `json:"logprobs,omitempty"`

	Usage    Usage             `json:"usage,omitzero"`
	Metadata Metadata          `json:"metadata,omitzero"`
	Error    *DiffractLLMError `json:"error,omitempty"`

	Raw []byte `json:"-"`
}
```

**Billing note.** Transcription is priced two ways and the provider says which.
Bifrost models it as `TranscriptionUsage.Type` = `"tokens"` or `"duration"`. We
map straight onto `Usage`:

```go
// duration-billed
usage.InputAudioSeconds = *res.Duration
// token-billed
usage.InputAudioTokens  = ...
usage.InputTokens       = ...   // the text-token half of input_token_details
usage.OutputTokens      = ...
```

If the provider reports neither, the request cannot be priced by duration —
worth logging rather than silently billing zero.

---

## 9. `kind-moderation.go`

```go
package core

type DiffractLLMModerationRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"` // the API accepts a string or an array

	// Images allows multimodal moderation (omni-moderation models).
	Images []ImageRef `json:"images,omitempty"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

// ModerationResult keeps categories as maps rather than a struct per category.
// Providers add categories regularly, and a typed struct would silently drop
// every new one - the failure mode being that a newly-flagged category reads as
// "not flagged".
type ModerationResult struct {
	Index          int                 `json:"index"`
	Flagged        bool                `json:"flagged"`
	Categories     map[string]bool     `json:"categories"`
	CategoryScores map[string]float64  `json:"category_scores"`
	AppliedInput   map[string][]string `json:"applied_input_types,omitempty"` // category -> [text|image]
}

type DiffractLLMModerationResponse struct {
	ID      string             `json:"id"`
	Results []ModerationResult `json:"results"`

	Usage    Usage    `json:"usage,omitzero"`
	Metadata Metadata `json:"metadata,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}
```

Moderation does not stream.

---

## 10. `kind-models.go`

```go
package core

// DiffractLLMModelsRequest has no body; it exists so the descriptor registry has
// a request type for GET /v1/models like every other kind.
type DiffractLLMModelsRequest struct {
	Provider Provider `json:"provider,omitempty"` // empty = every permitted provider
}

// ModelInfo is one entry. Served from the catalog and the virtual key's
// allow-lists, NOT by calling the provider: the gateway already knows which
// models a key may use, and asking upstream returns models the caller cannot
// reach - which then 403 when they try.
type ModelInfo struct {
	ID        string    `json:"id"` // provider/model, as the client addresses it
	Object    string    `json:"object,omitempty"` // "model"
	Provider  Provider  `json:"provider"`
	ModelType ModelType `json:"model_type"`
	OwnedBy   string    `json:"owned_by,omitempty"`
	Created   int64     `json:"created,omitempty"`

	Limits ModelLimits `json:"limits,omitzero"`

	// Capability is a bitfield and does not serialise usefully. Capabilities is
	// the wire form, produced by Capability.String().
	Capability   Capability `json:"-"`
	Capabilities []string   `json:"capabilities,omitempty"`
}

type DiffractLLMModelsResponse struct {
	Object string      `json:"object,omitempty"` // "list"
	Models []ModelInfo `json:"data"`

	Metadata Metadata `json:"metadata,omitzero"`
}
```

---

## 11. What is deliberately absent

**No `Provider` field on any request payload.** Routing identity is
`rctx.Modelkey`, set by the admission hook and changed by the data plane during
weighted resolution. Bifrost puts `Provider ModelProvider` on every request
struct; a second copy goes stale the moment routing moves.

**No `Fallbacks` field.** Bifrost carries fallback targets inside the request.
Here fallback is the data plane's job, driven by virtual-key weights and the
metrics engine, and belongs nowhere near a payload.

**No per-kind usage type.** One `core.Usage`. Bifrost has `BifrostLLMUsage`,
`ImageUsage`, `SpeechUsage`, `TranscriptionUsage` — four types, four sets of
backfill logic, four chances for a field the pricing engine cannot read.

**No `object` on requests, only responses.** It is a constant per endpoint; the
adapter writes it. It is carried on responses only because it varies between the
non-stream and stream forms and a passthrough should echo it.

**No realtime, video, rerank, OCR, vector store, batch or files.** The catalog
has models for several of them and `RequestKind` can grow, but no provider in
scope serves them. The payload types get written when one does.

---

## 12. Summary table

| kind | request | response | stream | notes |
|---|---|---|---|---|
| chat | `DiffractLLMChatRequest` | `DiffractLLMChatResponse` | `DiffractLLMChatStreamChunk` | `ReasoningConfig` shared with responses |
| completion | `DiffractLLMCompletionRequest` | `DiffractLLMCompletionResponse` | `DiffractLLMCompletionStreamChunk` | own params + own logprobs shape |
| responses | `DiffractLLMResponsesRequest` | `DiffractLLMResponsesResponse` | `DiffractLLMResponsesStreamChunk` | items in = items out |
| embedding | `DiffractLLMEmbeddingRequest` | `DiffractLLMEmbeddingResponse` | — | 4 input shapes, 5 output formats |
| image gen/edit/variation | `DiffractLLMImageRequest` | `DiffractLLMImageResponse` | `DiffractLLMImageStreamChunk` | `Selectors()` feeds pricing |
| speech | `DiffractLLMSpeechRequest` | `DiffractLLMSpeechResponse` | `DiffractLLMSpeechStreamChunk` | `InputCharacters()` counts runes |
| transcription | `DiffractLLMTranscriptionRequest` | `DiffractLLMTranscriptionResponse` | `DiffractLLMTranscriptionStreamChunk` | `SegmentKind` discriminator |
| moderation | `DiffractLLMModerationRequest` | `DiffractLLMModerationResponse` | — | categories as maps |
| models | `DiffractLLMModelsRequest` | `DiffractLLMModelsResponse` | — | served from the catalog |

### Head-to-head

| | Bifrost | DiffractLLM |
|---|---|---|
| custom `MarshalJSON`/`UnmarshalJSON` | **22** in the chat schema alone, plus embedding, speech, transcription | **0** |
| union invariants | enforced at marshal time, mid-request | `Validate()` at admission |
| message content | string-or-array union, **1,243** branch sites | `[]ContentPart` |
| usage types | 4 | 1 (`core.Usage`) |
| provider strings in the neutral schema | yes (`web_search_20250305` in `ChatTool.Type`) | none — provider tables only |
| routing identity in payload | `Provider`, `Fallbacks` | on `rctx` |
| `ContentPart` size | — | 88 bytes (136 before `MediaRef`) |

---

## 13. Verification

`internal/core/kind_test.go`.

**Structural — these keep the rules true.**

1. **No type in `core` implements `json.Marshaler` or `json.Unmarshaler`.**
   Reflect over every exported type and assert neither interface is satisfied.
   This is the test that protects R2 and the sonic fast path; it is the rule most
   likely to be broken by a well-meaning future change.
2. **`unsafe.Sizeof(ContentPart{}) <= 88`.** A regression means the media fields
   were re-flattened.
3. **Round trip, every type.** Marshal → unmarshal → `reflect.DeepEqual`,
   including `omitzero` struct fields. A field tagged `omitempty` on a struct
   emits `{}`; this catches it.
4. **`Raw` never serialises.** Marshal a request with `Raw` set; assert no base64
   blob in the output.
5. **One home per concept.** Assert `Message` has no `Refusal` field and that a
   refusal round-trips only through `ContentRefusal`.

**Correctness — these catch the bugs the design exists to prevent.**

6. **`Validate()` catches what marshalling used to.** `EmbeddingInput` with zero
   or two fields set; `Tool{Kind: ToolKindFunction, Server: &ServerTool{}}`. Both
   error at admission, with no upstream call made.
7. **Anthropic reasoning gets a budget.** A chat request with `Reasoning.Effort`
   set and `MaxTokens` nil, routed to Anthropic, has `MaxTokens` filled from
   `catalog.ModelLimits.MaxOutputTokens` before the payload is built.
8. **`ToolCallDelta` reassembly.** Feed the interleaved four-fragment sequence
   from the doc comment and assert two `ToolCall` values with
   `{"city":"Chennai"}` and `{"tz":"IST"}`. Concatenating without honouring
   `Index` produces invalid JSON.
9. **Responses round-trips its own output.** Take a response whose `Output`
   holds a message, a reasoning item with `EncryptedContent`, and a
   `function_call`; feed it back as the next request's `Input` and assert every
   field survives. This is the property the one-type-both-directions decision
   rests on.
10. **Delta targets stay distinct.** A stream interleaving
    `response.output_text.delta` and `response.reasoning_summary_text.delta` for
    one item must reassemble into two separate strings.
11. **Segment kinds do not cross-decode.** A diarized response with **zero**
    segments round-trips as `SegmentDiarized`, not verbose. This is the exact
    case Bifrost needed an invented `is_diarized` marker for.

**Billing — every one of these is money.**

12. **`Usage` reaches pricing.** Build each response type with a populated
    `Usage` and assert `CalculateCost` is non-zero.
13. **`InputCharacters()` counts runes.** `"வணக்கம்"` must not bill as `len()`
    bytes. Assert `InputCharacters() < len(Input)` for non-ASCII input.
14. **Image selectors match the catalog.** `size=1024x1024, quality=low` produces
    the canonical key the catalog stored for `low/1024-x-1024/gpt-image-1`. This
    is the guard against a 15x mispricing.
15. **Served tier wins over requested tier.** A response whose served
    `ServiceTier` differs from the requested one prices against the served one.
16. **Anthropic cache accounting.** `InputTokens = input_tokens +
    cache_read_input_tokens`, `CachedInputTokens = cache_read_input_tokens`,
    `CacheCreationTokens` NOT folded into `InputTokens`. Assert 412 uncached +
    256 cached bills 412 at the full rate and 256 at the read rate — copying
    Anthropic's `input_tokens` straight across undercharges every cache hit.
17. **Server-tool routing filter fails closed.** `tools: [web_search_20250305,
    file_search]` leaves no candidate and returns `CodeUnsupportedTool` at
    admission, with no upstream call. If anyone converts the filter to a drop, a
    request that cannot be served starts returning 200 and this test catches it.

**Benchmarks**, to put a number on R2 once a provider exists:

```
BenchmarkChatRequestMarshal    — 20 messages, 3 tools, sonic
BenchmarkChatRequestUnmarshal  — same payload
BenchmarkContentPartAlloc      — 20-turn conversation, before/after MediaRef
```

The mechanism behind R2 is certain; the magnitude is currently unmeasured and
should not be quoted until it is.
