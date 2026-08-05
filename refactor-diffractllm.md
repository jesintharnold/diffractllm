# DiffractLLM — Model Catalog & Price Catalog Implementation

**Scope:** the model catalog and the price catalog — feed ingestion, storage, in-memory
snapshot, and price resolution on the request path.

**Source feed:** `https://getbifrost.ai/datasheet` — 3,708 records, fetched 2026-08-05,
SHA-256 `d87b4a716f87d5014be9939585faf6357d0893796164aa5d4e9a7d5e2f073992`.

---

## 1. The problem being solved

The feed encodes priced variants as leading path segments on the model key. One model, several
priced rows:

```json
"1024-x-1024/50-steps/stability.stable-diffusion-xl-v1":  { "output_cost_per_image": 0.04, ... },
"1024-x-1024/max-steps/stability.stable-diffusion-xl-v1": { "output_cost_per_image": 0.08, ... },
"256-x-256/dall-e-2":   { "input_cost_per_pixel": 2.4414e-7, ... },
"512-x-512/dall-e-2":   { "input_cost_per_pixel": 6.86e-8,   ... },
"1024-x-1024/dall-e-2": { "input_cost_per_pixel": 1.9e-8,    ... },
"dall-e-2":             { "input_cost_per_image": 0.02,      ... }
```

Our current pricing table is unique on `(provider_id, model_name)`. All four `dall-e-2` rows
target the same key, so three are overwritten during sync and the survivor is whichever the
map ranged last. The prices differ by **12.8×**.

The fix is that **the model catalog and the price catalog have different cardinality and must
not share a key.**

```
model catalog    3617 rows    one row per model              "what is this model"
price catalog    3699 rows    one row per priced variant     "what does it cost"
```

Measured against the live feed with the parser in §4: 3,699 model rows produce 3,699 distinct
price keys with **zero collisions**, and 3,617 distinct metadata keys.

---

## 2. Files changed

```text
internal/core/catalog.go            NEW   catalog keys, selectors, metadata, variants
internal/core/ratecard.go           NEW   meters, rate cards, Money, cost calculation
internal/core/capability.go         EDIT  SupportsAll; fix Has
internal/core/modelcatalog.go       EDIT  ModelMetaData -> ModelMetadata, mode-aware

internal/modelcatalog/parser.go     NEW   feed key decomposition, provider aliases
internal/modelcatalog/selectors.go  NEW   request-side selector extraction per mode
internal/modelcatalog/source.go     EDIT  fix the interface, add content digest
internal/modelcatalog/source_datasheet.go  NEW   datasheet decode + rate normalization
internal/modelcatalog/snapshot.go   NEW   immutable snapshot + builder
internal/modelcatalog/modelcatalog.go EDIT one atomic pointer, ResolveCatalogRequest
internal/modelcatalog/sync.go       EDIT  validate before persist, one generation txn

internal/dbstore/model_metadata.go  EDIT  mode in key, source ownership
internal/dbstore/pricing.go         EDIT  variant rows, selector key, rate card
internal/dbstore/provider.go        EDIT  unique name, IsConfigured
internal/dbstore/migrations/        NEW   versioned migration, not AutoMigrate
```

---

## 3. Core types

### 3.1 Keys

`internal/core/catalog.go`

```go
package core

// ModelKey stays as-is for routing and virtual-key authorization.
type ModelKey struct {
	Provider  Provider
	ModelName string
}

// CatalogKey identifies a model in the model catalog. Mode is part of the key
// because the same model name is published under more than one mode with
// different limits and capabilities.
type CatalogKey struct {
	Provider  Provider
	ModelName string
	Mode      ModelType
}

// PriceKey identifies a row in the price catalog. It is CatalogKey plus the
// canonical selector string. This extra dimension is what lets four dall-e-2
// price rows coexist under one dall-e-2 model row.
type PriceKey struct {
	CatalogKey
	SelectorKey string
}

// SourceRowKey is the storage identity of an imported feed row. It is unique
// and never derived from parsing, so no feed row can be lost or overwritten
// regardless of how the parser evolves.
type SourceRowKey struct {
	Source string
	RawKey string
}
```

### 3.2 Selectors

Selectors are generic name/value pairs, not columns. Adding video resolution, audio duration,
or an OCR page class later requires no migration.

```go
type Selector struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SelectorSet is canonical: Values is sorted by name, and Key is a compact JSON
// object built from that sorted order. Two SelectorSets with the same contents
// always produce byte-identical Keys, which makes Key usable as both a map key
// and an indexed TEXT column.
type SelectorSet struct {
	Values []Selector `json:"values,omitempty"`
	Key    string     `json:"key"`
}

const EmptySelectorKey = "{}"

func NewSelectorSet(values map[string]string) (SelectorSet, error) {
	if len(values) == 0 {
		return SelectorSet{Key: EmptySelectorKey}, nil
	}

	names := make([]string, 0, len(values))
	for name, value := range values {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
			return SelectorSet{}, fmt.Errorf("selector name and value are required")
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var key strings.Builder
	key.WriteByte('{')
	selectors := make([]Selector, 0, len(names))
	for i, name := range names {
		if i > 0 {
			key.WriteByte(',')
		}
		encodedName, _ := json.Marshal(name)
		encodedValue, _ := json.Marshal(values[name])
		key.Write(encodedName)
		key.WriteByte(':')
		key.Write(encodedValue)
		selectors = append(selectors, Selector{Name: name, Value: values[name]})
	}
	key.WriteByte('}')

	return SelectorSet{Values: selectors, Key: key.String()}, nil
}

func (s SelectorSet) IsEmpty() bool {
	return len(s.Values) == 0
}
```

Canonical forms:

```text
{}
{"size":"512x512"}
{"quality":"hd","size":"1024x1792"}
{"size":"1024x1024","steps":"50"}
```

### 3.3 Metadata and variants

```go
type ModelLimits struct {
	ContextWindow        int32 `json:"context_window,omitempty"`
	MaxInputTokens       int32 `json:"max_input_tokens,omitempty"`
	MaxOutputTokens      int32 `json:"max_output_tokens,omitempty"`
	LongContextThreshold int32 `json:"long_context_threshold,omitempty"`
}

// ModelMetadata replaces ModelMetaData. One row per (provider, model, mode).
type ModelMetadata struct {
	ID           string
	Key          CatalogKey
	BaseModel    string
	Capability   Capability
	Limits       ModelLimits
	Source       string
	SourceRawKey string
}

func (m *ModelMetadata) ModelKey() ModelKey {
	return ModelKey{Provider: m.Key.Provider, ModelName: m.Key.ModelName}
}

// PricingVariant is one priced row. RawKey is the untouched feed key, kept so
// every imported row stays addressable no matter what the parser produced.
type PricingVariant struct {
	ID        string
	Source    string
	RawKey    string
	Key       PriceKey
	Selectors SelectorSet
	RateCard  RateCard
	Status    RateStatus
	Limits    ModelLimits
}
```

