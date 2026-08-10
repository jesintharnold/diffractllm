package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ModelKey struct {
	Provider  Provider
	ModelName string
}

func (m ModelKey) SlashKey() string {
	return string(m.Provider) + "/" + m.ModelName
}

type CatalogKey struct {
	ModelKey
	ModelType ModelType
}

func NewCatalogKey(model ModelKey, modelType ModelType) CatalogKey {
	return CatalogKey{ModelKey: model, ModelType: modelType}
}

type ModelLimits struct {
	ContextWindow        int32 `json:"context_window,omitempty"`
	MaxInputTokens       int32 `json:"max_input_tokens,omitempty"`
	MaxOutputTokens      int32 `json:"max_output_tokens,omitempty"`
	LongContextThreshold int32 `json:"long_context_threshold,omitempty"`
}

type ModelMetadata struct {
	ID           string      `json:"id,omitempty"`
	Provider     Provider    `json:"provider"`
	ModelType    ModelType   `json:"model_type"`
	ModelName    string      `json:"model_name"`
	BaseModel    string      `json:"base_model,omitempty"`
	Capability   Capability  `json:"-"`
	Limits       ModelLimits `json:"limits"`
	SourceRawKey string      `json:"source_raw_key,omitempty"`
}

func (m ModelMetadata) ModelKey() ModelKey {
	return ModelKey{Provider: m.Provider, ModelName: m.ModelName}
}

func (m ModelMetadata) CatalogKey() CatalogKey {
	return CatalogKey{ModelKey: m.ModelKey(), ModelType: m.ModelType}
}

type SelectorSet struct {
	Key string `json:"key"`
}

const EmptySelectorKey = "{}"

func NewSelectorSet(values map[string]string) (SelectorSet, error) {
	if len(values) == 0 {
		return SelectorSet{Key: EmptySelectorKey}, nil
	}

	for name, value := range values {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
			return SelectorSet{}, fmt.Errorf("selector name and value are required")
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return SelectorSet{}, fmt.Errorf("encode selector key: %w", err)
	}
	return SelectorSet{Key: string(encoded)}, nil
}

func (s SelectorSet) IsEmpty() bool { return s.CanonicalKey() == EmptySelectorKey }
func (s SelectorSet) CanonicalKey() string {
	if s.Key == "" {
		return EmptySelectorKey
	}
	return s.Key
}

type PriceKey struct {
	ModelKey
	SelectorKey string
}

func NewPriceKey(model ModelKey, selectors SelectorSet) PriceKey {
	return PriceKey{ModelKey: model, SelectorKey: selectors.CanonicalKey()}
}
