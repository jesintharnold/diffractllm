# Provider layer — target design

**v2 — 2026-08-25**

Shape adopted from bifrost: shared handlers as free functions, and each provider
writing its own thin `ChatCompletion` that resolves URL + auth and calls them.

---

## Why this shape

Two alternatives were considered and rejected.

**Struct embedding** does not work at all. Go has no virtual dispatch, so a
promoted `ChatCompletion` runs with receiver `*OpenAIProvider` and calls
**OpenAI's** `Endpoint` even from inside Azure. It compiles, and a direct
`az.Endpoint()` test passes, so the failure only shows in production.

**Function fields** work, but bind the dialect at construction. Azure hosts both
OpenAI-family and Claude-family models and must choose **per request** — bifrost
does exactly this (`azure.go:374`, `IsAnthropicModelFamily`). A field set once at
`New()` cannot.

Explicit handlers cost ~30 lines per provider and buy per-request routing,
greppable call sites, and no indirection. Bifrost runs 20+ providers this way.

---

## Decisions

**`patchRaw` is deleted.** `DiffractLLMChatParameters` carries **31 fields** —
OpenAI's whole request surface — so it forwards nothing the struct path loses,
while costing a second untyped path. Every bug in that area lived only there.

**`include_usage` is forced, always.** A client asking for `false` is asking not
to be billed. One line, no merge logic.

**`cred` leaves the transport.** `ServeHTTP` used it for one thing —
`cred.Endpoint`. The provider builds the URL; the transport moves bytes.

**Auth is a provider METHOD, takes a context, returns an error.** Azure SP and
managed-identity fetch an AAD token and cache it per credential, so auth needs
receiver state and can fail.

**One payload builder per kind.** `BuildChatCompletionPayload`, not a generic
`BuildPayload(kind, any)` with a type switch inside.

---

## Request flow

```
CLIENT (openai / anthropic dialect)
  │  POST /v1/chat/completions
  ▼
SERVER  handlers.go
  │  1. Authenticator.Authenticate(rctx)      ← before the body is read
  │  2. read body → rctx.BodyBytes
  │  3. desc.NewRequest() → OpenAIChatCompletionRequest
  │  4. desc.ToDiffract()  → IR                ← dialect leaves here
  ▼
GOVERNANCE  PreCall hooks (budget, model access)
  ▼
DATAPLANE  SelectionEngine.Resolve(rctx)
  │  → rctx.Modelkey.Provider, rctx.SelectedCredential
  ▼
PROVIDER  providers.Get(rctx.Modelkey.Provider).ChatCompletion(rctx, ir, cred)
  │  a. pick the DIALECT for this model      ← azure may route to anthropic
  │  b. build the URL from cred + kind
  │  c. authHeaders(ctx, cred)               ← may fetch a token
  │  d. openai.HandleChatCompletion(rctx, transport, cfg)
  ▼
SHARED HANDLER  openai/handler.go
  │  BuildChatCompletionPayload → transport.ServeHTTP → ParseChatCompletionResponse
  ▼
TRANSPORT  ServeHTTP(rctx, req)               ← no cred
  │  pool · retries · SSRF guard · size cap · stall detector
  ▼
PROVIDER API
  │
  ▼  response bytes
SHARED HANDLER  ParseChatCompletionResponse(status, body) → IR
  ▼
GOVERNANCE  PostCall hooks (usage, cost)
  ▼
SERVER  desc.FromDiffract(ir) → client dialect  ← dialect returns here
  ▼
CLIENT
```

The IR lives between step 4 and `FromDiffract`. Outside those points everything
speaks a wire dialect.

**Streaming** diverges after the transport: the body goes to
`ScanChatCompletionSSE`, which emits IR chunks on a channel, and the handler
writes each through `desc.FromDiffractStream`.

---

## Common — the interface every provider implements

```go
type Provider interface {
    Name() core.Provider

    ChatCompletion(rctx *core.DiffractLLMContext,
        req *core.DiffractLLMChatCompletionRequest, cred *core.Credential)
        (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError)

    ChatCompletionStream(rctx *core.DiffractLLMContext,
        req *core.DiffractLLMChatCompletionRequest, cred *core.Credential)
        (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError)
}
```

One pair per kind as kinds land: `Embedding`, `TextCompletion`, `Responses`,
`Speech`, `Transcription`, `Image`, `Moderation`, `Models`. A provider that does
not serve a kind returns `NewUnsupportedOperation` — that override works fine,
because the handler calls it from outside.

---

## Sub-methods each provider writes for itself

Not in the interface. Unexported, three small functions per provider.

