package core

import (
	"errors"
	"time"
)

type Tier uint8

const (
	TierStandard Tier = iota
	TierPriority
	TierFlex
	TierBatch
)

func (t Tier) String() string {
	switch t {
	case TierPriority:
		return "priority"
	case TierFlex:
		return "flex"
	case TierBatch:
		return "batch"
	default:
		return "standard"
	}
}

func ParseTier(value string) Tier {
	switch value {
	case "priority":
		return TierPriority
	case "flex":
		return TierFlex
	case "batch", "batches":
		return TierBatch
	default:
		return TierStandard
	}
}

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

	OutputCostPerReasoningToken *float64 `json:"output_cost_per_reasoning_token,omitempty"`

	InputCostPerCharacter   *float64 `json:"input_cost_per_character,omitempty"`
	InputCostPerAudioSecond *float64 `json:"input_cost_per_audio_second,omitempty"`
	InputCostPerAudioToken  *float64 `json:"input_cost_per_audio_token,omitempty"`
	OutputCostPerAudioToken *float64 `json:"output_cost_per_audio_token,omitempty"`

	InputCostPerImage  *float64 `json:"input_cost_per_image,omitempty"`
	OutputCostPerImage *float64 `json:"output_cost_per_image,omitempty"`
	CostPerPixel       *float64 `json:"input_cost_per_pixel,omitempty"`

	OCRCostPerPage *float64 `json:"ocr_cost_per_page,omitempty"`
	CostPerQuery   *float64 `json:"input_cost_per_query,omitempty"`
}

func firstRate(rates ...*float64) float64 {
	for _, rate := range rates {
		if rate != nil {
			return *rate
		}
	}
	return 0
}

func (p Pricing) isLongContext(inputTokens int64) bool {
	return p.LongContextThreshold != nil && inputTokens >= int64(*p.LongContextThreshold)
}

func (p Pricing) inputRate(tier Tier, longContext bool) float64 {
	if longContext {
		return firstRate(p.InputCostPerTokenAboveTier, p.InputCostPerToken)
	}
	switch tier {
	case TierPriority:
		return firstRate(p.InputCostPerTokenPriority, p.InputCostPerToken)
	case TierFlex:
		return firstRate(p.InputCostPerTokenFlex, p.InputCostPerToken)
	case TierBatch:
		return firstRate(p.InputCostPerTokenBatch, p.InputCostPerToken)
	}
	return firstRate(p.InputCostPerToken)
}

func (p Pricing) outputRate(tier Tier, longContext bool) float64 {
	if longContext {
		return firstRate(p.OutputCostPerTokenAboveTier, p.OutputCostPerToken)
	}
	switch tier {
	case TierPriority:
		return firstRate(p.OutputCostPerTokenPriority, p.OutputCostPerToken)
	case TierFlex:
		return firstRate(p.OutputCostPerTokenFlex, p.OutputCostPerToken)
	case TierBatch:
		return firstRate(p.OutputCostPerTokenBatch, p.OutputCostPerToken)
	}
	return firstRate(p.OutputCostPerToken)
}

func (p Pricing) cacheReadRate(tier Tier, longContext bool) float64 {
	if longContext {
		return firstRate(p.CacheReadInputTokenCostAboveTier, p.CacheReadInputTokenCost)
	}
	switch tier {
	case TierPriority:
		return firstRate(p.CacheReadInputTokenCostPriority, p.CacheReadInputTokenCost)
	case TierFlex:
		return firstRate(p.CacheReadInputTokenCostFlex, p.CacheReadInputTokenCost)
	case TierBatch:
		return firstRate(p.CacheReadInputTokenCostBatch, p.CacheReadInputTokenCost)
	}
	return firstRate(p.CacheReadInputTokenCost)
}

type PricingVariant struct {
	ID        string      `json:"id,omitempty"`
	Source    string      `json:"source"`
	RawKey    string      `json:"raw_key"`
	Key       PriceKey    `json:"key"`
	ModelType ModelType   `json:"model_type"`
	Selectors SelectorSet `json:"selectors"`

	Pricing

	Billable bool `json:"billable"`
}

