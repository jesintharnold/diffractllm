package modelcatalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"diffractllm/internal/core"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const pricingURL = "https://getbifrost.ai/datasheet"
const maxFeedInBytes = 32 << 20

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

var errControlRow = errors.New("feed row is not a model")

func isQualityMarker(parts []string) bool {
	if len(parts) < 3 {
		return false
	}
	if _, known := qualityMarkers[parts[0]]; !known {
		return false
	}
	return sizeMarker.MatchString(parts[1])
}

type SourceSchema struct {
	diffracModelType     core.ModelType   `json:"-"`
	diffractProvider     core.Provider    `json:"-"`
	diffractSelectorSet  core.SelectorSet `json:"-"`
	diffractCapabilities core.Capability  `json:"-"`
	diffractModelName    string           `json:"-"`
	diffractRawKey       string           `json:"-"`

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

func (s *SourceSchema) capabilities() core.Capability {
	var capability core.Capability
	set := func(flag *bool, bit core.Capability) {
		if flag != nil && *flag {
			capability |= bit
		}
	}

	set(s.SupportsFunctionCalling, core.CapFunctionCalling)
	set(s.SupportsParallelFunctionCalling, core.CapParallelToolCalls)
	set(s.SupportsToolChoice, core.CapToolChoice)
	set(s.SupportsReasoning, core.CapReasoning)
	set(s.SupportsVision, core.CapVision)
	set(s.SupportsImageInput, core.CapImageInput)
	set(s.SupportsAudioInput, core.CapAudioInput)
	set(s.SupportsAudioOutput, core.CapAudioOutput)
	set(s.SupportsPDFInput, core.CapPDFInput)
	set(s.SupportsVideoInput, core.CapVideoInput)
	set(s.SupportsPromptCaching, core.CapPromptCaching)
	set(s.SupportsResponseSchema, core.CapResponseSchema)
	set(s.SupportsSystemMessages, core.CapSystemMessages)
	set(s.SupportsNativeStreaming, core.CapStreaming)
	set(s.SupportsWebSearch, core.CapWebSearch)
	set(s.SupportsComputerUse, core.CapComputerUse)
	set(s.SupportsAssistantPrefill, core.CapAssistantPrefill)
	set(s.SupportsEmbeddingImageInput, core.CapEmbeddingImageInput)
	return capability
}

func (s *SourceSchema) UnmarshalJSON(data []byte) error {
	type Alias SourceSchema
	var aux Alias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&aux); err != nil {
		return fmt.Errorf("decode : %w", err)
	}

	if strings.TrimSpace(aux.Mode) == "" {
		return errControlRow
	}
	modeltype := core.ParseModelType(aux.Mode)
	if modeltype == core.ModelTypeUnknown {
		return fmt.Errorf("unknown mode %q", aux.Mode)
	}
	aux.diffracModelType = modeltype

	rawProvider := strings.TrimSpace(aux.Provider)
	if rawProvider == "" {
		return fmt.Errorf("decode providers are null")
	}

	provider := core.Provider(rawProvider)
	if aliasProvider, exists := providerAliases[rawProvider]; exists {
		provider = aliasProvider
	}

	aux.diffractProvider = provider
	if aux.SearchContext != nil {
		aux.SearchContextCostPerQueryHigh = aux.SearchContext.High
		aux.SearchContextCostPerQueryLow = aux.SearchContext.Low
		aux.SearchContextCostPerQueryMedium = aux.SearchContext.Medium
	}
	temp := SourceSchema(aux)
	temp.diffractCapabilities = temp.capabilities()
	*s = temp
	return nil
}

type CatalogSource struct {
	ModelsCatalog    []core.ModelMetadata
	PricingCatalog   []core.PricingVariant
	TotalModels      int
	TotalUnknown     int
	TotalSourceCount int
	Digest           string
}

