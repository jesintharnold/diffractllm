package core

import (
	"time"
)

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

type Budget struct {
	Name           string `json:"name"            binding:"required"`
	BudgetLimit    int64  `json:"budget_limit"    binding:"required,gt=0"`
	BudgetUnit     string `json:"budget_unit"`
	BudgetDuration int64  `json:"budget_duration"`
	Enforce        bool   `json:"enforce"`
}

type BudgetResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	BudgetLimit    int64  `json:"budget_limit"`
	BudgetUnit     string `json:"budget_unit"`
	BudgetDuration int64  `json:"budget_duration"`
	Enforce        bool   `json:"enforce"`
	TotalSpend     int64  `json:"total_spend"`
	RequestCount   int64  `json:"request_count"`
	Status         string `json:"status"`
}

type AllowedModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Weight   int    `json:"weight,omitempty"`
}

type VirtualKey struct {
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

// ------------- Pricing ----------------------

type Pricing struct {
	InputCostPerToken              *float64 `json:"input_cost_per_token,omitempty"`
	OutputCostPerToken             *float64 `json:"output_cost_per_token,omitempty"`
	CacheReadInputTokenCost        *float64 `json:"cache_read_input_token_cost,omitempty"`
	CacheCreationInputTokenCost    *float64 `json:"cache_creation_input_token_cost,omitempty"`
	CacheCreationInputTokenCost1Hr *float64 `json:"cache_creation_input_token_cost_1hr,omitempty"`

	InputCostPerTokenPriority       *float64 `json:"input_cost_per_token_priority,omitempty"`
	OutputCostPerTokenPriority      *float64 `json:"output_cost_per_token_priority,omitempty"`
	CacheReadInputTokenCostPriority *float64 `json:"cache_read_input_token_cost_priority,omitempty"`

	InputCostPerTokenFlex       *float64 `json:"input_cost_per_token_flex,omitempty"`
	OutputCostPerTokenFlex      *float64 `json:"output_cost_per_token_flex,omitempty"`
	CacheReadInputTokenCostFlex *float64 `json:"cache_read_input_token_cost_flex,omitempty"`

	InputCostPerTokenBatch       *float64 `json:"input_cost_per_token_batch,omitempty"`
	OutputCostPerTokenBatch      *float64 `json:"output_cost_per_token_batch,omitempty"`
	CacheReadInputTokenCostBatch *float64 `json:"cache_read_input_token_cost_batch,omitempty"`

	LongContextThreshold                 *int     `json:"long_context_threshold,omitempty"`
	InputCostPerTokenAboveTier           *float64 `json:"input_cost_per_token_above_tier,omitempty"`
	OutputCostPerTokenAboveTier          *float64 `json:"output_cost_per_token_above_tier,omitempty"`
	CacheReadInputTokenCostAboveTier     *float64 `json:"cache_read_input_token_cost_above_tier,omitempty"`
	CacheCreationInputTokenCostAboveTier *float64 `json:"cache_creation_input_token_cost_above_tier,omitempty"`

	InputCostPerCharacter   *float64 `json:"input_cost_per_character,omitempty"`
	InputCostPerAudioSecond *float64 `json:"input_cost_per_audio_second,omitempty"`
	InputCostPerAudioToken  *float64 `json:"input_cost_per_audio_token,omitempty"`
	OutputCostPerAudioToken *float64 `json:"output_cost_per_audio_token,omitempty"`
}

type ScopeType string

const (
	ScopeGlobal     ScopeType = "global"
	ScopeProvider   ScopeType = "provider"
	ScopeVirtualKey ScopeType = "virtualkey"
)

type ModelPricing struct {
	ID        string   `json:"id,omitempty"`
	ModelName string   `json:"model_name"`
	Provider  Provider `json:"provider"`
	ModelType string   `json:"model_type"`

	Pricing

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CustomPricing struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	ModelName string `json:"model_name"`
	ModelType string `json:"model_type"`

	Pricing

	ScopeType         ScopeType `json:"scope_type"`
	ScopeVirtualkeyID *string   `json:"scope_virtual_key_id,omitempty"`
	ScopeProvider     *Provider `json:"scope_provider,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CustomPricingRequest struct {
	Name      string `json:"name"       binding:"required"`
	ModelName string `json:"model_name" binding:"required"`
	ModelType string `json:"model_type" binding:"required"`

	Pricing

	ScopeType         ScopeType `json:"scope_type"             binding:"required,oneof=global provider virtualkey"`
	ScopeVirtualkeyID *string   `json:"scope_virtual_key_id,omitempty"`
	ScopeProvider     *Provider `json:"scope_provider,omitempty"`
}

type CustomPricingResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ModelName string `json:"model_name"`
	ModelType string `json:"model_type"`

	Pricing

	ScopeType         ScopeType `json:"scope_type"`
	ScopeVirtualkeyID *string   `json:"scope_virtual_key_id,omitempty"`
	ScopeProvider     *Provider `json:"scope_provider,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