### 3.4 Capability fix

`internal/core/capability.go`

```go
// Has reports whether ANY of f is present. Kept for display and filtering.
func (c Capability) Has(f Capability) bool { return c&f != 0 }

// SupportsAll reports whether EVERY bit in required is present. Request
// validation must use this. A vision-only model must not pass a check for
// CapVision|CapFunctionCalling.
func (c Capability) SupportsAll(required Capability) bool { return c&required == required }
```

`ModelMetaData.Supports` currently calls `Has`, which accepts a model that supports only one of
several required capabilities. Every validation caller moves to `SupportsAll`.

---

## 4. Rate cards

Pricing is normalized into meters instead of one column per feed field. This is what makes the
price catalog extensible to audio, video, and OCR without schema changes.

`internal/core/ratecard.go`

```go
type Meter string

const (
	MeterInputToken       Meter = "input_token"
	MeterCachedInputToken Meter = "cached_input_token"
	MeterOutputToken      Meter = "output_token"
	MeterInputCharacter   Meter = "input_character"
	MeterInputAudioSecond Meter = "input_audio_second"
	MeterInputAudioToken  Meter = "input_audio_token"
	MeterOutputAudioToken Meter = "output_audio_token"
	MeterInputImage       Meter = "input_image"
	MeterOutputImage      Meter = "output_image"
	MeterGeneratedPixel   Meter = "generated_pixel"
	MeterPage             Meter = "page"
)

type RateTerm struct {
	Meter       Meter  `json:"meter"`
	PriceUSD    string `json:"price_usd"`    // exact decimal string, never float64
	SourceField string `json:"source_field"` // audit trail back to the feed field
}

type RateCard struct {
	Terms []RateTerm `json:"terms"` // sorted by Meter
}

type RateStatus string

const (
	RatePriced      RateStatus = "priced"
	RateExplicitFree RateStatus = "explicit_free"
	RateUnpriced    RateStatus = "unpriced" // semantics not understood; must not bill
)
```

Prices are stored as decimal **strings** and parsed with `shopspring/decimal`. `float64` cannot
represent `1.9e-08` exactly, and image costs reach eleven decimal places.

---

## 5. Feed parser

`internal/modelcatalog/parser.go`

### 5.1 Provider aliases

Exact map, never substring matching — substring folding silently captures any future provider
whose name contains an existing one.

```go
var providerAliases = map[string]core.Provider{
	"azure_text":                    "azure",
	"fireworks_ai-embedding-models": "fireworks_ai",
	"text-completion-openai":        "openai",
	"text-completion-codestral":     "mistral",
	"text-completion-inception":     "inception",
	"amazon_nova":                   "amazon-nova",
}

func canonicalProvider(raw string) core.Provider {
	if provider, ok := providerAliases[raw]; ok {
		return provider
	}
	return core.Provider(raw)
}
```

### 5.2 Selector grammar

Only these three shapes are recognized as variant markers. Everything else stays in the model
name.

```go
var (
	reSizeMarker  = regexp.MustCompile(`^(\d+|max)-x-(\d+|max)$`)
	reStepsMarker = regexp.MustCompile(`^(\d+|max)-steps$`)

	qualityMarkers = map[string]struct{}{
		"low": {}, "medium": {}, "high": {},
		"standard": {}, "hd": {}, "auto": {},
	}
)

// isQualityMarker requires the NEXT segment to be a size marker. Quality words
// are ordinary English, so without that guard a real model named "standard/foo"
// would be silently truncated to "foo".
func isQualityMarker(parts []string) bool {
	if len(parts) < 3 {
		return false
	}
	if _, ok := qualityMarkers[parts[0]]; !ok {
		return false
	}
	return reSizeMarker.MatchString(parts[1])
}
```

### 5.3 Feed key decomposition

```go
type ParsedFeedKey struct {
	Provider  core.Provider
	ModelName string
	Selectors core.SelectorSet
}

// ParseFeedKey splits a raw datasheet key into the provider, the client-facing
// model name, and the priced selectors.
//
//	1024-x-1024/50-steps/stability.stable-diffusion-xl-v1
//	  -> bedrock, stability.stable-diffusion-xl-v1, {"size":"1024x1024","steps":"50"}
//
// A leading segment is removed only when it exactly equals the row's provider.
// Selector markers are then consumed from the front while they match the strict
// grammar above; the first non-marker segment ends the walk. 3,176 of 3,708 feed
// keys contain a slash and most of those slashes are part of the real model name
// (groq/openai/gpt-oss-120b), so stripping by position rather than by pattern
// would corrupt roughly 1,250 names.
func ParseFeedKey(rawKey, rawProvider string, mode core.ModelType) (ParsedFeedKey, error) {
	rawKey = strings.TrimSpace(rawKey)
	rawProvider = strings.TrimSpace(rawProvider)
	if rawKey == "" || rawProvider == "" {
		return ParsedFeedKey{}, fmt.Errorf("raw key and provider are required")
	}
	if mode == core.ModelTypeUnknown {
		return ParsedFeedKey{}, fmt.Errorf("mode is required for %q", rawKey)
	}

	provider := canonicalProvider(rawProvider)
	parts := strings.Split(rawKey, "/")

	if len(parts) > 1 && (parts[0] == rawProvider || parts[0] == string(provider)) {
		parts = parts[1:]
	}

	values := make(map[string]string, 3)
	consumed := false

	if mode == core.ModelTypeImageGeneration {
	walk:
		for len(parts) > 1 {
			head := parts[0]
			switch {
			case reSizeMarker.MatchString(head):
				if _, exists := values["size"]; exists {
					return ParsedFeedKey{}, fmt.Errorf("duplicate size in %q", rawKey)
				}
				match := reSizeMarker.FindStringSubmatch(head)
				values["size"] = match[1] + "x" + match[2]

			case reStepsMarker.MatchString(head):
				if _, exists := values["steps"]; exists {
					return ParsedFeedKey{}, fmt.Errorf("duplicate steps in %q", rawKey)
				}
				values["steps"] = reStepsMarker.FindStringSubmatch(head)[1]

			case isQualityMarker(parts):
				if _, exists := values["quality"]; exists {
					return ParsedFeedKey{}, fmt.Errorf("duplicate quality in %q", rawKey)
				}
				values["quality"] = head

			default:
				break walk
			}
			parts = parts[1:]
			consumed = true
		}
	}

	// A handful of feed keys repeat the provider AFTER the selectors:
	//   1024-x-1024/50-steps/bedrock/amazon.nova-canvas-v1:0
	// Only that exact shape is stripped. This is not a general
	// "remove anything that looks like a provider" rule.
	if consumed && len(parts) >= 2 && parts[0] == string(provider) {
		parts = parts[1:]
	}

	modelName := strings.Join(parts, "/")
	if modelName == "" {
		return ParsedFeedKey{}, fmt.Errorf("empty model name in %q", rawKey)
	}

	selectors, err := core.NewSelectorSet(values)
	if err != nil {
		return ParsedFeedKey{}, err
	}
	return ParsedFeedKey{Provider: provider, ModelName: modelName, Selectors: selectors}, nil
}
```

