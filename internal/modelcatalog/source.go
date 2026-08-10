package modelcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"diffractllm/internal/core"
)

const (
	SourceBifrost     = "bifrost"
	BifrostPricingURL = "https://getbifrost.ai/datasheet"
	maxFeedBytes      = 32 << 20
)

type Catalog struct {
	Source  string
	Digest  string
	Models  []core.ModelMetadata
	Pricing []core.PricingVariant
	Rows    int
	Control int
}

type Source interface {
	Name() string
	Fetch(ctx context.Context, client *http.Client) (Catalog, error)
}

type BifrostSource struct {
	PricingURL string
}

func (BifrostSource) Name() string { return SourceBifrost }

func (s BifrostSource) Fetch(ctx context.Context, client *http.Client) (Catalog, error) {
	pricingURL := s.PricingURL
	if pricingURL == "" {
		pricingURL = BifrostPricingURL
	}

	rows, digest, err := fetchJSON(ctx, client, pricingURL)
	if err != nil {
		return Catalog{}, err
	}

	catalog := Catalog{
		Source:  SourceBifrost,
		Digest:  digest,
		Rows:    len(rows),
		Models:  make([]core.ModelMetadata, 0, len(rows)),
		Pricing: make([]core.PricingVariant, 0, len(rows)),
	}

	seen := make(map[core.ModelKey]int, len(rows))

	for _, rawKey := range sortedKeys(rows) {
		metadata, variant, err := adaptRow(rawKey, rows[rawKey])
		if err != nil {
			if errors.Is(err, errControlRow) {
				catalog.Control++
				continue
			}
			return Catalog{}, fmt.Errorf("bifrost row %q: %w", rawKey, err)
		}

		if _, exists := seen[metadata.Key]; !exists {
			seen[metadata.Key] = len(catalog.Models)
			catalog.Models = append(catalog.Models, metadata)
		}
		catalog.Pricing = append(catalog.Pricing, variant)
	}

	return catalog, nil
}

func fetchJSON(ctx context.Context, client *http.Client, url string) (map[string]json.RawMessage, string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request for %s: %w", url, err)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch %s: status %s", url, response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxFeedBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", url, err)
	}
	if len(body) > maxFeedBytes {
		return nil, "", fmt.Errorf("%s exceeds %d bytes", url, maxFeedBytes)
	}

	rows := make(map[string]json.RawMessage)
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&rows); err != nil {
		return nil, "", fmt.Errorf("decode %s: %w", url, err)
	}

	sum := sha256.Sum256(body)
	return rows, hex.EncodeToString(sum[:]), nil
}

type feedRow struct {
	Mode                 string `json:"mode"`
	Provider             string `json:"provider"`
	BaseModel            string `json:"base_model"`
	MaxInputTokens       int32  `json:"max_input_tokens"`
	MaxOutputTokens      int32  `json:"max_output_tokens"`
	MaxTokens            int32  `json:"max_tokens"`
	LongContextThreshold int32  `json:"long_context_threshold"`
	SearchContext        *struct {
		Low    *float64 `json:"search_context_size_low"`
		Medium *float64 `json:"search_context_size_medium"`
		High   *float64 `json:"search_context_size_high"`
	} `json:"search_context_cost_per_query"`
	SupportsFunctionCalling         *bool `json:"supports_function_calling"`
	SupportsParallelFunctionCalling *bool `json:"supports_parallel_function_calling"`
	SupportsToolChoice              *bool `json:"supports_tool_choice"`
	SupportsReasoning               *bool `json:"supports_reasoning"`
	SupportsVision                  *bool `json:"supports_vision"`
	SupportsImageInput              *bool `json:"supports_image_input"`
	SupportsAudioInput              *bool `json:"supports_audio_input"`
	SupportsAudioOutput             *bool `json:"supports_audio_output"`
	SupportsPDFInput                *bool `json:"supports_pdf_input"`
	SupportsVideoInput              *bool `json:"supports_video_input"`
	SupportsPromptCaching           *bool `json:"supports_prompt_caching"`
	SupportsResponseSchema          *bool `json:"supports_response_schema"`
	SupportsSystemMessages          *bool `json:"supports_system_messages"`
	SupportsNativeStreaming         *bool `json:"supports_native_streaming"`
	SupportsWebSearch               *bool `json:"supports_web_search"`
	SupportsComputerUse             *bool `json:"supports_computer_use"`
	SupportsAssistantPrefill        *bool `json:"supports_assistant_prefill"`
	SupportsEmbeddingImageInput     *bool `json:"supports_embedding_image_input"`

	core.Pricing
}

var errControlRow = errors.New("feed row is not a model")

func adaptRow(rawKey string, raw json.RawMessage) (core.ModelMetadata, core.PricingVariant, error) {
	var row feedRow
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&row); err != nil {
		return core.ModelMetadata{}, core.PricingVariant{}, fmt.Errorf("decode: %w", err)
	}

	if strings.TrimSpace(row.Mode) == "" {
		return core.ModelMetadata{}, core.PricingVariant{}, errControlRow
	}
	modelType := core.ParseModelType(row.Mode)
	if modelType == core.ModelTypeUnknown {
		return core.ModelMetadata{}, core.PricingVariant{}, fmt.Errorf("unknown mode %q", row.Mode)
	}

	key, selectors, err := parseFeedKey(rawKey, row.Provider, modelType)
	if err != nil {
		return core.ModelMetadata{}, core.PricingVariant{}, err
	}

	pricing := row.Pricing
	if row.SearchContext != nil {
		pricing.SearchContextCostPerQueryLow = row.SearchContext.Low
		pricing.SearchContextCostPerQueryMedium = row.SearchContext.Medium
		pricing.SearchContextCostPerQueryHigh = row.SearchContext.High
	}

	metadata := core.ModelMetadata{
		Key:        key,
		ModelType:  modelType,
		BaseModel:  row.BaseModel,
		Capability: row.capabilities(),
		Limits: core.ModelLimits{
			ContextWindow:        row.MaxTokens,
			MaxInputTokens:       row.MaxInputTokens,
			MaxOutputTokens:      row.MaxOutputTokens,
			LongContextThreshold: row.LongContextThreshold,
		},
		Source:       SourceBifrost,
		SourceRawKey: rawKey,
	}

	variant := core.PricingVariant{
		Source:    SourceBifrost,
		RawKey:    rawKey,
		Key:       core.NewPriceKey(key, selectors),
		ModelType: modelType,
		Selectors: selectors,
		Pricing:   pricing,
	}

	return metadata, variant, nil
}