```go
endpoint(cred *core.Credential, kind core.RequestKind) (string, error)
authHeaders(ctx context.Context, cred *core.Credential) (map[string]string, error)
resolveModel(cred *core.Credential, model string) string
```

`authHeaders` is a **method**, not a free function — Azure caches tokens on the
provider struct.

---

## Shared — `internal/providers/openai`, reused verbatim

| function | job |
|---|---|
| `HandleChatCompletion(rctx, transport, cfg)` | build → send → parse, unary |
| `HandleChatCompletionStream(rctx, transport, cfg)` | same, returns an IR chunk channel |
| `BuildChatCompletionPayload(req, model, stream)` | IR → OpenAI wire bytes |
| `ParseChatCompletionResponse(status, body)` | wire bytes → IR |
| `ScanChatCompletionSSE(rctx, body, passthrough)` | SSE body → IR chunk channel |
| `ParseError(status, body)` | error body → `*core.DiffractLLMError` |
| `PathFor(kind)` | the `/v1/...` path, for providers that share it |

Codec pairs, per kind:

| | inbound | outbound |
|---|---|---|
| request | `ToDMChatCompletionRequest` | `ToOpenAIChatCompletionRequest` |
| response | `ToDMChatCompletionResponse` | `ToOpenAIChatCompletionResponse` |
| usage | `ToDMChatCompletionUsage` | `ToOpenAIChatCompletionUsage` |
| stream chunk | `ToDMChatCompletionStreamResponse` | `ToOpenAIChatCompletionStreamResponse` |

---

## `openai/handler.go` — the shared core

```go
// Config, not 14 positional params. Bifrost passes them positionally; at that
// count a struct is the only readable option and costs nothing.
type ChatCompletionConfig struct {
    Provider core.Provider
    URL      string
    Model    string
    Request  *core.DiffractLLMChatCompletionRequest
    Headers  map[string]string

    // Optional. Providers whose error body differs supply their own.
    ParseError func(status int, body []byte) *core.DiffractLLMError
}

func HandleChatCompletion(
    rctx *core.DiffractLLMContext,
    transport *dataplane.DiffractLLMTransport,
    cfg ChatCompletionConfig,
) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {

    body, dErr := BuildChatCompletionPayload(cfg.Request, cfg.Model, false)
    if dErr != nil {
        return nil, dErr
    }

    result, dErr := transport.ServeHTTP(rctx, &dataplane.DiffractLLMTransportRequest{
        Method: http.MethodPost, URL: cfg.URL, Body: body, Headers: cfg.Headers,
    })
    if dErr != nil {
        return nil, dErr
    }

    respBody, err := io.ReadAll(result.Body)
    result.Body.Close()
    if err != nil {
        return nil, core.NewUpstreamError(string(cfg.Provider), cfg.URL,
            result.Status, "reading response", err)
    }

    if result.Status != http.StatusOK {
        if cfg.ParseError != nil {
            return nil, cfg.ParseError(result.Status, respBody)
        }
        return nil, ParseError(result.Status, respBody)
    }
    return ParseChatCompletionResponse(respBody)
}
```

### BuildChatCompletionPayload

```go
// One path, no patchRaw. include_usage is forced because a stream without it is
// unbillable; the caller's other stream_options are left untouched.
func BuildChatCompletionPayload(
    req *core.DiffractLLMChatCompletionRequest, model string, stream bool,
) ([]byte, *core.DiffractLLMError) {

    wire := ToOpenAIChatCompletionRequest(req, model)
    if stream {
        wire.Stream = &stream
        if wire.StreamOptions == nil {
            wire.StreamOptions = &core.ChatStreamOptions{}
        }
        t := true
        wire.StreamOptions.IncludeUsage = &t
    }

    body, err := sonic.Marshal(wire)
    if err != nil {
        return nil, core.NewInternalError("openai", "marshalling chat request", err)
    }
    return body, nil
}
```

The caller's `include_obfuscation` survives free: it arrives in
`req.Parameters.StreamOptions` and only `IncludeUsage` is overwritten.

---

## `openai/openai.go`