Verified output on the live feed:

| Raw key | Provider | Model name | Selectors |
|---|---|---|---|
| `gpt-4o` | openai | `gpt-4o` | `{}` |
| `512-x-512/dall-e-2` | openai | `dall-e-2` | `{"size":"512x512"}` |
| `azure/hd/1024-x-1792/dall-e-3` | azure | `dall-e-3` | `{"quality":"hd","size":"1024x1792"}` |
| `1024-x-1024/50-steps/stability.stable-diffusion-xl-v1` | bedrock | `stability.stable-diffusion-xl-v1` | `{"size":"1024x1024","steps":"50"}` |
| `1024-x-1024/50-steps/bedrock/amazon.nova-canvas-v1:0` | bedrock | `amazon.nova-canvas-v1:0` | `{"size":"1024x1024","steps":"50"}` |
| `groq/openai/gpt-oss-120b` | groq | `openai/gpt-oss-120b` | `{}` |
| `azure/eu/gpt-4o-2024-08-06` | azure | `eu/gpt-4o-2024-08-06` | `{}` |

### 5.4 Rows that are not models

Nine feed records carry no `mode` and are not models:

```text
fallback_generalizations              regex routing rules
fireworks-ai-up-to-4b                 parameter-band pricing tier
fireworks-ai-4.1b-to-16b              parameter-band pricing tier
fireworks-ai-above-16b                parameter-band pricing tier
fireworks-ai-moe-up-to-56b            parameter-band pricing tier
fireworks-ai-56b-to-176b              parameter-band pricing tier
fireworks-ai-default                  parameter-band pricing tier
fireworks-ai-embedding-up-to-150m     parameter-band pricing tier
fireworks-ai-embedding-150m-to-350m   parameter-band pricing tier
```

They are classified as **control rows**: counted, logged, not imported as models. All 294 real
`fireworks_ai` models carry their own price, so no billable coverage is lost.

Every record must be classified. The import report asserts:

```text
input_count = model_count + control_count + rejected_count
```

An unclassified record fails the sync rather than being dropped.

---

## 6. Request-side selectors

`internal/modelcatalog/selectors.go`

The request side must produce **byte-identical** canonical keys to the feed side, because a
price lookup is an exact string match.

```go
type SelectorExtractor func(body []byte) (core.SelectorSet, error)

type SelectorRegistry struct {
	byMode map[core.ModelType]SelectorExtractor
}

func NewSelectorRegistry() *SelectorRegistry {
	return &SelectorRegistry{
		byMode: map[core.ModelType]SelectorExtractor{
			core.ModelTypeImageGeneration: extractImageSelectors,
		},
	}
}

// Extract returns an empty selector set for modes with no priced variants.
// Adding video or audio variants later means registering one extractor here.
func (r *SelectorRegistry) Extract(mode core.ModelType, body []byte) (core.SelectorSet, error) {
	if extractor, ok := r.byMode[mode]; ok {
		return extractor(body)
	}
	return core.NewSelectorSet(nil)
}

var reRequestSize = regexp.MustCompile(`^(\d+|max)x(\d+|max)$`)

type imageSelectorPayload struct {
	Size    string          `json:"size"`
	Quality string          `json:"quality"`
	Steps   json.RawMessage `json:"steps"`
}

func extractImageSelectors(body []byte) (core.SelectorSet, error) {
	var payload imageSelectorPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return core.SelectorSet{}, fmt.Errorf("decode image selectors: %w", err)
	}

	values := make(map[string]string, 3)
	if payload.Size != "" {
		size := strings.ToLower(strings.TrimSpace(payload.Size))
		if !reRequestSize.MatchString(size) {
			return core.SelectorSet{}, fmt.Errorf("invalid size %q", payload.Size)
		}
		values["size"] = size
	}
	if payload.Quality != "" {
		quality := strings.ToLower(strings.TrimSpace(payload.Quality))
		if _, ok := qualityMarkers[quality]; !ok {
			return core.SelectorSet{}, fmt.Errorf("invalid quality %q", payload.Quality)
		}
		values["quality"] = quality
	}
	if steps, ok, err := normalizeRequestSteps(payload.Steps); err != nil {
		return core.SelectorSet{}, err
	} else if ok {
		values["steps"] = steps
	}
	return core.NewSelectorSet(values)
}

// normalizeRequestSteps accepts 50 and "50" and "max". The feed writes steps as
// a bare string, so both JSON forms must converge on the same canonical value.
func normalizeRequestSteps(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}
	value := string(raw)
	if raw[0] == '"' {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return "", false, fmt.Errorf("decode steps: %w", err)
		}
		value = strings.ToLower(strings.TrimSpace(decoded))
	}
	if value == "max" || isPositiveInteger(value) {
		return value, true, nil
	}
	return "", false, fmt.Errorf("steps must be a positive integer or max")
}

func isPositiveInteger(value string) bool {
	if value == "" || value == "0" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
```

Note the feed writes `50-steps` (string) while a client sends `"steps": 50` (number). Both
normalize to `"50"`, so the canonical keys match.

---

## 7. Database schema

`internal/dbstore/`

### 7.1 Provider