func (r feedRow) capabilities() core.Capability {
	var capability core.Capability
	set := func(flag *bool, bit core.Capability) {
		if flag != nil && *flag {
			capability |= bit
		}
	}

	set(r.SupportsFunctionCalling, core.CapFunctionCalling)
	set(r.SupportsParallelFunctionCalling, core.CapParallelToolCalls)
	set(r.SupportsToolChoice, core.CapToolChoice)
	set(r.SupportsReasoning, core.CapReasoning)
	set(r.SupportsVision, core.CapVision)
	set(r.SupportsImageInput, core.CapImageInput)
	set(r.SupportsAudioInput, core.CapAudioInput)
	set(r.SupportsAudioOutput, core.CapAudioOutput)
	set(r.SupportsPDFInput, core.CapPDFInput)
	set(r.SupportsVideoInput, core.CapVideoInput)
	set(r.SupportsPromptCaching, core.CapPromptCaching)
	set(r.SupportsResponseSchema, core.CapResponseSchema)
	set(r.SupportsSystemMessages, core.CapSystemMessages)
	set(r.SupportsNativeStreaming, core.CapStreaming)
	set(r.SupportsWebSearch, core.CapWebSearch)
	set(r.SupportsComputerUse, core.CapComputerUse)
	set(r.SupportsAssistantPrefill, core.CapAssistantPrefill)
	set(r.SupportsEmbeddingImageInput, core.CapEmbeddingImageInput)
	return capability
}

const searchContextField = "search_context_cost_per_query"

func isRateField(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "cost") || strings.Contains(lower, "price")
}

var (
	sizeMarker  = regexp.MustCompile(`^(\d+|max)-x-(\d+|max)$`)
	stepsMarker = regexp.MustCompile(`^(\d+|max)-steps$`)

	qualityMarkers = map[string]struct{}{
		"low": {}, "medium": {}, "high": {},
		"standard": {}, "hd": {}, "auto": {},
	}
	providerAliases = map[string]core.Provider{
		"azure_text":                    "azure",
		"fireworks_ai-embedding-models": "fireworks_ai",
		"text-completion-openai":        "openai",
		"text-completion-inception":     "inception",
		"amazon_nova":                   "amazon-nova",
	}
)

func parseFeedKey(
	rawKey, rawProvider string,
	modelType core.ModelType,
) (core.ModelKey, core.SelectorSet, error) {
	rawKey = strings.TrimSpace(rawKey)
	rawProvider = strings.TrimSpace(rawProvider)
	if rawKey == "" || rawProvider == "" {
		return core.ModelKey{}, core.SelectorSet{}, fmt.Errorf("raw key and provider are required")
	}

	provider := core.Provider(rawProvider)
	if alias, exists := providerAliases[rawProvider]; exists {
		provider = alias
	}

	parts := strings.Split(rawKey, "/")
	if len(parts) > 1 && (parts[0] == rawProvider || parts[0] == string(provider)) {
		parts = parts[1:]
	}

	values := make(map[string]string, 3)
	consumed := false

	if modelType == core.ModelTypeImageGeneration {
	walk:
		for len(parts) > 1 {
			head := parts[0]
			switch {
			case sizeMarker.MatchString(head):
				if _, exists := values["size"]; exists {
					return core.ModelKey{}, core.SelectorSet{}, fmt.Errorf("duplicate size in %q", rawKey)
				}
				match := sizeMarker.FindStringSubmatch(head)
				values["size"] = match[1] + "x" + match[2]

			case stepsMarker.MatchString(head):
				if _, exists := values["steps"]; exists {
					return core.ModelKey{}, core.SelectorSet{}, fmt.Errorf("duplicate steps in %q", rawKey)
				}
				values["steps"] = stepsMarker.FindStringSubmatch(head)[1]

			case isQualityMarker(parts):
				if _, exists := values["quality"]; exists {
					return core.ModelKey{}, core.SelectorSet{}, fmt.Errorf("duplicate quality in %q", rawKey)
				}
				values["quality"] = head

			default:
				break walk
			}
			parts = parts[1:]
			consumed = true
		}
	}
	if consumed && len(parts) >= 2 && parts[0] == string(provider) {
		parts = parts[1:]
	}

	modelName := strings.Join(parts, "/")
	if modelName == "" {
		return core.ModelKey{}, core.SelectorSet{}, fmt.Errorf("empty model name in %q", rawKey)
	}

	selectors, err := core.NewSelectorSet(values)
	if err != nil {
		return core.ModelKey{}, core.SelectorSet{}, fmt.Errorf("selectors for %q: %w", rawKey, err)
	}
	return core.ModelKey{Provider: provider, ModelName: modelName}, selectors, nil
}

func isQualityMarker(parts []string) bool {
	if len(parts) < 3 {
		return false
	}
	if _, known := qualityMarkers[parts[0]]; !known {
		return false
	}
	return sizeMarker.MatchString(parts[1])
}

func sortedKeys(rows map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
