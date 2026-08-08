package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type ModelKey struct {
	Provider  Provider
	ModelName string
}

func (m ModelKey) SlashKey() string {
	return string(m.Provider) + "/" + m.ModelName
}

type ModelLimits struct {
	ContextWindow        int32 `json:"context_window,omitempty"`
	MaxInputTokens       int32 `json:"max_input_tokens,omitempty"`
	MaxOutputTokens      int32 `json:"max_output_tokens,omitempty"`
	LongContextThreshold int32 `json:"long_context_threshold,omitempty"`
}

type ModelMetadata struct {
	ID           string      `json:"id,omitempty"`
	Key          ModelKey    `json:"key"`
	ModelType    ModelType   `json:"model_type"`
	BaseModel    string      `json:"base_model,omitempty"`
	Capability   Capability  `json:"-"`
	Limits       ModelLimits `json:"limits"`
	Source       string      `json:"source"`
	SourceRawKey string      `json:"source_raw_key,omitempty"`
}

func (m *ModelMetadata) Supports(required Capability) bool {
	return m.Capability.SupportsAll(required)
}

var (
	ErrCatalogMiss       = errors.New("model not found in catalog")
	ErrCapabilityMissing = errors.New("model does not support the request")
)

// -------------- selector -------------------

type Selector struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

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

func (s SelectorSet) IsEmpty() bool { return len(s.Values) == 0 }
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