type Usage struct {
	Tier Tier `json:"tier,omitempty"`

	InputTokens       int64 `json:"input_tokens,omitempty"`
	OutputTokens      int64 `json:"output_tokens,omitempty"`
	ReasoningTokens   int64 `json:"reasoning_tokens,omitempty"`
	CachedInputTokens int64 `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64 `json:"cache_write_tokens,omitempty"`

	InputAudioTokens  int64   `json:"input_audio_tokens,omitempty"`
	OutputAudioTokens int64   `json:"output_audio_tokens,omitempty"`
	InputAudioSeconds float64 `json:"input_audio_seconds,omitempty"`
	InputCharacters   int64   `json:"input_characters,omitempty"`

	InputImages     int64 `json:"input_images,omitempty"`
	OutputImages    int64 `json:"output_images,omitempty"`
	GeneratedPixels int64 `json:"generated_pixels,omitempty"`

	Pages   int64 `json:"pages,omitempty"`
	Queries int64 `json:"queries,omitempty"`
}

func CalculateCost(pricing Pricing, usage Usage) float64 {
	tier := usage.Tier
	longContext := pricing.isLongContext(usage.InputTokens)
	uncachedInput := usage.InputTokens
	if pricing.CacheReadInputTokenCost != nil {
		uncachedInput -= usage.CachedInputTokens
		if uncachedInput < 0 {
			uncachedInput = 0
		}
	}

	total := float64(uncachedInput) * pricing.inputRate(tier, longContext)
	total += float64(usage.OutputTokens) * pricing.outputRate(tier, longContext)
	total += float64(usage.CachedInputTokens) * pricing.cacheReadRate(tier, longContext)

	if longContext {
		total += float64(usage.CacheWriteTokens) *
			firstRate(pricing.CacheCreationInputTokenCostAboveTier, pricing.CacheCreationInputTokenCost)
	} else {
		total += float64(usage.CacheWriteTokens) * firstRate(pricing.CacheCreationInputTokenCost)
	}

	total += float64(usage.ReasoningTokens) * firstRate(pricing.OutputCostPerReasoningToken)
	total += float64(usage.InputCharacters) * firstRate(pricing.InputCostPerCharacter)
	total += usage.InputAudioSeconds * firstRate(pricing.InputCostPerAudioSecond)
	total += float64(usage.InputAudioTokens) * firstRate(pricing.InputCostPerAudioToken)
	total += float64(usage.OutputAudioTokens) * firstRate(pricing.OutputCostPerAudioToken)

	total += float64(usage.InputImages) * firstRate(pricing.InputCostPerImage)
	total += float64(usage.OutputImages) * firstRate(pricing.OutputCostPerImage)
	total += float64(usage.GeneratedPixels) * firstRate(pricing.CostPerPixel)

	total += float64(usage.Pages) * firstRate(pricing.OCRCostPerPage)
	total += float64(usage.Queries) * firstRate(pricing.CostPerQuery)

	return total
}

var (
	ErrVariantRequired    = errors.New("model requires variant parameters")
	ErrUnsupportedVariant = errors.New("no price for the requested variant")
	ErrUnpricedVariant    = errors.New("variant has no billable rate")
)

// ------------- Operator overrides ----------------------

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
	override := func(dst **float64, src *float64) {
		if src != nil {
			*dst = src
		}
	}

	override(&merged.InputCostPerToken, custom.InputCostPerToken)
	override(&merged.OutputCostPerToken, custom.OutputCostPerToken)
	override(&merged.CacheReadInputTokenCost, custom.CacheReadInputTokenCost)
	override(&merged.CacheCreationInputTokenCost, custom.CacheCreationInputTokenCost)
	override(&merged.CacheCreationInputTokenCost1Hr, custom.CacheCreationInputTokenCost1Hr)

	override(&merged.InputCostPerTokenPriority, custom.InputCostPerTokenPriority)
	override(&merged.OutputCostPerTokenPriority, custom.OutputCostPerTokenPriority)
	override(&merged.CacheReadInputTokenCostPriority, custom.CacheReadInputTokenCostPriority)

	override(&merged.InputCostPerTokenFlex, custom.InputCostPerTokenFlex)
	override(&merged.OutputCostPerTokenFlex, custom.OutputCostPerTokenFlex)
	override(&merged.CacheReadInputTokenCostFlex, custom.CacheReadInputTokenCostFlex)

	override(&merged.InputCostPerTokenBatch, custom.InputCostPerTokenBatch)
	override(&merged.OutputCostPerTokenBatch, custom.OutputCostPerTokenBatch)
	override(&merged.CacheReadInputTokenCostBatch, custom.CacheReadInputTokenCostBatch)

	if custom.LongContextThreshold != nil {
		merged.LongContextThreshold = custom.LongContextThreshold
	}
	override(&merged.InputCostPerTokenAboveTier, custom.InputCostPerTokenAboveTier)
	override(&merged.OutputCostPerTokenAboveTier, custom.OutputCostPerTokenAboveTier)
	override(&merged.CacheReadInputTokenCostAboveTier, custom.CacheReadInputTokenCostAboveTier)
	override(&merged.CacheCreationInputTokenCostAboveTier, custom.CacheCreationInputTokenCostAboveTier)

	override(&merged.OutputCostPerReasoningToken, custom.OutputCostPerReasoningToken)

	override(&merged.InputCostPerCharacter, custom.InputCostPerCharacter)
	override(&merged.InputCostPerAudioSecond, custom.InputCostPerAudioSecond)
	override(&merged.InputCostPerAudioToken, custom.InputCostPerAudioToken)
	override(&merged.OutputCostPerAudioToken, custom.OutputCostPerAudioToken)

	override(&merged.InputCostPerImage, custom.InputCostPerImage)
	override(&merged.OutputCostPerImage, custom.OutputCostPerImage)
	override(&merged.CostPerPixel, custom.CostPerPixel)

	override(&merged.OCRCostPerPage, custom.OCRCostPerPage)
	override(&merged.CostPerQuery, custom.CostPerQuery)

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