```go
type OpenAIProvider struct {
    transport *dataplane.DiffractLLMTransport
    logger    *zap.Logger
}

func New(t *dataplane.DiffractLLMTransport, log *zap.Logger) *OpenAIProvider {
    return &OpenAIProvider{transport: t, logger: log}
}

func (p *OpenAIProvider) Name() core.Provider { return core.ProviderOpenAI }

func (p *OpenAIProvider) ChatCompletion(
    rctx *core.DiffractLLMContext,
    req *core.DiffractLLMChatCompletionRequest,
    cred *core.Credential,
) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {

    url, err := p.endpoint(cred, core.ChatRequest)
    if err != nil {
        return nil, core.NewInternalError("openai", "building url", err)
    }
    headers, err := p.authHeaders(rctx.Context(), cred)
    if err != nil {
        return nil, core.NewUpstreamAuth("openai", url, err.Error())
    }

    return HandleChatCompletion(rctx, p.transport, ChatCompletionConfig{
        Provider: core.ProviderOpenAI,
        URL:      url,
        Model:    p.resolveModel(cred, req.Model),
        Request:  req,
        Headers:  headers,
    })
}

func (p *OpenAIProvider) endpoint(cred *core.Credential, kind core.RequestKind) (string, error) {
    base := strings.TrimRight(cred.Endpoint, "/")
    if base == "" {
        base = "https://api.openai.com"
    }
    path, ok := PathFor(kind)
    if !ok {
        return "", fmt.Errorf("openai: unsupported kind %s", kind)
    }
    return base + path, nil
}

func (p *OpenAIProvider) authHeaders(_ context.Context, cred *core.Credential) (map[string]string, error) {
    if cred.APIKey == "" {
        return nil, errors.New("openai: missing api key")
    }
    return map[string]string{
        "Content-Type":  "application/json",
        "Authorization": "Bearer " + cred.APIKey,
    }, nil
}

func (p *OpenAIProvider) resolveModel(cred *core.Credential, model string) string {
    return cred.CheckModelAlias(model)
}
```

`ChatCompletionStream` is the same four steps, calling
`HandleChatCompletionStream`.

---

## `azure/azure.go`

Azure is the reason for this shape: it serves **two dialects**, chosen per
request.

```go
type AzureProvider struct {
    transport *dataplane.DiffractLLMTransport
    logger    *zap.Logger

    // tenantID:clientID -> cached token. Why authHeaders is a method.
    tokens sync.Map
}

func (p *AzureProvider) Name() core.Provider { return core.ProviderAzure }

func (p *AzureProvider) ChatCompletion(
    rctx *core.DiffractLLMContext,
    req *core.DiffractLLMChatCompletionRequest,
    cred *core.Credential,
) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {

    model := p.resolveModel(cred, req.Model)

    headers, err := p.authHeaders(rctx.Context(), cred)
    if err != nil {
        return nil, core.NewUpstreamAuth("azure", cred.Endpoint, err.Error())
    }

    // PER-REQUEST dialect switch. Claude on Azure speaks Anthropic's wire, not
    // OpenAI's - this is what a construction-time seam cannot express.
    if isAnthropicFamily(model) {
        url, err := p.anthropicEndpoint(cred)
        if err != nil {
            return nil, core.NewInternalError("azure", "building url", err)
        }
        headers["anthropic-version"] = anthropicVersion(cred)
        return anthropic.HandleChatCompletion(rctx, p.transport, anthropic.ChatCompletionConfig{
            Provider: core.ProviderAzure, URL: url, Model: model,
            Request: req, Headers: headers,
        })
    }

    url, err := p.endpoint(cred, core.ChatRequest, model)
    if err != nil {
        return nil, core.NewInternalError("azure", "building url", err)
    }
    return openai.HandleChatCompletion(rctx, p.transport, openai.ChatCompletionConfig{
        Provider: core.ProviderAzure, URL: url, Model: model,
        Request: req, Headers: headers,
    })
}

// {endpoint}/openai/deployments/{deployment}/chat/completions?api-version=...
// The deployment is in the PATH, so the model must be resolved first.
func (p *AzureProvider) endpoint(cred *core.Credential, kind core.RequestKind, model string) (string, error) {
    az := cred.Settings.Azure
    if az == nil || az.APIVersion == "" {
        return "", errors.New("azure: api_version is required")
    }
    path, ok := openai.PathFor(kind)
    if !ok {
        return "", fmt.Errorf("azure: unsupported kind %s", kind)
    }
    return fmt.Sprintf("%s/openai/deployments/%s%s?api-version=%s",
        strings.TrimRight(cred.Endpoint, "/"), model, path, az.APIVersion), nil
}

// Key mode is a header. SP and managed identity call AAD, so this can fail and
// the token is cached on p.tokens.
func (p *AzureProvider) authHeaders(ctx context.Context, cred *core.Credential) (map[string]string, error) {
    az := cred.Settings.Azure
    if az == nil {
        return nil, errors.New("azure: settings are required")
    }
    h := map[string]string{"Content-Type": "application/json"}

    switch az.AuthMode {
    case core.AzureAuthKeyMode:
        h["api-key"] = cred.APIKey
    case core.AzureAuthServicePrincipal, core.AzureManagedIdentity:
        tok, err := p.token(ctx, az)
        if err != nil {
            return nil, err
        }
        h["Authorization"] = "Bearer " + tok
    default:
        return nil, fmt.Errorf("azure: unknown auth_mode %q", az.AuthMode)
    }
    return h, nil
}

// Azure deployments are named by the operator, so the alias map IS the
// model-to-deployment mapping.
func (p *AzureProvider) resolveModel(cred *core.Credential, model string) string {
    return cred.CheckModelAlias(model)
}
```

