package core

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type VKMode uint8

func (m VKMode) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

func (m *VKMode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseVKMode(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}

const (
	VKDirect VKMode = iota
	VKWeighted
)

const (
	VKDirectName   = "direct"
	VKWeightedName = "weighted"
)

func (mode VKMode) String() string {
	switch mode {
	case VKDirect:
		return VKDirectName
	case VKWeighted:
		return VKWeightedName
	default:
		return ""
	}
}

func ParseVKMode(value string) (VKMode, error) {
	switch value {
	case VKDirectName:
		return VKDirect, nil
	case VKWeightedName:
		return VKWeighted, nil
	default:
		return 0, fmt.Errorf("unknow virtual key mode %q", value)
	}
}

type ProviderConfig struct {
	Provider             Provider            `json:"provider"`
	AllowedModels        []string            `json:"allowed_models"`
	Weight               float32             `json:"weight,omitempty"`
	runtimeAllowedModels map[string]struct{} `json:"-"`
	allowAll             bool                `json:"-"`
}

func (config *ProviderConfig) IsModelAllowed(key ModelKey) bool {
	if config == nil || key.Provider != config.Provider || key.ModelName == "" {
		return false
	}
	if config.allowAll {
		return true
	}
	_, allowed := config.runtimeAllowedModels[key.ModelName]
	return allowed
}

func CompileProviderConfigs(mode VKMode, configs []ProviderConfig) ([]*ProviderConfig, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one provider config is required")
	}
	if mode == VKDirect && len(configs) != 1 {
		return nil, fmt.Errorf("direct mode requires exactly one provider config")
	}
	runtimeConfigs := make([]*ProviderConfig, 0, len(configs))
	providers := make(map[Provider]struct{}, len(configs))
	for _, stored := range configs {
		provider := Provider(strings.TrimSpace(string(stored.Provider)))
		if provider == "" {
			return nil, fmt.Errorf("provider cannot be empty")
		}
		if _, exists := providers[provider]; exists {
			return nil, fmt.Errorf("duplicate provider %q", provider)
		}
		providers[provider] = struct{}{}
		if mode == VKWeighted && (stored.Weight <= 0 || stored.Weight > 1 || math.IsNaN(float64(stored.Weight)) || math.IsInf(float64(stored.Weight), 0)) {
			return nil, fmt.Errorf("provider %q weight must be finite and in (0, 1]", provider)
		}
		config := &ProviderConfig{
			Provider:             provider,
			Weight:               stored.Weight,
			AllowedModels:        make([]string, 0, len(stored.AllowedModels)),
			runtimeAllowedModels: make(map[string]struct{}, len(stored.AllowedModels)),
		}

		for _, rawModel := range stored.AllowedModels {
			model := strings.TrimSpace(rawModel)
			if model == "" {
				return nil, fmt.Errorf("provider %q contains an empty model", provider)
			}

			if model == "*" {
				config.allowAll = true
			}
			if _, duplicate := config.runtimeAllowedModels[model]; duplicate {
				continue
			}
			config.runtimeAllowedModels[model] = struct{}{}
			config.AllowedModels = append(config.AllowedModels, model)
		}
		if len(config.AllowedModels) == 0 {
			return nil, fmt.Errorf("provider %q requires at least one allowed model", provider)
		}
		runtimeConfigs = append(runtimeConfigs, config)
	}
	return runtimeConfigs, nil
}

type ScopeType string

const (
	ScopeGlobal     ScopeType = "global"
	ScopeProvider   ScopeType = "provider"
	ScopeVirtualKey ScopeType = "virtualkey"
)

type VirtualKey struct {
	ID              string
	Key             string
	ClientID        string
	BudgetID        string
	IsActive        bool
	ExpiresAt       *time.Time
	Mode            VKMode
	CustomPoolName  string
	LoadBalancer    LBKind
	ProviderConfigs []*ProviderConfig
}

func (vkconfig *VirtualKey) IsModelKeyAllowed(key ModelKey) bool {
	for _, vkproviderconfig := range vkconfig.ProviderConfigs {
		if vkproviderconfig.IsModelAllowed(key) {
			return true
		}
	}
	return false
}

type VirtualKeyRequest struct {
	ClientID        string           `json:"client_id" binding:"required"`
	BudgetID        string           `json:"budget_id" binding:"required"`
	ExpiresAt       *time.Time       `json:"expires_at"`
	Mode            *VKMode          `json:"mode" binding:"required"`
	CustomPoolName  string           `json:"custom_pool_name,omitempty"`
	LoadBalancer    *LBKind          `json:"load_balancer" binding:"required"`
	ProviderConfigs []ProviderConfig `json:"provider_configs" binding:"required,min=1"`
}

type VirtualKeyResponse struct {
	ID              string           `json:"id"`
	DisplayPrefix   string           `json:"display_prefix"`
	ClientID        string           `json:"client_id"`
	BudgetID        string           `json:"budget_id"`
	Mode            VKMode           `json:"mode"`
	CustomPoolName  string           `json:"custom_pool_name,omitempty"`
	LoadBalancer    LBKind           `json:"load_balancer"`
	ProviderConfigs []ProviderConfig `json:"provider_configs"`
	IsActive        bool             `json:"is_active"`
	ExpiresAt       time.Time        `json:"expires_at"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}
