package core

import (
	"errors"
	"time"
)

type RateStatus string

const (
	RatePriced       RateStatus = "priced"
	RateExplicitFree RateStatus = "explicit_free"
	RateUnpriced     RateStatus = "unpriced"
)

type PricingVariant struct {
	ID        string      `json:"id,omitempty"`
	Source    string      `json:"source"`
	RawKey    string      `json:"raw_key"`
	Key       PriceKey    `json:"key"`
	ModelType ModelType   `json:"model_type"`
	Selectors SelectorSet `json:"selectors"`

	RateCard RateCard   `json:"rate_card"`
	Status   RateStatus `json:"status"`

	SourceRates           map[string]float64 `json:"source_rates,omitempty"`
	UnsupportedRateFields []string           `json:"unsupported_rate_fields,omitempty"`
}

func (v PricingVariant) Billable() bool {
	return v.Status == RatePriced || v.Status == RateExplicitFree
}

type Usage struct {
	Condition RateCondition `json:"condition,omitempty"`
	Qualifier string        `json:"qualifier,omitempty"`

	InputTokens         int64 `json:"input_tokens,omitempty"`
	OutputTokens        int64 `json:"output_tokens,omitempty"`
	ReasoningTokens     int64 `json:"reasoning_tokens,omitempty"`
	CachedInputTokens   int64 `json:"cached_input_tokens,omitempty"`
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	CitationTokens      int64 `json:"citation_tokens,omitempty"`

	InputAudioTokens         int64   `json:"input_audio_tokens,omitempty"`
	OutputAudioTokens        int64   `json:"output_audio_tokens,omitempty"`
	CachedAudioTokens        int64   `json:"cached_audio_tokens,omitempty"`
	CacheCreationAudioTokens int64   `json:"cache_creation_audio_tokens,omitempty"`
	InputAudioSeconds        float64 `json:"input_audio_seconds,omitempty"`

	InputCharacters  int64 `json:"input_characters,omitempty"`
	OutputCharacters int64 `json:"output_characters,omitempty"`

	InputImages       int64 `json:"input_images,omitempty"`
	OutputImages      int64 `json:"output_images,omitempty"`
	InputImageTokens  int64 `json:"input_image_tokens,omitempty"`
	OutputImageTokens int64 `json:"output_image_tokens,omitempty"`
	GeneratedPixels   int64 `json:"generated_pixels,omitempty"`
	OutputPixels      int64 `json:"output_pixels,omitempty"`

	InputSeconds      float64 `json:"input_seconds,omitempty"`
	OutputSeconds     float64 `json:"output_seconds,omitempty"`
	InputVideoSeconds float64 `json:"input_video_seconds,omitempty"`
	VideoSeconds      float64 `json:"video_seconds,omitempty"`
	VideoTokens       int64   `json:"video_tokens,omitempty"`

	Videos int64 `json:"videos,omitempty"`

	Queries         int64 `json:"queries,omitempty"`
	SearchQueries   int64 `json:"search_queries,omitempty"`
	Requests        int64 `json:"requests,omitempty"`
	Pages           int64 `json:"pages,omitempty"`
	AnnotationPages int64 `json:"annotation_pages,omitempty"`
	OCRCredits      int64 `json:"ocr_credits,omitempty"`
	CodeSessions    int64 `json:"code_sessions,omitempty"`
	InputDBUs       int64 `json:"input_dbus,omitempty"`
	OutputDBUs      int64 `json:"output_dbus,omitempty"`
	Units           int64 `json:"units,omitempty"`
}