---

## `vllm/vllm.go`

vLLM serves the OpenAI dialect verbatim. Only the endpoint and auth differ.

```go
type VLLMProvider struct {
    transport *dataplane.DiffractLLMTransport
    logger    *zap.Logger
}

func (p *VLLMProvider) Name() core.Provider { return core.ProviderVLLM }

func (p *VLLMProvider) ChatCompletion(
    rctx *core.DiffractLLMContext,
    req *core.DiffractLLMChatCompletionRequest,
    cred *core.Credential,
) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {

    url, err := p.endpoint(cred, core.ChatRequest)
    if err != nil {
        return nil, core.NewInternalError("vllm", "building url", err)
    }
    headers, _ := p.authHeaders(rctx.Context(), cred)

    return openai.HandleChatCompletion(rctx, p.transport, openai.ChatCompletionConfig{
        Provider: core.ProviderVLLM,
        URL:      url,
        Model:    cred.CheckModelAlias(req.Model),
        Request:  req,
        Headers:  headers,
    })
}

// No default host - a self-hosted endpoint has no well-known address.
func (p *VLLMProvider) endpoint(cred *core.Credential, kind core.RequestKind) (string, error) {
    if cred.Endpoint == "" {
        return "", errors.New("vllm: endpoint is required")
    }
    path, ok := openai.PathFor(kind)
    if !ok {
        return "", fmt.Errorf("vllm: unsupported kind %s", kind)
    }
    return strings.TrimRight(cred.Endpoint, "/") + path, nil
}

// Usually unauthenticated behind a private network.
func (p *VLLMProvider) authHeaders(_ context.Context, cred *core.Credential) (map[string]string, error) {
    h := map[string]string{"Content-Type": "application/json"}
    if cred.APIKey != "" {
        h["Authorization"] = "Bearer " + cred.APIKey
    }
    return h, nil
}
```

A self-hosted vLLM on `10.x` needs `allow_private_network: true` on its upstream,
or the transport's SSRF guard refuses the dial.

---

## What each provider costs

| provider | lines | what differs |
|---|---|---|
| openai | baseline | — |
| vllm | ~40 | endpoint required, optional auth |
| ollama | ~40 | same shape as vllm |
| azure | ~140 | URL shape, 3 auth modes, token cache, dual dialect |
| anthropic | full | own dialect, own handlers, same interface |

---

## Transport change

```go
type DiffractLLMTransportRequest struct {
    Method      string
    URL         string   // ABSOLUTE - was Path, resolved against cred.Endpoint
    Body        []byte
    Headers     map[string]string
    IsStreaming bool
}

func (t *DiffractLLMTransport) ServeHTTP(
    rctx *core.DiffractLLMContext, req *DiffractLLMTransportRequest,
) (*DiffractLLMTransportResult, *core.DiffractLLMError)
```

`internal/dataplane` stops importing anything credential-shaped.

---

## Prerequisites

- `core.ProviderVLLM` does not exist. Add the constant and the
  `supportedProviders` entry.
- `providers.Provider.ChatCompletion` currently declares **no return values** —
  compiles, cannot be implemented.
- `providers.Get` returns `(*Provider, error)` — a pointer to an interface.

## Build order

1. `DiffractLLMTransportRequest.URL`; drop `cred` from `ServeHTTP`
2. Fix `providers.Provider` and `Get`
3. `openai/types.go`, `openai/chat-completion.go` — codec; **delete `patchRaw`**
4. `openai/stream.go`, `openai/errors.go`
5. `openai/handler.go` — the shared handlers
6. `openai/openai.go` — the provider
7. `vllm/`, then `azure/`

## Verification

- `grep -r "core.Credential" internal/dataplane/` → no hits
- `grep -rn "include_usage" internal/providers/` → one hit
- table test: openai, azure and vllm produce three different URLs and three
  different auth headers from one `BuildChatCompletionPayload` output
- azure key mode sets `api-key`; SP mode calls the token func **once** across two
  requests on the same credential
- azure with a Claude model routes to the anthropic handler, not the openai one
- a stub provider implementing only `ChatCompletion` compiles
