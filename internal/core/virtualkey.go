package core

import "time"

type VKMode uint8

const (
	VKAllowedModel VKMode = iota
	VKModelPool
)

const (
	VK_ALLOWED_MODEL = "allowed_model"
	VK_MODEL_POOL    = "model_pool"
)

func (mode VKMode) String() string {
	switch mode {
	case VKAllowedModel:
		return VK_ALLOWED_MODEL
	default:
		return VK_MODEL_POOL
	}
}

func ParseVKMode(Mode string) VKMode {
	switch Mode {
	case VK_ALLOWED_MODEL:
		return VKAllowedModel
	default:
		return VKModelPool
	}
}

type AllowedModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Weight   int    `json:"weight,omitempty"`
}

type ScopeType string

const (
	ScopeGlobal     ScopeType = "global"
	ScopeProvider   ScopeType = "provider"
	ScopeVirtualKey ScopeType = "virtualkey"
)

type VirtualKey struct {
	ID            string // DB UUID — carried onto the request context
	Key           string // raw key
	ClientID      string
	BudgetID      string // always set — use an unlimited budget if no cap needed
	IsActive      bool
	ExpiresAt     *time.Time
	Mode          VKMode
	AllowedModels map[ModelKey]struct{}
	ModelPools    map[string]struct{}
}

type VirtualKeyRequest struct {
	ClientID      string         `json:"client_id"      binding:"required"`
	BudgetID      string         `json:"budget_id"      binding:"required"`
	ExpiresAt     *time.Time     `json:"expires_at"`
	Mode          VKMode         `json:"mode"           binding:"required"`
	AllowedModels []AllowedModel `json:"allowed_models"`
	AllowedPools  []string       `json:"allowed_pools"`
}

type VirtualKeyResponse struct {
	ID            string         `json:"id"`
	DisplayPrefix string         `json:"display_prefix"`
	ClientID      string         `json:"client_id"`
	BudgetID      string         `json:"budget_id"`
	Mode          VKMode         `json:"mode"`
	AllowedModels []AllowedModel `json:"allowed_models,omitempty"`
	AllowedPools  []string       `json:"allowed_pools,omitempty"`
	IsActive      bool           `json:"is_active"`
	ExpiresAt     time.Time      `json:"expires_at"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}
