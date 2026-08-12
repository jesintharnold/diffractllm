package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

type CatalogKey struct {
	Provider  Provider
	ModelName string
	ModelType ModelType
}

func (c CatalogKey) SlashKey() string {
	return string(c.Provider) + "/" + c.ModelName
}

func (c CatalogKey) RouteKey() CatalogKey {
	c.ModelType = ModelTypeUnknown
	return c
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

func (m ModelMetadata) CatalogKey() CatalogKey {
	return CatalogKey{Provider: m.Provider, ModelName: m.ModelName, ModelType: m.ModelType}
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
