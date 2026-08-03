package core

import (
	"time"
)

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

type BasePricing struct {
	ID        string    `json:"id,omitempty"`
	ModelName string    `json:"model_name"`
	Provider  Provider  `json:"provider"`
	ModelType ModelType `json:"model_type"`

	Pricing

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CustomPricing struct {
	ID        string    `json:"id,omitempty"`
	Name      string    `json:"name"`
	ModelName string    `json:"model_name"`
	ModelType ModelType `json:"model_type"`

	Pricing

	ScopeType         ScopeType `json:"scope_type"`
	ScopeVirtualkeyID *string   `json:"scope_virtual_key_id,omitempty"`
	ScopeProvider     *Provider `json:"scope_provider,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CustomScopePricing struct {
	Global     *CustomPricing
	Provider   map[Provider]*CustomPricing
	VirtualKey map[string]*CustomPricing
}

func MergePricing(base *Pricing, custom *Pricing) *Pricing {
	merged := *base
	if custom.InputCostPerToken != nil {
		merged.InputCostPerToken = custom.InputCostPerToken
	}
	if custom.OutputCostPerToken != nil {
		merged.OutputCostPerToken = custom.OutputCostPerToken
	}
	if custom.CacheReadInputTokenCost != nil {
		merged.CacheReadInputTokenCost = custom.CacheReadInputTokenCost
	}
	if custom.CacheCreationInputTokenCost != nil {
		merged.CacheCreationInputTokenCost = custom.CacheCreationInputTokenCost
	}
	if custom.CacheCreationInputTokenCost1Hr != nil {
		merged.CacheCreationInputTokenCost1Hr = custom.CacheCreationInputTokenCost1Hr
	}
	if custom.InputCostPerTokenPriority != nil {
		merged.InputCostPerTokenPriority = custom.InputCostPerTokenPriority
	}
	if custom.OutputCostPerTokenPriority != nil {
		merged.OutputCostPerTokenPriority = custom.OutputCostPerTokenPriority
	}
	if custom.CacheReadInputTokenCostPriority != nil {
		merged.CacheReadInputTokenCostPriority = custom.CacheReadInputTokenCostPriority
	}
	if custom.InputCostPerTokenFlex != nil {
		merged.InputCostPerTokenFlex = custom.InputCostPerTokenFlex
	}
	if custom.OutputCostPerTokenFlex != nil {
		merged.OutputCostPerTokenFlex = custom.OutputCostPerTokenFlex
	}
	if custom.CacheReadInputTokenCostFlex != nil {
		merged.CacheReadInputTokenCostFlex = custom.CacheReadInputTokenCostFlex
	}
	if custom.InputCostPerTokenBatch != nil {
		merged.InputCostPerTokenBatch = custom.InputCostPerTokenBatch
	}
	if custom.OutputCostPerTokenBatch != nil {
		merged.OutputCostPerTokenBatch = custom.OutputCostPerTokenBatch
	}
	if custom.CacheReadInputTokenCostBatch != nil {
		merged.CacheReadInputTokenCostBatch = custom.CacheReadInputTokenCostBatch
	}
	if custom.LongContextThreshold != nil {
		merged.LongContextThreshold = custom.LongContextThreshold
	}
	if custom.InputCostPerTokenAboveTier != nil {
		merged.InputCostPerTokenAboveTier = custom.InputCostPerTokenAboveTier
	}
	if custom.OutputCostPerTokenAboveTier != nil {
		merged.OutputCostPerTokenAboveTier = custom.OutputCostPerTokenAboveTier
	}
	if custom.CacheReadInputTokenCostAboveTier != nil {
		merged.CacheReadInputTokenCostAboveTier = custom.CacheReadInputTokenCostAboveTier
	}
	if custom.CacheCreationInputTokenCostAboveTier != nil {
		merged.CacheCreationInputTokenCostAboveTier = custom.CacheCreationInputTokenCostAboveTier
	}
	if custom.InputCostPerCharacter != nil {
		merged.InputCostPerCharacter = custom.InputCostPerCharacter
	}
	if custom.InputCostPerAudioSecond != nil {
		merged.InputCostPerAudioSecond = custom.InputCostPerAudioSecond
	}
	if custom.InputCostPerAudioToken != nil {
		merged.InputCostPerAudioToken = custom.InputCostPerAudioToken
	}
	if custom.OutputCostPerAudioToken != nil {
		merged.OutputCostPerAudioToken = custom.OutputCostPerAudioToken
	}
	return &merged
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