```go
type StoreProvider struct {
	ID           string `gorm:"primaryKey;type:text"                         json:"id"`
	Name         string `gorm:"not null;type:text;uniqueIndex:uq_provider_name" json:"name"`
	IsConfigured bool   `gorm:"not null;default:false"                       json:"is_configured"`
}
```

`Name` has no unique index today, so `UpsertProviders` has no `ON CONFLICT` target. This must
land before any sync work.

### 7.2 Model catalog

```go
type StoreModelMetadata struct {
	ID string `gorm:"primaryKey;type:text" json:"id"`

	Source string `gorm:"not null;type:text;uniqueIndex:uq_metadata,priority:1" json:"source"`

	ProviderID string        `gorm:"not null;type:text;uniqueIndex:uq_metadata,priority:2;index:ix_metadata_lookup,priority:1" json:"provider_id"`
	Provider   StoreProvider `gorm:"foreignKey:ProviderID;references:ID"                                                       json:"provider"`

	ModelName string `gorm:"not null;type:text;uniqueIndex:uq_metadata,priority:3;index:ix_metadata_lookup,priority:2" json:"model_name"`
	Mode      string `gorm:"not null;type:text;uniqueIndex:uq_metadata,priority:4;index:ix_metadata_lookup,priority:3" json:"mode"`
	BaseModel string `gorm:"type:text"                                                                                json:"base_model"`

	Capabilities []string         `gorm:"serializer:json;type:text;not null" json:"capabilities"`
	Limits       core.ModelLimits `gorm:"serializer:json;type:text;not null" json:"limits"`

	SourceRawKey  string `gorm:"not null;type:text" json:"source_raw_key"`
	SyncGeneration string `gorm:"not null;type:text" json:"sync_generation"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (StoreModelMetadata) TableName() string { return "model_metadata" }