func (u Usage) Quantity(meter Meter, splitCache bool) float64 {
	switch meter {
	case MeterInputToken:
		billable := u.InputTokens
		if splitCache {
			billable -= u.CachedInputTokens
		}
		if billable < 0 {
			billable = 0
		}
		return float64(billable)
	case MeterOutputToken:
		return float64(u.OutputTokens)
	case MeterReasoningToken:
		return float64(u.ReasoningTokens)
	case MeterCachedInputToken:
		return float64(u.CachedInputTokens)
	case MeterCacheCreationInputToken:
		return float64(u.CacheCreationTokens)
	case MeterCitationToken:
		return float64(u.CitationTokens)

	case MeterInputAudioToken:
		return float64(u.InputAudioTokens)
	case MeterOutputAudioToken:
		return float64(u.OutputAudioTokens)
	case MeterCachedAudioToken:
		return float64(u.CachedAudioTokens)
	case MeterCacheCreationAudioToken:
		return float64(u.CacheCreationAudioTokens)
	case MeterInputAudioSecond:
		return u.InputAudioSeconds

	case MeterInputCharacter:
		return float64(u.InputCharacters)
	case MeterOutputCharacter:
		return float64(u.OutputCharacters)

	case MeterInputImage:
		return float64(u.InputImages)
	case MeterOutputImage:
		return float64(u.OutputImages)
	case MeterInputImageToken:
		return float64(u.InputImageTokens)
	case MeterOutputImageToken:
		return float64(u.OutputImageTokens)
	case MeterGeneratedPixel:
		return float64(u.GeneratedPixels)
	case MeterOutputPixel:
		return float64(u.OutputPixels)

	case MeterInputSecond:
		return u.InputSeconds
	case MeterOutputSecond:
		return u.OutputSeconds
	case MeterInputVideoSecond:
		return u.InputVideoSeconds
	case MeterVideoSecond:
		return u.VideoSeconds
	case MeterVideoToken:
		return float64(u.VideoTokens)
	case MeterVideo:
		return float64(u.Videos)

	case MeterQuery:
		return float64(u.Queries)
	case MeterSearchQuery:
		return float64(u.SearchQueries)
	case MeterRequest:
		return float64(u.Requests)
	case MeterPage:
		return float64(u.Pages)
	case MeterAnnotationPage:
		return float64(u.AnnotationPages)
	case MeterOCRCredit:
		return float64(u.OCRCredits)
	case MeterCodeSession:
		return float64(u.CodeSessions)
	case MeterInputDBU:
		return float64(u.InputDBUs)
	case MeterOutputDBU:
		return float64(u.OutputDBUs)
	case MeterUnit:
		return float64(u.Units)
	default:
		return 0
	}
}

func CalculateCost(card RateCard, usage Usage) float64 {
	splitCache := card.HasMeter(MeterCachedInputToken)

	var total float64
	for _, meter := range card.Meters() {
		term, ok := card.Select(meter, usage.Condition, usage.InputTokens, usage.Qualifier)
		if !ok {
			continue
		}
		total += term.PriceUSD * usage.Quantity(meter, splitCache)
	}
	return total
}

var (
	ErrVariantRequired    = errors.New("model requires variant parameters")
	ErrUnsupportedVariant = errors.New("no price for the requested variant")
	ErrUnpricedVariant    = errors.New("variant has no billable rate")
)

type CustomPricing struct {
	ID        string    `json:"id,omitempty"`
	Name      string    `json:"name"`
	ModelName string    `json:"model_name"`
	ModelType ModelType `json:"model_type"`

	RateCard RateCard `json:"rate_card"`

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

type CustomPricingRequest struct {
	Name      string `json:"name"       binding:"required"`
	ModelName string `json:"model_name" binding:"required"`
	ModelType string `json:"model_type" binding:"required"`

	RateCard RateCard `json:"rate_card"`

	ScopeType         ScopeType `json:"scope_type"             binding:"required,oneof=global provider virtualkey"`
	ScopeVirtualkeyID *string   `json:"scope_virtual_key_id,omitempty"`
	ScopeProvider     *Provider `json:"scope_provider,omitempty"`
}

type CustomPricingResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ModelName string `json:"model_name"`
	ModelType string `json:"model_type"`

	RateCard RateCard `json:"rate_card"`

	ScopeType         ScopeType `json:"scope_type"`
	ScopeVirtualkeyID *string   `json:"scope_virtual_key_id,omitempty"`
	ScopeProvider     *Provider `json:"scope_provider,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