func (c *CatalogSource) Fetch(ctx context.Context, client *http.Client) (*[]core.ModelMetadata, *[]core.PricingVariant, error) {
	if client == nil {
		client = http.DefaultClient
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pricingURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request for %s: %w", pricingURL, err)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch %s: %w", pricingURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("fetch %s: status %s", pricingURL, response.Status)
	}

	limited := io.LimitReader(response.Body, maxFeedInBytes+1)
	hasher := sha256.New()
	counter := &countingReader{reader: io.TeeReader(limited, hasher)}

	parsedSchema, err := c.modelParser(counter)

	if counter.count > maxFeedInBytes {
		return nil, nil, fmt.Errorf("%s exceeds %d bytes", pricingURL, maxFeedInBytes)
	}
	if err != nil {
		return nil, nil, err
	}

	c.Digest = hex.EncodeToString(hasher.Sum(nil))
	c.build(parsedSchema)
	return &c.ModelsCatalog, &c.PricingCatalog, nil
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

func (c *CatalogSource) build(rows []SourceSchema) {
	c.ModelsCatalog = make([]core.ModelMetadata, 0, len(rows))
	c.PricingCatalog = make([]core.PricingVariant, 0, len(rows))

	seen := make(map[core.CatalogKey]struct{}, len(rows))

	for i := range rows {
		row := &rows[i]
		key := core.ModelKey{
			Provider:  row.diffractProvider,
			ModelName: row.diffractModelName,
		}
		catalogKey := core.NewCatalogKey(key, row.diffracModelType)

		if _, exists := seen[catalogKey]; !exists {
			seen[catalogKey] = struct{}{}
			c.ModelsCatalog = append(c.ModelsCatalog, core.ModelMetadata{
				Provider:   row.diffractProvider,
				ModelType:  row.diffracModelType,
				ModelName:  row.diffractModelName,
				BaseModel:  row.BaseModel,
				Capability: row.diffractCapabilities,
				Limits: core.ModelLimits{
					ContextWindow:        row.MaxTokens,
					MaxInputTokens:       row.MaxInputTokens,
					MaxOutputTokens:      row.MaxOutputTokens,
					LongContextThreshold: row.LongContextThreshold,
				},
				SourceRawKey: row.diffractRawKey,
			})
		}

		c.PricingCatalog = append(c.PricingCatalog, core.PricingVariant{
			RawKey:    row.diffractRawKey,
			Key:       core.NewPriceKey(key, row.diffractSelectorSet),
			ModelType: row.diffracModelType,
			Selectors: row.diffractSelectorSet,
			Pricing:   row.Pricing,
		})
	}

	c.TotalModels = len(c.ModelsCatalog)
}

func (c *CatalogSource) modelParser(data io.Reader) ([]SourceSchema, error) {
	dec := json.NewDecoder(data)
	dec.UseNumber()

	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("failed to read start object wrapper: %w", err)
	}

	results := make([]SourceSchema, 0)
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("failed to read map key token: %w", err)
		}

		rawModelKey, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string map key, got %T", keyToken)
		}
		c.TotalSourceCount++

		var schema SourceSchema
		if err := dec.Decode(&schema); err != nil {
			if errors.Is(err, errControlRow) {
				c.TotalUnknown++
				continue
			}
			return nil, fmt.Errorf("failed parsing structural entity for %s: %w", rawModelKey, err)
		}
		schema.diffractRawKey = rawModelKey

		parts := strings.Split(rawModelKey, "/")
		if len(parts) > 1 && (parts[0] == schema.Provider || parts[0] == string(schema.diffractProvider)) {
			parts = parts[1:]
		}

		values := make(map[string]string, 3)
		consumed := false

		if schema.diffracModelType == core.ModelTypeImageGeneration {
		walk:
			for len(parts) > 1 {
				head := parts[0]
				switch {
				case sizeMarker.MatchString(head):
					if _, exists := values["size"]; exists {
						return nil, fmt.Errorf("duplicate size in %q", rawModelKey)
					}
					match := sizeMarker.FindStringSubmatch(head)
					values["size"] = match[1] + "x" + match[2]

				case stepsMarker.MatchString(head):
					if _, exists := values["steps"]; exists {
						return nil, fmt.Errorf("duplicate steps in %q", rawModelKey)
					}
					values["steps"] = stepsMarker.FindStringSubmatch(head)[1]

				case isQualityMarker(parts):
					if _, exists := values["quality"]; exists {
						return nil, fmt.Errorf("duplicate quality in %q", rawModelKey)
					}
					values["quality"] = head

				default:
					break walk
				}
				parts = parts[1:]
				consumed = true
			}
		}
		if consumed && len(parts) >= 2 && parts[0] == string(schema.diffractProvider) {
			parts = parts[1:]
		}
		modelName := strings.Join(parts, "/")
		if modelName == "" {
			return nil, fmt.Errorf("empty model name in %q", rawModelKey)
		}
		selectors, err := core.NewSelectorSet(values)
		if err != nil {
			return nil, fmt.Errorf("selectors for %q: %w", rawModelKey, err)
		}
		schema.diffractSelectorSet = selectors
		schema.diffractModelName = modelName
		results = append(results, schema)
	}

	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("failed to clear closing object token: %w", err)
	}
	return results, nil
}