```

```text
UNIQUE uq_metadata(source, provider_id, model_name, mode)
INDEX  ix_metadata_lookup(provider_id, model_name, mode)
```

`source` is in the unique index so two feeds cannot overwrite each other.

**Capabilities are stored as names, never bit positions.** The bit values are an implementation
detail; persisting integers would make reordering the `iota` block silently rewrite history.

### 7.3 Price catalog

```go
type StoreModelPricing struct {
	ID string `gorm:"primaryKey;type:text" json:"id"`

	// Storage identity. Unique, and never derived from parsing, so no imported
	// row can be lost regardless of how the parser changes.
	Source string `gorm:"not null;type:text;uniqueIndex:uq_pricing_source_row,priority:1" json:"source"`
	RawKey string `gorm:"not null;type:text;uniqueIndex:uq_pricing_source_row,priority:2" json:"raw_key"`

	// Lookup identity. Deliberately NOT unique.
	ProviderID string        `gorm:"not null;type:text;index:ix_pricing_lookup,priority:1" json:"provider_id"`
	Provider   StoreProvider `gorm:"foreignKey:ProviderID;references:ID"                    json:"provider"`
	ModelName  string        `gorm:"not null;type:text;index:ix_pricing_lookup,priority:2" json:"model_name"`
	Mode       string        `gorm:"not null;type:text;index:ix_pricing_lookup,priority:3" json:"mode"`

	SelectorKey string           `gorm:"not null;type:text;default:'{}';index:ix_pricing_lookup,priority:4" json:"selector_key"`
	Selectors   core.SelectorSet `gorm:"serializer:json;type:text;not null"                                 json:"selectors"`

	RateCard core.RateCard   `gorm:"serializer:json;type:text;not null" json:"rate_card"`
	Status   core.RateStatus `gorm:"not null;type:text"                 json:"status"`
	Limits   core.ModelLimits `gorm:"serializer:json;type:text;not null" json:"limits"`

	SyncGeneration string `gorm:"not null;type:text" json:"sync_generation"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (StoreModelPricing) TableName() string { return "model_pricing" }
```

```text
UNIQUE uq_pricing_source_row(source, raw_key)
INDEX  ix_pricing_lookup(provider_id, model_name, mode, selector_key)
```

**The lookup index is intentionally not unique.** If two rows ever project to the same lookup
key, both stay in the table and the conflict is raised at snapshot build. A unique index would
force one to be discarded at write time, which is the failure we are removing.

`selector_key` is `NOT NULL DEFAULT '{}'` because NULL is DISTINCT from NULL in unique indexes
on both SQLite and PostgreSQL — a nullable variant column would silently permit duplicates.

Rows in `model_pricing` for a single model:

```text
provider_id  model_name  mode              selector_key                        rate_card
openai       dall-e-2    image_generation  {}                                  input_image 0.02
openai       dall-e-2    image_generation  {"size":"256x256"}                  generated_pixel 2.4414e-7
openai       dall-e-2    image_generation  {"size":"512x512"}                  generated_pixel 6.86e-8
openai       dall-e-2    image_generation  {"size":"1024x1024"}                generated_pixel 1.9e-8
```

Four price rows, one metadata row. That is the cardinality difference §1 describes.

---

## 8. Datasheet source adapter

`internal/modelcatalog/source_datasheet.go` — the only place the datasheet's field names appear.

```go
const SourceDatasheet = "bifrost-datasheet"

// datasheetEntry mirrors one record. Every numeric field is a pointer so an
// explicit 0 stays distinguishable from an absent field.
type datasheetEntry struct {
	Mode      string `json:"mode"`
	Provider  string `json:"provider"`
	BaseModel string `json:"base_model"`

	MaxInputTokens  *int32 `json:"max_input_tokens"`
	MaxOutputTokens *int32 `json:"max_output_tokens"`
	MaxTokens       *int32 `json:"max_tokens"`

	InputCostPerToken       *json.Number `json:"input_cost_per_token"`
	OutputCostPerToken      *json.Number `json:"output_cost_per_token"`
	CacheReadInputTokenCost *json.Number `json:"cache_read_input_token_cost"`
	InputCostPerCharacter   *json.Number `json:"input_cost_per_character"`
	InputCostPerAudioToken  *json.Number `json:"input_cost_per_audio_token"`
	OutputCostPerAudioToken *json.Number `json:"output_cost_per_audio_token"`
	InputCostPerImage       *json.Number `json:"input_cost_per_image"`
	OutputCostPerImage      *json.Number `json:"output_cost_per_image"`
	InputCostPerPixel       *json.Number `json:"input_cost_per_pixel"`

	SupportsFunctionCalling *bool `json:"supports_function_calling"`
	SupportsVision          *bool `json:"supports_vision"`
	SupportsPromptCaching   *bool `json:"supports_prompt_caching"`
	SupportsResponseSchema  *bool `json:"supports_response_schema"`
	SupportsToolChoice      *bool `json:"supports_tool_choice"`
	SupportsPDFInput        *bool `json:"supports_pdf_input"`
	SupportsReasoning       *bool `json:"supports_reasoning"`
	SupportsSystemMessages  *bool `json:"supports_system_messages"`
	SupportsWebSearch       *bool `json:"supports_web_search"`
	SupportsAssistantPrefill *bool `json:"supports_assistant_prefill"`
}

// json.Number keeps the exact decimal text from the wire. Decoding 1.9e-08 into
// a float64 and formatting it back would not round-trip.
func normalizeRates(rawKey string, mode core.ModelType, entry datasheetEntry) (core.RateCard, core.RateStatus) {
	terms := make([]core.RateTerm, 0, 4)
	add := func(meter core.Meter, value *json.Number, field string) {
		if value == nil {
			return
		}
		terms = append(terms, core.RateTerm{Meter: meter, PriceUSD: value.String(), SourceField: field})
	}

	add(core.MeterInputToken, entry.InputCostPerToken, "input_cost_per_token")
	add(core.MeterOutputToken, entry.OutputCostPerToken, "output_cost_per_token")
	add(core.MeterCachedInputToken, entry.CacheReadInputTokenCost, "cache_read_input_token_cost")
	add(core.MeterInputCharacter, entry.InputCostPerCharacter, "input_cost_per_character")
	add(core.MeterInputAudioToken, entry.InputCostPerAudioToken, "input_cost_per_audio_token")
	add(core.MeterOutputAudioToken, entry.OutputCostPerAudioToken, "output_cost_per_audio_token")
	add(core.MeterInputImage, entry.InputCostPerImage, "input_cost_per_image")
	add(core.MeterOutputImage, entry.OutputCostPerImage, "output_cost_per_image")

	// input_cost_per_pixel is NOT an input meter. On image rows the published
	// value is a flat per-image price divided by that variant's pixel count, so
	// it meters GENERATED pixels. Because that reading is inferred rather than
	// stated by the feed, a row is billable only after it appears in the
	// reviewed allowlist. Everything else is marked unpriced and rejected at
	// request time instead of billed on a guess.
	if entry.InputCostPerPixel != nil {
		if mode != core.ModelTypeImageGeneration {
			return core.RateCard{}, core.RateUnpriced
		}
		if _, approved := reviewedGeneratedPixelRows[rawKey]; !approved {
			return core.RateCard{}, core.RateUnpriced
		}
		add(core.MeterGeneratedPixel, entry.InputCostPerPixel, "input_cost_per_pixel")
	}

	if len(terms) == 0 {
		return core.RateCard{}, core.RateUnpriced
	}
	slices.SortFunc(terms, func(a, b core.RateTerm) int {
		return strings.Compare(string(a.Meter), string(b.Meter))
	})
	return core.RateCard{Terms: terms}, core.RatePriced
}

func normalizeCapabilities(entry datasheetEntry) core.Capability {
	var capability core.Capability
	set := func(flag *bool, bit core.Capability) {
		if flag != nil && *flag {
			capability |= bit
		}
	}
	set(entry.SupportsFunctionCalling, core.CapFunctionCalling)
	set(entry.SupportsVision, core.CapVision)
	set(entry.SupportsPromptCaching, core.CapPromptCaching)
	set(entry.SupportsResponseSchema, core.CapResponseSchema)
	set(entry.SupportsToolChoice, core.CapToolChoice)
	set(entry.SupportsPDFInput, core.CapPDFInput)
	set(entry.SupportsReasoning, core.CapReasoning)
	set(entry.SupportsSystemMessages, core.CapSystemMessages)
	set(entry.SupportsWebSearch, core.CapWebSearch)
	set(entry.SupportsAssistantPrefill, core.CapAssistantPrefill)
	return capability
}
```

---

## 9. Sync

`internal/modelcatalog/sync.go`

Order: **validate everything before touching stored rows.**

```text
fetch -> digest -> decode -> classify -> parse -> build candidate snapshot
      -> one transaction: upsert providers, metadata, pricing, reconcile
      -> atomically publish the already-validated snapshot
```

```go
type CatalogGeneration struct {
	Source        string
	GenerationID  string
	ContentDigest string
	Providers     []core.Provider
	Metadata      []dbstore.StoreModelMetadata
	Pricing       []dbstore.StoreModelPricing
	InputCount    int
	ModelCount    int
	ControlCount  int
	RejectedCount int
}

func validateGeneration(generation CatalogGeneration, previousModelCount int) error {
	if generation.ModelCount == 0 || len(generation.Pricing) == 0 {
		return fmt.Errorf("refusing empty catalog generation")
	}
	if generation.InputCount != generation.ModelCount+generation.ControlCount+generation.RejectedCount {
		return fmt.Errorf("unclassified records: input=%d model=%d control=%d rejected=%d",
			generation.InputCount, generation.ModelCount, generation.ControlCount, generation.RejectedCount)
	}
	if previousModelCount > 0 && generation.ModelCount < previousModelCount*80/100 {
		return fmt.Errorf("refusing catalog collapse from %d to %d rows",
			previousModelCount, generation.ModelCount)
	}
	return nil
}
```

The collapse guard matters because the feed is a mutable URL with no ETag. A truncated response
must not delete 3,000 priced rows.

Reconcile is scoped by `source`, so a datasheet sync can never delete manual rows or overrides:

```go
return s.DB.Transaction(func(tx *gorm.DB) error {
	if err := s.UpsertProviders(tx, generation.Providers); err != nil {
		return err
	}
	if err := upsertMetadata(tx, generation.Metadata); err != nil {
		return err
	}
	if err := upsertPricing(tx, generation.Pricing); err != nil {
		return err
	}
	if err := tx.Where("source = ? AND sync_generation <> ?", generation.Source, generation.GenerationID).
		Delete(&StoreModelPricing{}).Error; err != nil {
		return err
	}
	return tx.Where("source = ? AND sync_generation <> ?", generation.Source, generation.GenerationID).
		Delete(&StoreModelMetadata{}).Error
})
```

Row IDs are deterministic so re-importing the same feed does not mint new identities:

```go
var catalogNamespace = uuid.MustParse("a7462c66-6951-4a95-9266-df561cadf752")

func pricingRowID(source, rawKey string) string {
	return uuid.NewSHA1(catalogNamespace, []byte("price\x00"+source+"\x00"+rawKey)).String()
}

func metadataRowID(source string, key core.CatalogKey) string {
	value := "meta\x00" + source + "\x00" + string(key.Provider) + "\x00" +
		key.ModelName + "\x00" + key.Mode.String()
	return uuid.NewSHA1(catalogNamespace, []byte(value)).String()
}
```

---

## 10. Snapshot and resolution

`internal/modelcatalog/snapshot.go`

### 10.1 One pointer

```go
type catalogSnapshot struct {
	generation string

	metadataRows []core.ModelMetadata
	pricingRows  []core.PricingVariant

	models        map[core.CatalogKey]*core.ModelMetadata
	prices        map[core.PriceKey]*core.PricingVariant
	rawRows       map[core.SourceRowKey]*core.PricingVariant
	conflicts     map[core.PriceKey][]*core.PricingVariant
	variantCounts map[core.CatalogKey]int
}

type ModelCatalog struct {
	snapshot atomic.Pointer[catalogSnapshot]

	store  *dbstore.Store
	logger *zap.Logger
	client *http.Client

	refresh sync.Mutex
	cfg     config.ModelCatalogConfig
	done    chan struct{}
	wg      sync.WaitGroup
}
```

The current three independent `atomic.Pointer` fields (`models`, `basePricing`,
`customPricing`) are replaced by one. With three, a reader can observe new metadata against old
pricing mid-swap. One pointer means every reader sees a single consistent generation.

Readers never lock. `refresh` serializes writers only.

### 10.2 Building

```go
func buildSnapshot(generation string, metadata []core.ModelMetadata, pricing []core.PricingVariant) (*catalogSnapshot, error) {
	snapshot := &catalogSnapshot{
		generation:    generation,
		metadataRows:  metadata,
		pricingRows:   pricing,
		models:        make(map[core.CatalogKey]*core.ModelMetadata, len(metadata)),
		prices:        make(map[core.PriceKey]*core.PricingVariant, len(pricing)),
		rawRows:       make(map[core.SourceRowKey]*core.PricingVariant, len(pricing)),
		conflicts:     make(map[core.PriceKey][]*core.PricingVariant),
		variantCounts: make(map[core.CatalogKey]int),
	}

	for i := range snapshot.metadataRows {
		row := &snapshot.metadataRows[i]
		snapshot.models[row.Key] = row
	}

	candidates := make(map[core.PriceKey][]*core.PricingVariant, len(pricing))
	for i := range snapshot.pricingRows {
		row := &snapshot.pricingRows[i]

		rawKey := core.SourceRowKey{Source: row.Source, RawKey: row.RawKey}
		if _, exists := snapshot.rawRows[rawKey]; exists {
			return nil, fmt.Errorf("duplicate source row %s/%s", row.Source, row.RawKey)
		}
		snapshot.rawRows[rawKey] = row

		candidates[row.Key] = append(candidates[row.Key], row)
		snapshot.variantCounts[row.Key.CatalogKey]++
	}

	for key, rows := range candidates {
		if len(rows) == 1 {
			snapshot.prices[key] = rows[0]
			continue
		}
		// Identical rates under one key are harmless; differing rates are a
		// real ambiguity and must NOT be resolved by picking a winner, because
		// map iteration order would decide which price a customer pays.
		if allSameRates(rows) {
			snapshot.prices[key] = lowestRawKey(rows)
			continue
		}
		snapshot.conflicts[key] = rows
	}
	return snapshot, nil
}
```

`rawRows` guarantees every imported row stays addressable even if it never wins a lookup. That
is the property that makes row loss impossible rather than merely unlikely.

### 10.3 Resolving

```go
var (
	ErrCatalogMiss        = errors.New("model not found in catalog")
	ErrVariantRequired    = errors.New("model requires variant parameters")
	ErrUnsupportedVariant = errors.New("no price for the requested variant")
	ErrAmbiguousPrice     = errors.New("conflicting price rows")
	ErrUnpricedRow        = errors.New("price row has no billable rate")
	ErrCapabilityMissing  = errors.New("model does not support the request")
)

type ResolveQuery struct {
	Model                ModelKey
	Mode                 ModelType
	Selectors            SelectorSet
	DefaultSelectors     *SelectorSet
	RequiredCapabilities Capability
}

type ResolvedCatalog struct {
	Metadata   ModelMetadata
	Variant    PricingVariant
	RateCard   RateCard
	Generation string
}
```

```go
// Resolve is the only request-path entry point. It returns metadata and pricing
// pinned to one snapshot generation, so a sync completing mid-request cannot
// change the price between validation and billing.
func (c *ModelCatalog) Resolve(query core.ResolveQuery) (core.ResolvedCatalog, error) {
	snapshot := c.snapshot.Load()
	if snapshot == nil {
		return core.ResolvedCatalog{}, core.ErrCatalogMiss
	}

	catalogKey := core.CatalogKey{
		Provider:  query.Model.Provider,
		ModelName: query.Model.ModelName,
		Mode:      query.Mode,
	}
	metadata, ok := snapshot.models[catalogKey]
	if !ok {
		return core.ResolvedCatalog{}, core.ErrCatalogMiss
	}
	if !metadata.Capability.SupportsAll(query.RequiredCapabilities) {
		return core.ResolvedCatalog{}, core.ErrCapabilityMissing
	}

	variant, err := resolveVariant(snapshot, catalogKey, query)
	if err != nil {
		return core.ResolvedCatalog{}, err
	}
	if variant.Status != core.RatePriced {
		return core.ResolvedCatalog{}, core.ErrUnpricedRow
	}

	return core.ResolvedCatalog{
		Metadata:   *metadata,
		Variant:    *variant,
		RateCard:   variant.RateCard,
		Generation: snapshot.generation,
	}, nil
}

func resolveVariant(snapshot *catalogSnapshot, catalogKey core.CatalogKey, query core.ResolveQuery) (*core.PricingVariant, error) {
	priceKey := core.PriceKey{CatalogKey: catalogKey, SelectorKey: query.Selectors.Key}

	if _, ambiguous := snapshot.conflicts[priceKey]; ambiguous {
		return nil, core.ErrAmbiguousPrice
	}
	if row, ok := snapshot.prices[priceKey]; ok {
		return row, nil
	}

	// An operator-configured default applies only when the client sent nothing.
	// It must never rescue an explicit selector that simply has no price.
	if query.Selectors.IsEmpty() && query.DefaultSelectors != nil {
		priceKey.SelectorKey = query.DefaultSelectors.Key
		if _, ambiguous := snapshot.conflicts[priceKey]; ambiguous {
			return nil, core.ErrAmbiguousPrice
		}
		if row, ok := snapshot.prices[priceKey]; ok {
			return row, nil
		}
	}

	// There is deliberately no fallback to the {} row after an explicit miss.
	// Falling back would bill a 1024x1024 image at the 256x256 rate.
	if snapshot.variantCounts[catalogKey] > 0 {
		if query.Selectors.IsEmpty() {
			return nil, core.ErrVariantRequired
		}
		return nil, core.ErrUnsupportedVariant
	}
	return nil, core.ErrCatalogMiss
}
```

The three misses stay distinct because they need different responses:

```text
ErrCatalogMiss         404   we do not know this model
ErrVariantRequired     400   tell us size/steps
ErrUnsupportedVariant  400   we do not price that combination
```

---

## 11. Cost calculation

`internal/core/ratecard.go`

```go
type Money struct {
	USD decimal.Decimal
}

// NormalizedUsage is filled by the provider adapter. Present marks which
// quantities the provider actually reported, so a genuine zero stays
// distinguishable from a field the provider never sent.
type NormalizedUsage struct {
	Present map[Meter]bool

	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	InputCharacters   int64
	InputAudioTokens  int64
	OutputAudioTokens int64
	InputImages       int64
	OutputImages      int64
	GeneratedPixels   int64
	Pages             int64
}

func (u NormalizedUsage) Has(meter Meter) bool {
	return u.Present != nil && u.Present[meter]
}

func CalculateCost(card RateCard, usage NormalizedUsage) (Money, error) {
	if usage.CachedInputTokens > usage.InputTokens {
		return Money{}, fmt.Errorf("cached input exceeds total input")
	}

	// When the card prices cached tokens separately, the input-token term must
	// bill only the uncached remainder or cached tokens are charged twice.
	hasCachedRate := false
	for _, term := range card.Terms {
		if term.Meter == MeterCachedInputToken {
			hasCachedRate = true
			break
		}
	}

	total := decimal.Zero
	for _, term := range card.Terms {
		if !usage.Has(term.Meter) {
			continue // provider did not report this meter; nothing to bill
		}
		price, err := decimal.NewFromString(term.PriceUSD)
		if err != nil {
			return Money{}, fmt.Errorf("invalid %s rate %q: %w", term.Meter, term.PriceUSD, err)
		}
		quantity, err := meterQuantity(term.Meter, usage, hasCachedRate)
		if err != nil {
			return Money{}, err
		}
		total = total.Add(price.Mul(quantity))
	}
	return Money{USD: total}, nil
}

func meterQuantity(meter Meter, usage NormalizedUsage, hasCachedRate bool) (decimal.Decimal, error) {
	switch meter {
	case MeterInputToken:
		billable := usage.InputTokens
		if hasCachedRate {
			billable -= usage.CachedInputTokens
		}
		return decimal.NewFromInt(billable), nil
	case MeterCachedInputToken:
		return decimal.NewFromInt(usage.CachedInputTokens), nil
	case MeterOutputToken:
		return decimal.NewFromInt(usage.OutputTokens), nil
	case MeterInputCharacter:
		return decimal.NewFromInt(usage.InputCharacters), nil
	case MeterInputAudioToken:
		return decimal.NewFromInt(usage.InputAudioTokens), nil
	case MeterOutputAudioToken:
		return decimal.NewFromInt(usage.OutputAudioTokens), nil
	case MeterInputImage:
		return decimal.NewFromInt(usage.InputImages), nil
	case MeterOutputImage:
		return decimal.NewFromInt(usage.OutputImages), nil
	case MeterGeneratedPixel:
		return decimal.NewFromInt(usage.GeneratedPixels), nil
	case MeterPage:
		return decimal.NewFromInt(usage.Pages), nil
	default:
		return decimal.Zero, fmt.Errorf("unsupported meter %q", meter)
	}
}
```

`CalculateCost` **skips** meters the provider did not report rather than erroring. Providers
routinely omit fields that are zero; erroring would fail live traffic on a normal response.

---

## 12. Migration order

`AutoMigrate` cannot do this. Versioned migration, in this order:

```text
1. Consolidate duplicate provider rows, repoint foreign keys, add uq_provider_name.
2. Add IsConfigured to providers.
3. Create model_metadata and model_pricing with new columns, nullable, no indexes.
4. Backfill source, raw_key, mode, selector_key ('{}' for everything existing).
5. Assert zero NULLs and zero duplicates on the new unique targets.
6. Create uq_metadata, ix_metadata_lookup, uq_pricing_source_row, ix_pricing_lookup.
7. Drop the old (provider_id, model_name) unique index on pricing.
8. Remove both tables from the AutoMigrate list.
9. Run the same migration against SQLite and PostgreSQL.
```

Steps 1–5 are additive and revertible. Step 7 is the point of no return.

---

## 13. End-to-end: real datasheet rows to a charge

### 13.1 The feed rows

Two records, verbatim:

```json
"1024-x-1024/50-steps/stability.stable-diffusion-xl-v1": {
  "max_input_tokens": 77,
  "max_tokens": 77,
  "mode": "image_generation",
  "output_cost_per_image": 0.04,
  "provider": "bedrock",
  "base_model": "stable-diffusion-xl-v1"
},
"1024-x-1024/max-steps/stability.stable-diffusion-xl-v1": {
  "max_input_tokens": 77,
  "max_tokens": 77,
  "mode": "image_generation",
  "output_cost_per_image": 0.08,
  "provider": "bedrock",
  "base_model": "stable-diffusion-xl-v1"
}
```

Same model. Same size. **Steps double the price.** Under a `(provider, model_name)` key one of
these overwrites the other and half of all requests are billed at the wrong rate.

### 13.2 Ingestion

`ParseFeedKey("1024-x-1024/50-steps/stability.stable-diffusion-xl-v1", "bedrock", image_generation)`:

```text
parts            [1024-x-1024, 50-steps, stability.stable-diffusion-xl-v1]
parts[0]=="bedrock"?   no -> nothing stripped
walk: 1024-x-1024   matches size marker    -> size = "1024x1024"
walk: 50-steps      matches steps marker   -> steps = "50"
walk: stability...  no match               -> stop
model_name       stability.stable-diffusion-xl-v1
selector_key     {"size":"1024x1024","steps":"50"}
```

**`model_metadata`** — one row, both feed records collapse into it:

```text
source          bifrost-datasheet
provider_id     <bedrock>
model_name      stability.stable-diffusion-xl-v1
mode            image_generation
base_model      stable-diffusion-xl-v1
capabilities    []
limits          {"max_input_tokens":77}
```

**`model_pricing`** — two rows, one per priced variant:

```text
raw_key       1024-x-1024/50-steps/stability.stable-diffusion-xl-v1
model_name    stability.stable-diffusion-xl-v1
mode          image_generation
selector_key  {"size":"1024x1024","steps":"50"}
rate_card     [{"meter":"output_image","price_usd":"0.04","source_field":"output_cost_per_image"}]
status        priced

raw_key       1024-x-1024/max-steps/stability.stable-diffusion-xl-v1
model_name    stability.stable-diffusion-xl-v1
mode          image_generation
selector_key  {"size":"1024x1024","steps":"max"}
rate_card     [{"meter":"output_image","price_usd":"0.08","source_field":"output_cost_per_image"}]
status        priced
```

### 13.3 The client request

```bash
curl http://localhost:8085/v1/images/generations \
  -H "Authorization: Bearer dk-live-7f2a..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "bedrock/stability.stable-diffusion-xl-v1",
    "prompt": "A lighthouse during a storm",
    "size": "1024x1024",
    "steps": 50,
    "n": 2
  }'
```

### 13.4 The path

**1. Mode from the route.** `/v1/images/generations` → `ModelTypeImageGeneration`.

**2. Parse the model.** `bedrock` is split off only because it is a registered provider — not
because the string contains a slash. `groq/openai/gpt-oss-120b` keeps its slashes for the same
reason.

```text
provider    bedrock
model_name  stability.stable-diffusion-xl-v1
```

**3. Authenticate and authorize.** The virtual key is resolved, then `AllowedModels` for the
`bedrock` provider config is checked against `stability.stable-diffusion-xl-v1` — the
provider-relative name, without the redundant `bedrock/` prefix.

**4. Extract selectors.** `SelectorRegistry.Extract(ModelTypeImageGeneration, body)` reads
`size` and `steps`. Note `"steps": 50` arrives as a JSON **number** while the feed wrote
`50-steps` as a **string**; both normalize to `"50"`.

```text
{"size":"1024x1024","steps":"50"}
```

Byte-identical to what the parser stored at ingestion. That is the whole mechanism.

**5. Resolve — one snapshot load, both catalogs.**

```text
models[{bedrock, stability.stable-diffusion-xl-v1, image_generation}]
  -> limits max_input_tokens 77, capabilities []

prices[{bedrock, stability.stable-diffusion-xl-v1, image_generation, {"size":"1024x1024","steps":"50"}}]
  -> rate_card [{output_image, "0.04"}]   status priced
```

Both come from the same `catalogSnapshot`, so a sync completing right now cannot change the
price between here and step 8.

**6. Route.** A healthy Bedrock deployment is selected; the adapter receives the upstream model
ID `stability.stable-diffusion-xl-v1`.

**7. Call the provider.** The adapter returns 2 images and reports:

```go
NormalizedUsage{
    Present:      map[Meter]bool{MeterOutputImage: true},
    OutputImages: 2,
}
```

**8. Bill against the pinned rate card.**

```text
term         output_image
price        0.04            (decimal, from the string "0.04")
quantity     2
cost         0.08 USD
```

**9. Record.** The ledger row carries full provenance, so the charge can be re-derived later:

```text
request_id         01J8X...
virtual_key_id     vk_9c1...
provider           bedrock
model_name         stability.stable-diffusion-xl-v1
mode               image_generation
selector_key       {"size":"1024x1024","steps":"50"}
source             bifrost-datasheet
raw_key            1024-x-1024/50-steps/stability.stable-diffusion-xl-v1
catalog_generation gen_2026-08-05T00:04:11Z_d87b4a71
cost_usd           0.08
```

`raw_key` plus `catalog_generation` means any disputed invoice line resolves to the exact feed
record that produced it.

### 13.5 The same request with `"steps": "max"`

Only the selector key changes:

```text
{"size":"1024x1024","steps":"max"}  ->  rate_card [{output_image, "0.08"}]
2 × $0.08 = $0.16
```

Twice the cost, correctly. This is the case that silently mis-bills under a single-row key.

### 13.6 The same request with no `steps`

```text
selector_key  {"size":"1024x1024"}
prices[...]   miss
variantCounts[{bedrock, stability.stable-diffusion-xl-v1, image_generation}] == 2
Selectors.IsEmpty() == false
  -> ErrUnsupportedVariant -> 400
```

This model has **no `{}` row** — the feed publishes it only with size and steps. Falling back to
some other row would invent a price. The request is rejected before the provider is called, so
the customer is never charged for something we cannot price.

### 13.7 A chat model, for contrast

```json
"gpt-4o": {
  "input_cost_per_token": 2.5e-06,
  "output_cost_per_token": 1e-05,
  "cache_read_input_token_cost": 1.25e-06,
  "max_input_tokens": 128000,
  "max_output_tokens": 16384,
  "mode": "chat",
  "supports_function_calling": true,
  "supports_vision": true,
  "supports_prompt_caching": true,
  "provider": "openai",
  "base_model": "gpt-4o"
}
```

No markers in the key, and chat has no registered selector extractor, so:

```text
model_metadata   openai | gpt-4o | chat | caps: function_calling|vision|prompt_caching|...
model_pricing    openai | gpt-4o | chat | {} | [input_token 2.5e-06,
                                               cached_input_token 1.25e-06,
                                               output_token 1e-05]
```

One model row, one price row. Given 1,000 input tokens of which 200 were cache hits, and 500
output tokens:

```text
input_token         (1000 - 200) × 0.0000025   = 0.00200
cached_input_token         200   × 0.00000125  = 0.00025
output_token               500   × 0.00001     = 0.00500
                                                 ─────────
                                                 $0.00725
```

The cached tokens are subtracted from the input term because the card prices them separately —
without that, they are billed twice.

Same two tables, same resolution path, same cost function. The only difference is that the
selector key is `{}`.
