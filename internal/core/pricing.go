package core

import (
	"errors"
	"reflect"
	"strings"
	"time"
)

const (
	BandAbove128K int64 = 128_000
	BandAbove200K int64 = 200_000
	BandAbove256K int64 = 256_000
	BandAbove272K int64 = 272_000
	BandAbove512K int64 = 512_000
)

const (
	PixelsAbove1024 int64 = 1024 * 1024
	PixelsAbove2048 int64 = 2048 * 2048
	PixelsAbove4096 int64 = 4096 * 4096
)

func PricingFieldNames() map[string]struct{} {
	pricingType := reflect.TypeFor[Pricing]()
	tags := make(map[string]struct{}, pricingType.NumField())
	for i := 0; i < pricingType.NumField(); i++ {
		tag := pricingType.Field(i).Tag.Get("json")
		if name, _, _ := strings.Cut(tag, ","); name != "" && name != "-" {
			tags[name] = struct{}{}
		}
	}
	return tags
}

type ServiceTier uint8

const (
	TierStandard ServiceTier = iota
	TierPriority
	TierFlex
	TierBatch
	TierFast
)

func (t ServiceTier) String() string {
	switch t {
	case TierPriority:
		return "priority"
	case TierFlex:
		return "flex"
	case TierBatch:
		return "batch"
	case TierFast:
		return "fast"
	default:
		return "standard"
	}
}

func ParseServiceTier(value string) ServiceTier {
	switch value {
	case "priority":
		return TierPriority
	case "flex":
		return TierFlex
	case "batch", "batches":
		return TierBatch
	case "fast":
		return TierFast
	default:
		return TierStandard
	}
}

type Pricing struct {
	// ---------------- text tokens ----------------
	InputCostPerToken  *float64 `json:"input_cost_per_token,omitempty"`
	OutputCostPerToken *float64 `json:"output_cost_per_token,omitempty"`

	InputCostPerTokenPriority  *float64 `json:"input_cost_per_token_priority,omitempty"`
	OutputCostPerTokenPriority *float64 `json:"output_cost_per_token_priority,omitempty"`
	InputCostPerTokenFlex      *float64 `json:"input_cost_per_token_flex,omitempty"`
	OutputCostPerTokenFlex     *float64 `json:"output_cost_per_token_flex,omitempty"`
	InputCostPerTokenBatches   *float64 `json:"input_cost_per_token_batches,omitempty"`
	OutputCostPerTokenBatches  *float64 `json:"output_cost_per_token_batches,omitempty"`
	InputCostPerTokenFast      *float64 `json:"input_cost_per_token_fast,omitempty"`
	OutputCostPerTokenFast     *float64 `json:"output_cost_per_token_fast,omitempty"`

	InputCostPerTokenAbove128kTokens  *float64 `json:"input_cost_per_token_above_128k_tokens,omitempty"`
	OutputCostPerTokenAbove128kTokens *float64 `json:"output_cost_per_token_above_128k_tokens,omitempty"`
	InputCostPerTokenAbove200kTokens  *float64 `json:"input_cost_per_token_above_200k_tokens,omitempty"`
	OutputCostPerTokenAbove200kTokens *float64 `json:"output_cost_per_token_above_200k_tokens,omitempty"`
	InputCostPerTokenAbove256kTokens  *float64 `json:"input_cost_per_token_above_256k_tokens,omitempty"`
	OutputCostPerTokenAbove256kTokens *float64 `json:"output_cost_per_token_above_256k_tokens,omitempty"`
	InputCostPerTokenAbove272kTokens  *float64 `json:"input_cost_per_token_above_272k_tokens,omitempty"`
	OutputCostPerTokenAbove272kTokens *float64 `json:"output_cost_per_token_above_272k_tokens,omitempty"`
	InputCostPerTokenAbove512kTokens  *float64 `json:"input_cost_per_token_above_512k_tokens,omitempty"`
	OutputCostPerTokenAbove512kTokens *float64 `json:"output_cost_per_token_above_512k_tokens,omitempty"`

	InputCostPerTokenAbove200kTokensPriority  *float64 `json:"input_cost_per_token_above_200k_tokens_priority,omitempty"`
	OutputCostPerTokenAbove200kTokensPriority *float64 `json:"output_cost_per_token_above_200k_tokens_priority,omitempty"`
	InputCostPerTokenAbove272kTokensPriority  *float64 `json:"input_cost_per_token_above_272k_tokens_priority,omitempty"`
	OutputCostPerTokenAbove272kTokensPriority *float64 `json:"output_cost_per_token_above_272k_tokens_priority,omitempty"`
	InputCostPerTokenAbove272kTokensFlex      *float64 `json:"input_cost_per_token_above_272k_tokens_flex,omitempty"`
	OutputCostPerTokenAbove272kTokensFlex     *float64 `json:"output_cost_per_token_above_272k_tokens_flex,omitempty"`
	InputCostPerTokenFlexAbove272kTokens      *float64 `json:"input_cost_per_token_flex_above_272k_tokens,omitempty"`
	OutputCostPerTokenFlexAbove272kTokens     *float64 `json:"output_cost_per_token_flex_above_272k_tokens,omitempty"`
	InputCostPerTokenBatchesAbove272kTokens   *float64 `json:"input_cost_per_token_batches_above_272k_tokens,omitempty"`
	OutputCostPerTokenBatchesAbove272kTokens  *float64 `json:"output_cost_per_token_batches_above_272k_tokens,omitempty"`

	// ---------------- cache read ----------------
	CacheReadInputTokenCost                        *float64 `json:"cache_read_input_token_cost,omitempty"`
	CacheReadInputTokenCostPriority                *float64 `json:"cache_read_input_token_cost_priority,omitempty"`
	CacheReadInputTokenCostFlex                    *float64 `json:"cache_read_input_token_cost_flex,omitempty"`
	CacheReadInputTokenCostBatches                 *float64 `json:"cache_read_input_token_cost_batches,omitempty"`
	CacheReadInputTokenCostFast                    *float64 `json:"cache_read_input_token_cost_fast,omitempty"`
	CacheReadInputTokenCostAbove200kTokens         *float64 `json:"cache_read_input_token_cost_above_200k_tokens,omitempty"`
	CacheReadInputTokenCostAbove272kTokens         *float64 `json:"cache_read_input_token_cost_above_272k_tokens,omitempty"`
	CacheReadInputTokenCostAbove512kTokens         *float64 `json:"cache_read_input_token_cost_above_512k_tokens,omitempty"`
	CacheReadInputTokenCostAbove200kTokensPriority *float64 `json:"cache_read_input_token_cost_above_200k_tokens_priority,omitempty"`
	CacheReadInputTokenCostAbove272kTokensPriority *float64 `json:"cache_read_input_token_cost_above_272k_tokens_priority,omitempty"`
	CacheReadInputTokenCostAbove272kTokensFlex     *float64 `json:"cache_read_input_token_cost_above_272k_tokens_flex,omitempty"`
	CacheReadInputTokenCostFlexAbove272kTokens     *float64 `json:"cache_read_input_token_cost_flex_above_272k_tokens,omitempty"`
	CacheReadInputTokenCostBatchesAbove272kTokens  *float64 `json:"cache_read_input_token_cost_batches_above_272k_tokens,omitempty"`

	// DeepSeek spells a cache read this way.
	InputCostPerTokenCacheHit *float64 `json:"input_cost_per_token_cache_hit,omitempty"`

	// ---------------- cache creation ----------------
	CacheCreationInputTokenCost                        *float64 `json:"cache_creation_input_token_cost,omitempty"`
	CacheCreationInputTokenCostPriority                *float64 `json:"cache_creation_input_token_cost_priority,omitempty"`
	CacheCreationInputTokenCostFlex                    *float64 `json:"cache_creation_input_token_cost_flex,omitempty"`
	CacheCreationInputTokenCostBatches                 *float64 `json:"cache_creation_input_token_cost_batches,omitempty"`
	CacheCreationInputTokenCostFast                    *float64 `json:"cache_creation_input_token_cost_fast,omitempty"`
	CacheCreationInputTokenCostAbove200kTokens         *float64 `json:"cache_creation_input_token_cost_above_200k_tokens,omitempty"`
	CacheCreationInputTokenCostAbove272kTokens         *float64 `json:"cache_creation_input_token_cost_above_272k_tokens,omitempty"`
	CacheCreationInputTokenCostAbove272kTokensFlex     *float64 `json:"cache_creation_input_token_cost_above_272k_tokens_flex,omitempty"`
	CacheCreationInputTokenCostFlexAbove272kTokens     *float64 `json:"cache_creation_input_token_cost_flex_above_272k_tokens,omitempty"`
	CacheCreationInputTokenCostBatchesAbove272kTokens  *float64 `json:"cache_creation_input_token_cost_batches_above_272k_tokens,omitempty"`
	CacheCreationInputTokenCostAbove1hr                *float64 `json:"cache_creation_input_token_cost_above_1hr,omitempty"`
	CacheCreationInputTokenCostAbove1hrAbove200kTokens *float64 `json:"cache_creation_input_token_cost_above_1hr_above_200k_tokens,omitempty"`
	CacheCreationInputTokenCostAbove1hrFast            *float64 `json:"cache_creation_input_token_cost_above_1hr_fast,omitempty"`
	CacheCreationInputTokenCostBatches1h               *float64 `json:"cache_creation_input_token_cost_batches_1h,omitempty"`

	// ---------------- reasoning and citations ----------------
	OutputCostPerReasoningToken *float64 `json:"output_cost_per_reasoning_token,omitempty"`
	CitationCostPerToken        *float64 `json:"citation_cost_per_token,omitempty"`

	// ---------------- audio ----------------
	InputCostPerAudioToken                    *float64 `json:"input_cost_per_audio_token,omitempty"`
	OutputCostPerAudioToken                   *float64 `json:"output_cost_per_audio_token,omitempty"`
	InputCostPerAudioTokenPriority            *float64 `json:"input_cost_per_audio_token_priority,omitempty"`
	CacheReadInputAudioTokenCost              *float64 `json:"cache_read_input_audio_token_cost,omitempty"`
	CacheCreationInputAudioTokenCost          *float64 `json:"cache_creation_input_audio_token_cost,omitempty"`
	InputCostPerAudioPerSecond                *float64 `json:"input_cost_per_audio_per_second,omitempty"`
	InputCostPerAudioPerSecondAbove128kTokens *float64 `json:"input_cost_per_audio_per_second_above_128k_tokens,omitempty"`

	// ---------------- characters ----------------
	InputCostPerCharacter                 *float64 `json:"input_cost_per_character,omitempty"`
	OutputCostPerCharacter                *float64 `json:"output_cost_per_character,omitempty"`
	InputCostPerCharacterAbove128kTokens  *float64 `json:"input_cost_per_character_above_128k_tokens,omitempty"`
	OutputCostPerCharacterAbove128kTokens *float64 `json:"output_cost_per_character_above_128k_tokens,omitempty"`

	// ---------------- images ----------------
	InputCostPerImage                *float64 `json:"input_cost_per_image,omitempty"`
	OutputCostPerImage               *float64 `json:"output_cost_per_image,omitempty"`
	InputCostPerImageAbove128kTokens *float64 `json:"input_cost_per_image_above_128k_tokens,omitempty"`
	InputCostPerImageToken           *float64 `json:"input_cost_per_image_token,omitempty"`
	OutputCostPerImageToken          *float64 `json:"output_cost_per_image_token,omitempty"`

	InputCostPerPixel  *float64 `json:"input_cost_per_pixel,omitempty"`
	OutputCostPerPixel *float64 `json:"output_cost_per_pixel,omitempty"`

	// Quality tiers, selected by the request's quality parameter.
	OutputCostPerImageLowQuality    *float64 `json:"output_cost_per_image_low_quality,omitempty"`
	OutputCostPerImageMediumQuality *float64 `json:"output_cost_per_image_medium_quality,omitempty"`
	OutputCostPerImageHighQuality   *float64 `json:"output_cost_per_image_high_quality,omitempty"`
	OutputCostPerImageAutoQuality   *float64 `json:"output_cost_per_image_auto_quality,omitempty"`

	// Pixel bands, applied strictly above the named square. The feed spells
	// some as a single dimension and some as both; both forms are live.
	OutputCostPerImageAbove1024And1024Pixels *float64 `json:"output_cost_per_image_above_1024_and_1024_pixels,omitempty"`
	OutputCostPerImageAbove2048And2048Pixels *float64 `json:"output_cost_per_image_above_2048_and_2048_pixels,omitempty"`
	OutputCostPerImageAbove4096And4096Pixels *float64 `json:"output_cost_per_image_above_4096_and_4096_pixels,omitempty"`
	OutputCostPerImageAbove2048Pixels        *float64 `json:"output_cost_per_image_above_2048_pixels,omitempty"`
	OutputCostPerImageAbove4096Pixels        *float64 `json:"output_cost_per_image_above_4096_pixels,omitempty"`

	// ---------------- video and time ----------------
	InputCostPerVideoPerSecond                *float64 `json:"input_cost_per_video_per_second,omitempty"`
	InputCostPerVideoPerSecondAbove128kTokens *float64 `json:"input_cost_per_video_per_second_above_128k_tokens,omitempty"`
	OutputCostPerVideoPerSecond               *float64 `json:"output_cost_per_video_per_second,omitempty"`
	OutputCostPerVideoToken                   *float64 `json:"output_cost_per_video_token,omitempty"`
	OutputCostPerVideo                        *float64 `json:"output_cost_per_video,omitempty"`
	InputCostPerSecond                        *float64 `json:"input_cost_per_second,omitempty"`
	OutputCostPerSecond                       *float64 `json:"output_cost_per_second,omitempty"`

	// Input video billed by clip length band.
	InputCostPerVideoPerSecondAbove8sInterval  *float64 `json:"input_cost_per_video_per_second_above_8s_interval,omitempty"`
	InputCostPerVideoPerSecondAbove15sInterval *float64 `json:"input_cost_per_video_per_second_above_15s_interval,omitempty"`

	// Output video per second by resolution. _audio and _video_in are
	// surcharged variants of the same resolution, not separate resolutions.
	OutputCostPerVideoPerSecond360p          *float64 `json:"output_cost_per_video_per_second_360p,omitempty"`
	OutputCostPerVideoPerSecond360pAudio     *float64 `json:"output_cost_per_video_per_second_360p_audio,omitempty"`
	OutputCostPerVideoPerSecond480p          *float64 `json:"output_cost_per_video_per_second_480p,omitempty"`
	OutputCostPerVideoPerSecond480pVideoIn   *float64 `json:"output_cost_per_video_per_second_480p_video_in,omitempty"`
	OutputCostPerVideoPerSecond540p          *float64 `json:"output_cost_per_video_per_second_540p,omitempty"`
	OutputCostPerVideoPerSecond540pAudio     *float64 `json:"output_cost_per_video_per_second_540p_audio,omitempty"`
	OutputCostPerVideoPerSecond720p          *float64 `json:"output_cost_per_video_per_second_720p,omitempty"`
	OutputCostPerVideoPerSecond720pAudio     *float64 `json:"output_cost_per_video_per_second_720p_audio,omitempty"`
	OutputCostPerVideoPerSecond720pVideoIn   *float64 `json:"output_cost_per_video_per_second_720p_video_in,omitempty"`
	OutputCostPerVideoPerSecond1080p         *float64 `json:"output_cost_per_video_per_second_1080p,omitempty"`
	OutputCostPerVideoPerSecond1080pAudio    *float64 `json:"output_cost_per_video_per_second_1080p_audio,omitempty"`
	OutputCostPerVideoPerSecond1080pVideoIn  *float64 `json:"output_cost_per_video_per_second_1080p_video_in,omitempty"`
	OutputCostPerVideoPerSecond4k            *float64 `json:"output_cost_per_video_per_second_4k,omitempty"`
	OutputCostPerVideoPerSecond4kAudio       *float64 `json:"output_cost_per_video_per_second_4k_audio,omitempty"`
	OutputCostPerVideoPerSecond4kVideoIn     *float64 `json:"output_cost_per_video_per_second_4k_video_in,omitempty"`
	OutputCostPerVideoPerSecondPro           *float64 `json:"output_cost_per_video_per_second_pro,omitempty"`
	OutputCostPerVideoPerSecondProAudio      *float64 `json:"output_cost_per_video_per_second_pro_audio,omitempty"`
	OutputCostPerVideoPerSecondStandard      *float64 `json:"output_cost_per_video_per_second_standard,omitempty"`
	OutputCostPerVideoPerSecondStandardAudio *float64 `json:"output_cost_per_video_per_second_standard_audio,omitempty"`
	OutputCostPerSecond1080p                 *float64 `json:"output_cost_per_second_1080p,omitempty"`

	// Flat per-video prices for a fixed resolution and duration.
	OutputCostPerVideo768p6s  *float64 `json:"output_cost_per_video_768p_6s,omitempty"`
	OutputCostPerVideo768p10s *float64 `json:"output_cost_per_video_768p_10s,omitempty"`
	OutputCostPerVideo1080p6s *float64 `json:"output_cost_per_video_1080p_6s,omitempty"`

	// ---------------- per-unit ----------------
	InputCostPerQuery               *float64 `json:"input_cost_per_query,omitempty"`
	SearchContextCostPerQueryLow    *float64 `json:"search_context_cost_per_query_low,omitempty"`
	SearchContextCostPerQueryMedium *float64 `json:"search_context_cost_per_query_medium,omitempty"`
	SearchContextCostPerQueryHigh   *float64 `json:"search_context_cost_per_query_high,omitempty"`
	CostPerRequest                  *float64 `json:"cost_per_request,omitempty"`
	InputCostPerRequest             *float64 `json:"input_cost_per_request,omitempty"`
	OCRCostPerPage                  *float64 `json:"ocr_cost_per_page,omitempty"`
	OCRCostPerCredit                *float64 `json:"ocr_cost_per_credit,omitempty"`
	AnnotationCostPerPage           *float64 `json:"annotation_cost_per_page,omitempty"`
	CodeInterpreterCostPerSession   *float64 `json:"code_interpreter_cost_per_session,omitempty"`
	InputDBUCostPerToken            *float64 `json:"input_dbu_cost_per_token,omitempty"`
	OutputDBUCostPerToken           *float64 `json:"output_dbu_cost_per_token,omitempty"`
	OutputCostPerUnit               *float64 `json:"output_cost_per_unit,omitempty"`
}

func rate(candidates ...*float64) float64 {
	for _, candidate := range candidates {
		if candidate != nil {
			return *candidate
		}
	}
	return 0
}

func (p *Pricing) inputRate(totalTokens int64, tier ServiceTier) float64 {
	switch tier {
	case TierFast:
		if p.InputCostPerTokenFast != nil {
			return *p.InputCostPerTokenFast
		}
	case TierFlex:
		if totalTokens > BandAbove272K {
			if v := rate(p.InputCostPerTokenAbove272kTokensFlex, p.InputCostPerTokenFlexAbove272kTokens); v != 0 {
				return v
			}
		}
		if p.InputCostPerTokenFlex != nil {
			return *p.InputCostPerTokenFlex
		}
	case TierBatch:
		if totalTokens > BandAbove272K && p.InputCostPerTokenBatchesAbove272kTokens != nil {
			return *p.InputCostPerTokenBatchesAbove272kTokens
		}
		if p.InputCostPerTokenBatches != nil {
			return *p.InputCostPerTokenBatches
		}
	}
	if totalTokens > BandAbove512K && p.InputCostPerTokenAbove512kTokens != nil {
		return *p.InputCostPerTokenAbove512kTokens
	}
	if totalTokens > BandAbove272K {
		if tier == TierPriority && p.InputCostPerTokenAbove272kTokensPriority != nil {
			return *p.InputCostPerTokenAbove272kTokensPriority
		}
		if p.InputCostPerTokenAbove272kTokens != nil {
			return *p.InputCostPerTokenAbove272kTokens
		}
	}
	if totalTokens > BandAbove256K && p.InputCostPerTokenAbove256kTokens != nil {
		return *p.InputCostPerTokenAbove256kTokens
	}
	if totalTokens > BandAbove200K {
		if tier == TierPriority && p.InputCostPerTokenAbove200kTokensPriority != nil {
			return *p.InputCostPerTokenAbove200kTokensPriority
		}
		if p.InputCostPerTokenAbove200kTokens != nil {
			return *p.InputCostPerTokenAbove200kTokens
		}
	}
	if totalTokens > BandAbove128K && p.InputCostPerTokenAbove128kTokens != nil {
		return *p.InputCostPerTokenAbove128kTokens
	}
	if tier == TierPriority && p.InputCostPerTokenPriority != nil {
		return *p.InputCostPerTokenPriority
	}
	return rate(p.InputCostPerToken)
}

func (p *Pricing) outputRate(totalTokens int64, tier ServiceTier) float64 {
	switch tier {
	case TierFast:
		if p.OutputCostPerTokenFast != nil {
			return *p.OutputCostPerTokenFast
		}
	case TierFlex:
		if totalTokens > BandAbove272K {
			if v := rate(p.OutputCostPerTokenAbove272kTokensFlex, p.OutputCostPerTokenFlexAbove272kTokens); v != 0 {
				return v
			}
		}
		if p.OutputCostPerTokenFlex != nil {
			return *p.OutputCostPerTokenFlex
		}
	case TierBatch:
		if totalTokens > BandAbove272K && p.OutputCostPerTokenBatchesAbove272kTokens != nil {
			return *p.OutputCostPerTokenBatchesAbove272kTokens
		}
		if p.OutputCostPerTokenBatches != nil {
			return *p.OutputCostPerTokenBatches
		}
	}
	if totalTokens > BandAbove512K && p.OutputCostPerTokenAbove512kTokens != nil {
		return *p.OutputCostPerTokenAbove512kTokens
	}
	if totalTokens > BandAbove272K {
		if tier == TierPriority && p.OutputCostPerTokenAbove272kTokensPriority != nil {
			return *p.OutputCostPerTokenAbove272kTokensPriority
		}
		if p.OutputCostPerTokenAbove272kTokens != nil {
			return *p.OutputCostPerTokenAbove272kTokens
		}
	}
	if totalTokens > BandAbove256K && p.OutputCostPerTokenAbove256kTokens != nil {
		return *p.OutputCostPerTokenAbove256kTokens
	}
	if totalTokens > BandAbove200K {
		if tier == TierPriority && p.OutputCostPerTokenAbove200kTokensPriority != nil {
			return *p.OutputCostPerTokenAbove200kTokensPriority
		}
		if p.OutputCostPerTokenAbove200kTokens != nil {
			return *p.OutputCostPerTokenAbove200kTokens
		}
	}
	if totalTokens > BandAbove128K && p.OutputCostPerTokenAbove128kTokens != nil {
		return *p.OutputCostPerTokenAbove128kTokens
	}
	if tier == TierPriority && p.OutputCostPerTokenPriority != nil {
		return *p.OutputCostPerTokenPriority
	}
	return rate(p.OutputCostPerToken)
}

func (p *Pricing) cacheReadRate(totalTokens int64, tier ServiceTier) float64 {
	switch tier {
	case TierFast:
		if p.CacheReadInputTokenCostFast != nil {
			return *p.CacheReadInputTokenCostFast
		}
	case TierFlex:
		if totalTokens > BandAbove272K {
			if v := rate(p.CacheReadInputTokenCostAbove272kTokensFlex, p.CacheReadInputTokenCostFlexAbove272kTokens); v != 0 {
				return v
			}
		}
		if p.CacheReadInputTokenCostFlex != nil {
			return *p.CacheReadInputTokenCostFlex
		}
	case TierBatch:
		if totalTokens > BandAbove272K && p.CacheReadInputTokenCostBatchesAbove272kTokens != nil {
			return *p.CacheReadInputTokenCostBatchesAbove272kTokens
		}
		if p.CacheReadInputTokenCostBatches != nil {
			return *p.CacheReadInputTokenCostBatches
		}
	}
	if totalTokens > BandAbove512K && p.CacheReadInputTokenCostAbove512kTokens != nil {
		return *p.CacheReadInputTokenCostAbove512kTokens
	}
	if totalTokens > BandAbove272K {
		if tier == TierPriority && p.CacheReadInputTokenCostAbove272kTokensPriority != nil {
			return *p.CacheReadInputTokenCostAbove272kTokensPriority
		}
		if p.CacheReadInputTokenCostAbove272kTokens != nil {
			return *p.CacheReadInputTokenCostAbove272kTokens
		}
	}
	if totalTokens > BandAbove200K {
		if tier == TierPriority && p.CacheReadInputTokenCostAbove200kTokensPriority != nil {
			return *p.CacheReadInputTokenCostAbove200kTokensPriority
		}
		if p.CacheReadInputTokenCostAbove200kTokens != nil {
			return *p.CacheReadInputTokenCostAbove200kTokens
		}
	}
	if tier == TierPriority && p.CacheReadInputTokenCostPriority != nil {
		return *p.CacheReadInputTokenCostPriority
	}
	return rate(p.CacheReadInputTokenCost, p.InputCostPerTokenCacheHit)
}

func (p *Pricing) cacheCreationRate(totalTokens int64, tier ServiceTier, longTTL bool) float64 {
	if longTTL {
		if tier == TierFast && p.CacheCreationInputTokenCostAbove1hrFast != nil {
			return *p.CacheCreationInputTokenCostAbove1hrFast
		}
		if tier == TierBatch && p.CacheCreationInputTokenCostBatches1h != nil {
			return *p.CacheCreationInputTokenCostBatches1h
		}
		if totalTokens > BandAbove200K && p.CacheCreationInputTokenCostAbove1hrAbove200kTokens != nil {
			return *p.CacheCreationInputTokenCostAbove1hrAbove200kTokens
		}
		if p.CacheCreationInputTokenCostAbove1hr != nil {
			return *p.CacheCreationInputTokenCostAbove1hr
		}
	}
	switch tier {
	case TierFast:
		if p.CacheCreationInputTokenCostFast != nil {
			return *p.CacheCreationInputTokenCostFast
		}
	case TierFlex:
		if totalTokens > BandAbove272K {
			if v := rate(p.CacheCreationInputTokenCostAbove272kTokensFlex, p.CacheCreationInputTokenCostFlexAbove272kTokens); v != 0 {
				return v
			}
		}
		if p.CacheCreationInputTokenCostFlex != nil {
			return *p.CacheCreationInputTokenCostFlex
		}
	case TierBatch:
		if totalTokens > BandAbove272K && p.CacheCreationInputTokenCostBatchesAbove272kTokens != nil {
			return *p.CacheCreationInputTokenCostBatchesAbove272kTokens
		}
		if p.CacheCreationInputTokenCostBatches != nil {
			return *p.CacheCreationInputTokenCostBatches
		}
	}
	if totalTokens > BandAbove272K && p.CacheCreationInputTokenCostAbove272kTokens != nil {
		return *p.CacheCreationInputTokenCostAbove272kTokens
	}
	if totalTokens > BandAbove200K && p.CacheCreationInputTokenCostAbove200kTokens != nil {
		return *p.CacheCreationInputTokenCostAbove200kTokens
	}
	if tier == TierPriority && p.CacheCreationInputTokenCostPriority != nil {
		return *p.CacheCreationInputTokenCostPriority
	}
	return rate(p.CacheCreationInputTokenCost)
}

func (p *Pricing) audioInputTokenRate(tier ServiceTier) float64 {
	if tier == TierPriority && p.InputCostPerAudioTokenPriority != nil {
		return *p.InputCostPerAudioTokenPriority
	}
	return rate(p.InputCostPerAudioToken)
}

func (p *Pricing) audioSecondRate(totalTokens int64) float64 {
	if totalTokens > BandAbove128K && p.InputCostPerAudioPerSecondAbove128kTokens != nil {
		return *p.InputCostPerAudioPerSecondAbove128kTokens
	}
	return rate(p.InputCostPerAudioPerSecond)
}

func (p *Pricing) inputCharacterRate(totalTokens int64) float64 {
	if totalTokens > BandAbove128K && p.InputCostPerCharacterAbove128kTokens != nil {
		return *p.InputCostPerCharacterAbove128kTokens
	}
	return rate(p.InputCostPerCharacter)
}

func (p *Pricing) outputCharacterRate(totalTokens int64) float64 {
	if totalTokens > BandAbove128K && p.OutputCostPerCharacterAbove128kTokens != nil {
		return *p.OutputCostPerCharacterAbove128kTokens
	}
	return rate(p.OutputCostPerCharacter)
}

func (p *Pricing) inputImageRate(totalTokens int64) float64 {
	if totalTokens > BandAbove128K && p.InputCostPerImageAbove128kTokens != nil {
		return *p.InputCostPerImageAbove128kTokens
	}
	return rate(p.InputCostPerImage)
}

func (p *Pricing) inputVideoSecondRate(totalTokens int64) float64 {
	if totalTokens > BandAbove128K && p.InputCostPerVideoPerSecondAbove128kTokens != nil {
		return *p.InputCostPerVideoPerSecondAbove128kTokens
	}
	return rate(p.InputCostPerVideoPerSecond)
}


func (p *Pricing) outputVideoSecondRate(resolution string, hasAudio, fromVideo bool) float64 {
	switch resolution {
	case "360p":
		if hasAudio && p.OutputCostPerVideoPerSecond360pAudio != nil {
			return *p.OutputCostPerVideoPerSecond360pAudio
		}
		if p.OutputCostPerVideoPerSecond360p != nil {
			return *p.OutputCostPerVideoPerSecond360p
		}
	case "480p":
		if fromVideo && p.OutputCostPerVideoPerSecond480pVideoIn != nil {
			return *p.OutputCostPerVideoPerSecond480pVideoIn
		}
		if p.OutputCostPerVideoPerSecond480p != nil {
			return *p.OutputCostPerVideoPerSecond480p
		}
	case "540p":
		if hasAudio && p.OutputCostPerVideoPerSecond540pAudio != nil {
			return *p.OutputCostPerVideoPerSecond540pAudio
		}
		if p.OutputCostPerVideoPerSecond540p != nil {
			return *p.OutputCostPerVideoPerSecond540p
		}
	case "720p":
		if hasAudio && p.OutputCostPerVideoPerSecond720pAudio != nil {
			return *p.OutputCostPerVideoPerSecond720pAudio
		}
		if fromVideo && p.OutputCostPerVideoPerSecond720pVideoIn != nil {
			return *p.OutputCostPerVideoPerSecond720pVideoIn
		}
		if p.OutputCostPerVideoPerSecond720p != nil {
			return *p.OutputCostPerVideoPerSecond720p
		}
	case "1080p":
		if hasAudio && p.OutputCostPerVideoPerSecond1080pAudio != nil {
			return *p.OutputCostPerVideoPerSecond1080pAudio
		}
		if fromVideo && p.OutputCostPerVideoPerSecond1080pVideoIn != nil {
			return *p.OutputCostPerVideoPerSecond1080pVideoIn
		}
		if v := rate(p.OutputCostPerVideoPerSecond1080p, p.OutputCostPerSecond1080p); v != 0 {
			return v
		}
	case "4k":
		if hasAudio && p.OutputCostPerVideoPerSecond4kAudio != nil {
			return *p.OutputCostPerVideoPerSecond4kAudio
		}
		if fromVideo && p.OutputCostPerVideoPerSecond4kVideoIn != nil {
			return *p.OutputCostPerVideoPerSecond4kVideoIn
		}
		if p.OutputCostPerVideoPerSecond4k != nil {
			return *p.OutputCostPerVideoPerSecond4k
		}
	case "pro":
		if hasAudio && p.OutputCostPerVideoPerSecondProAudio != nil {
			return *p.OutputCostPerVideoPerSecondProAudio
		}
		if p.OutputCostPerVideoPerSecondPro != nil {
			return *p.OutputCostPerVideoPerSecondPro
		}
	case "standard":
		if hasAudio && p.OutputCostPerVideoPerSecondStandardAudio != nil {
			return *p.OutputCostPerVideoPerSecondStandardAudio
		}
		if p.OutputCostPerVideoPerSecondStandard != nil {
			return *p.OutputCostPerVideoPerSecondStandard
		}
	}
	return rate(p.OutputCostPerVideoPerSecond)
}

// outputImageRate prefers an explicit quality tier, then the pixel band the
// image falls into, then the flat per-image rate. Bands apply strictly above
// their threshold, matching the token bands.
func (p *Pricing) outputImageRate(quality string, pixels int64) float64 {
	switch quality {
	case "low":
		if p.OutputCostPerImageLowQuality != nil {
			return *p.OutputCostPerImageLowQuality
		}
	case "medium":
		if p.OutputCostPerImageMediumQuality != nil {
			return *p.OutputCostPerImageMediumQuality
		}
	case "high":
		if p.OutputCostPerImageHighQuality != nil {
			return *p.OutputCostPerImageHighQuality
		}
	case "auto":
		if p.OutputCostPerImageAutoQuality != nil {
			return *p.OutputCostPerImageAutoQuality
		}
	}

	if pixels > PixelsAbove4096 {
		if v := rate(p.OutputCostPerImageAbove4096And4096Pixels, p.OutputCostPerImageAbove4096Pixels); v != 0 {
			return v
		}
	}
	if pixels > PixelsAbove2048 {
		if v := rate(p.OutputCostPerImageAbove2048And2048Pixels, p.OutputCostPerImageAbove2048Pixels); v != 0 {
			return v
		}
	}
	if pixels > PixelsAbove1024 && p.OutputCostPerImageAbove1024And1024Pixels != nil {
		return *p.OutputCostPerImageAbove1024And1024Pixels
	}
	return rate(p.OutputCostPerImage)
}

// inputVideoSecondRateForClip bills a whole input clip by its duration band.
func (p *Pricing) inputVideoSecondRateForClip(seconds float64, totalTokens int64) float64 {
	if seconds > 15 && p.InputCostPerVideoPerSecondAbove15sInterval != nil {
		return *p.InputCostPerVideoPerSecondAbove15sInterval
	}
	if seconds > 8 && p.InputCostPerVideoPerSecondAbove8sInterval != nil {
		return *p.InputCostPerVideoPerSecondAbove8sInterval
	}
	return p.inputVideoSecondRate(totalTokens)
}

// outputVideoRate covers models priced per finished video at a fixed
// resolution and duration rather than per second.
func (p *Pricing) outputVideoRate(resolution string, seconds float64) float64 {
	switch resolution {
	case "768p":
		if seconds > 6 && p.OutputCostPerVideo768p10s != nil {
			return *p.OutputCostPerVideo768p10s
		}
		if p.OutputCostPerVideo768p6s != nil {
			return *p.OutputCostPerVideo768p6s
		}
	case "1080p":
		if p.OutputCostPerVideo1080p6s != nil {
			return *p.OutputCostPerVideo1080p6s
		}
	}
	return rate(p.OutputCostPerVideo)
}

func (p *Pricing) searchQueryRate(contextSize string) float64 {
	switch contextSize {
	case "low":
		if p.SearchContextCostPerQueryLow != nil {
			return *p.SearchContextCostPerQueryLow
		}
	case "high":
		if p.SearchContextCostPerQueryHigh != nil {
			return *p.SearchContextCostPerQueryHigh
		}
	case "medium":
		if p.SearchContextCostPerQueryMedium != nil {
			return *p.SearchContextCostPerQueryMedium
		}
	}
	return rate(p.SearchContextCostPerQueryMedium, p.SearchContextCostPerQueryHigh, p.SearchContextCostPerQueryLow)
}

type Usage struct {
	Tier              ServiceTier `json:"tier,omitempty"`
	CacheLongTTL      bool        `json:"cache_long_ttl,omitempty"`
	SearchContextSize string      `json:"search_context_size,omitempty"`
	VideoResolution string `json:"video_resolution,omitempty"` // 360p 480p 540p 720p 1080p 4k pro standard
	VideoHasAudio   bool   `json:"video_has_audio,omitempty"`
	VideoFromVideo  bool   `json:"video_from_video,omitempty"`
	ImageQuality    string `json:"image_quality,omitempty"` // low medium high auto

	InputTokens         int64       `json:"input_tokens,omitempty"`
	OutputTokens        int64       `json:"output_tokens,omitempty"`
	ReasoningTokens     int64       `json:"reasoning_tokens,omitempty"`
	CitationTokens      int64       `json:"citation_tokens,omitempty"`
	CachedInputTokens   int64       `json:"cached_input_tokens,omitempty"`
	CacheCreationTokens int64       `json:"cache_creation_tokens,omitempty"`

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

	InputVideoSeconds  float64 `json:"input_video_seconds,omitempty"`
	OutputVideoSeconds float64 `json:"output_video_seconds,omitempty"`
	OutputVideoTokens  int64   `json:"output_video_tokens,omitempty"`
	OutputVideos       int64   `json:"output_videos,omitempty"`
	InputSeconds       float64 `json:"input_seconds,omitempty"`
	OutputSeconds      float64 `json:"output_seconds,omitempty"`

	Queries         int64 `json:"queries,omitempty"`
	SearchQueries   int64 `json:"search_queries,omitempty"`
	Requests        int64 `json:"requests,omitempty"`
	InputRequests   int64 `json:"input_requests,omitempty"`
	Pages           int64 `json:"pages,omitempty"`
	OCRCredits      int64 `json:"ocr_credits,omitempty"`
	AnnotationPages int64 `json:"annotation_pages,omitempty"`
	CodeSessions    int64 `json:"code_sessions,omitempty"`
	InputDBUs       int64 `json:"input_dbus,omitempty"`
	OutputDBUs      int64 `json:"output_dbus,omitempty"`
	Units           int64 `json:"units,omitempty"`
}

func CalculateCost(p Pricing, u Usage) float64 {

	uncachedInput := u.InputTokens
	if p.CacheReadInputTokenCost != nil || p.InputCostPerTokenCacheHit != nil {
		uncachedInput -= u.CachedInputTokens
		if uncachedInput < 0 {
			uncachedInput = 0
		}
	}

	total := float64(uncachedInput) * p.inputRate(u.InputTokens, u.Tier)
	total += float64(u.OutputTokens) * p.outputRate(u.InputTokens, u.Tier)
	total += float64(u.CachedInputTokens) * p.cacheReadRate(u.InputTokens, u.Tier)
	total += float64(u.CacheCreationTokens) * p.cacheCreationRate(u.InputTokens, u.Tier, u.CacheLongTTL)
	total += float64(u.ReasoningTokens) * rate(p.OutputCostPerReasoningToken)
	total += float64(u.CitationTokens) * rate(p.CitationCostPerToken)

	total += float64(u.InputAudioTokens) * p.audioInputTokenRate(u.Tier)
	total += float64(u.OutputAudioTokens) * rate(p.OutputCostPerAudioToken)
	total += float64(u.CachedAudioTokens) * rate(p.CacheReadInputAudioTokenCost)
	total += float64(u.CacheCreationAudioTokens) * rate(p.CacheCreationInputAudioTokenCost)
	total += u.InputAudioSeconds * p.audioSecondRate(u.InputTokens)

	total += float64(u.InputCharacters) * p.inputCharacterRate(u.InputTokens)
	total += float64(u.OutputCharacters) * p.outputCharacterRate(u.InputTokens)

	total += float64(u.InputImages) * p.inputImageRate(u.InputTokens)
	total += float64(u.OutputImages) * p.outputImageRate(u.ImageQuality, u.OutputPixels)
	total += float64(u.InputImageTokens) * rate(p.InputCostPerImageToken)
	total += float64(u.OutputImageTokens) * rate(p.OutputCostPerImageToken)
	total += float64(u.GeneratedPixels) * rate(p.InputCostPerPixel)
	total += float64(u.OutputPixels) * rate(p.OutputCostPerPixel)

	total += u.InputVideoSeconds * p.inputVideoSecondRateForClip(u.InputVideoSeconds, u.InputTokens)
	total += u.OutputVideoSeconds * p.outputVideoSecondRate(u.VideoResolution, u.VideoHasAudio, u.VideoFromVideo)
	total += float64(u.OutputVideoTokens) * rate(p.OutputCostPerVideoToken)
	total += float64(u.OutputVideos) * p.outputVideoRate(u.VideoResolution, u.OutputVideoSeconds)
	total += u.InputSeconds * rate(p.InputCostPerSecond)
	total += u.OutputSeconds * rate(p.OutputCostPerSecond)

	total += float64(u.Queries) * rate(p.InputCostPerQuery)
	total += float64(u.SearchQueries) * p.searchQueryRate(u.SearchContextSize)
	total += float64(u.Requests) * rate(p.CostPerRequest)
	total += float64(u.InputRequests) * rate(p.InputCostPerRequest)
	total += float64(u.Pages) * rate(p.OCRCostPerPage)
	total += float64(u.OCRCredits) * rate(p.OCRCostPerCredit)
	total += float64(u.AnnotationPages) * rate(p.AnnotationCostPerPage)
	total += float64(u.CodeSessions) * rate(p.CodeInterpreterCostPerSession)
	total += float64(u.InputDBUs) * rate(p.InputDBUCostPerToken)
	total += float64(u.OutputDBUs) * rate(p.OutputDBUCostPerToken)
	total += float64(u.Units) * rate(p.OutputCostPerUnit)

	return total
}

type PricingVariant struct {
	ID        string      `json:"id,omitempty"`
	Source    string      `json:"source"`
	RawKey    string      `json:"raw_key"`
	Key       PriceKey    `json:"key"`
	ModelType ModelType   `json:"model_type"`
	Selectors SelectorSet `json:"selectors"`

	Pricing Pricing `json:"pricing"`
}

var (
	ErrVariantRequired    = errors.New("model requires variant parameters")
	ErrUnsupportedVariant = errors.New("no price for the requested variant")
)

func pricingFieldPairs(merged *Pricing, override *Pricing) []struct {
	dst **float64
	src *float64
} {
	return []struct {
		dst **float64
		src *float64
	}{
		{&merged.InputCostPerToken, override.InputCostPerToken},
		{&merged.OutputCostPerToken, override.OutputCostPerToken},
		{&merged.InputCostPerTokenPriority, override.InputCostPerTokenPriority},
		{&merged.OutputCostPerTokenPriority, override.OutputCostPerTokenPriority},
		{&merged.InputCostPerTokenFlex, override.InputCostPerTokenFlex},
		{&merged.OutputCostPerTokenFlex, override.OutputCostPerTokenFlex},
		{&merged.InputCostPerTokenBatches, override.InputCostPerTokenBatches},
		{&merged.OutputCostPerTokenBatches, override.OutputCostPerTokenBatches},
		{&merged.InputCostPerTokenFast, override.InputCostPerTokenFast},
		{&merged.OutputCostPerTokenFast, override.OutputCostPerTokenFast},
		{&merged.InputCostPerTokenAbove128kTokens, override.InputCostPerTokenAbove128kTokens},
		{&merged.OutputCostPerTokenAbove128kTokens, override.OutputCostPerTokenAbove128kTokens},
		{&merged.InputCostPerTokenAbove200kTokens, override.InputCostPerTokenAbove200kTokens},
		{&merged.OutputCostPerTokenAbove200kTokens, override.OutputCostPerTokenAbove200kTokens},
		{&merged.InputCostPerTokenAbove256kTokens, override.InputCostPerTokenAbove256kTokens},
		{&merged.OutputCostPerTokenAbove256kTokens, override.OutputCostPerTokenAbove256kTokens},
		{&merged.InputCostPerTokenAbove272kTokens, override.InputCostPerTokenAbove272kTokens},
		{&merged.OutputCostPerTokenAbove272kTokens, override.OutputCostPerTokenAbove272kTokens},
		{&merged.InputCostPerTokenAbove512kTokens, override.InputCostPerTokenAbove512kTokens},
		{&merged.OutputCostPerTokenAbove512kTokens, override.OutputCostPerTokenAbove512kTokens},
		{&merged.InputCostPerTokenAbove200kTokensPriority, override.InputCostPerTokenAbove200kTokensPriority},
		{&merged.OutputCostPerTokenAbove200kTokensPriority, override.OutputCostPerTokenAbove200kTokensPriority},
		{&merged.InputCostPerTokenAbove272kTokensPriority, override.InputCostPerTokenAbove272kTokensPriority},
		{&merged.OutputCostPerTokenAbove272kTokensPriority, override.OutputCostPerTokenAbove272kTokensPriority},
		{&merged.InputCostPerTokenAbove272kTokensFlex, override.InputCostPerTokenAbove272kTokensFlex},
		{&merged.OutputCostPerTokenAbove272kTokensFlex, override.OutputCostPerTokenAbove272kTokensFlex},
		{&merged.InputCostPerTokenFlexAbove272kTokens, override.InputCostPerTokenFlexAbove272kTokens},
		{&merged.OutputCostPerTokenFlexAbove272kTokens, override.OutputCostPerTokenFlexAbove272kTokens},
		{&merged.InputCostPerTokenBatchesAbove272kTokens, override.InputCostPerTokenBatchesAbove272kTokens},
		{&merged.OutputCostPerTokenBatchesAbove272kTokens, override.OutputCostPerTokenBatchesAbove272kTokens},
		{&merged.CacheReadInputTokenCost, override.CacheReadInputTokenCost},
		{&merged.CacheReadInputTokenCostPriority, override.CacheReadInputTokenCostPriority},
		{&merged.CacheReadInputTokenCostFlex, override.CacheReadInputTokenCostFlex},
		{&merged.CacheReadInputTokenCostBatches, override.CacheReadInputTokenCostBatches},
		{&merged.CacheReadInputTokenCostFast, override.CacheReadInputTokenCostFast},
		{&merged.CacheReadInputTokenCostAbove200kTokens, override.CacheReadInputTokenCostAbove200kTokens},
		{&merged.CacheReadInputTokenCostAbove272kTokens, override.CacheReadInputTokenCostAbove272kTokens},
		{&merged.CacheReadInputTokenCostAbove512kTokens, override.CacheReadInputTokenCostAbove512kTokens},
		{&merged.CacheReadInputTokenCostAbove200kTokensPriority, override.CacheReadInputTokenCostAbove200kTokensPriority},
		{&merged.CacheReadInputTokenCostAbove272kTokensPriority, override.CacheReadInputTokenCostAbove272kTokensPriority},
		{&merged.CacheReadInputTokenCostAbove272kTokensFlex, override.CacheReadInputTokenCostAbove272kTokensFlex},
		{&merged.CacheReadInputTokenCostFlexAbove272kTokens, override.CacheReadInputTokenCostFlexAbove272kTokens},
		{&merged.CacheReadInputTokenCostBatchesAbove272kTokens, override.CacheReadInputTokenCostBatchesAbove272kTokens},
		{&merged.InputCostPerTokenCacheHit, override.InputCostPerTokenCacheHit},
		{&merged.CacheCreationInputTokenCost, override.CacheCreationInputTokenCost},
		{&merged.CacheCreationInputTokenCostPriority, override.CacheCreationInputTokenCostPriority},
		{&merged.CacheCreationInputTokenCostFlex, override.CacheCreationInputTokenCostFlex},
		{&merged.CacheCreationInputTokenCostBatches, override.CacheCreationInputTokenCostBatches},
		{&merged.CacheCreationInputTokenCostFast, override.CacheCreationInputTokenCostFast},
		{&merged.CacheCreationInputTokenCostAbove200kTokens, override.CacheCreationInputTokenCostAbove200kTokens},
		{&merged.CacheCreationInputTokenCostAbove272kTokens, override.CacheCreationInputTokenCostAbove272kTokens},
		{&merged.CacheCreationInputTokenCostAbove272kTokensFlex, override.CacheCreationInputTokenCostAbove272kTokensFlex},
		{&merged.CacheCreationInputTokenCostFlexAbove272kTokens, override.CacheCreationInputTokenCostFlexAbove272kTokens},
		{&merged.CacheCreationInputTokenCostBatchesAbove272kTokens, override.CacheCreationInputTokenCostBatchesAbove272kTokens},
		{&merged.CacheCreationInputTokenCostAbove1hr, override.CacheCreationInputTokenCostAbove1hr},
		{&merged.CacheCreationInputTokenCostAbove1hrAbove200kTokens, override.CacheCreationInputTokenCostAbove1hrAbove200kTokens},
		{&merged.CacheCreationInputTokenCostAbove1hrFast, override.CacheCreationInputTokenCostAbove1hrFast},
		{&merged.CacheCreationInputTokenCostBatches1h, override.CacheCreationInputTokenCostBatches1h},
		{&merged.OutputCostPerReasoningToken, override.OutputCostPerReasoningToken},
		{&merged.CitationCostPerToken, override.CitationCostPerToken},
		{&merged.InputCostPerAudioToken, override.InputCostPerAudioToken},
		{&merged.OutputCostPerAudioToken, override.OutputCostPerAudioToken},
		{&merged.InputCostPerAudioTokenPriority, override.InputCostPerAudioTokenPriority},
		{&merged.CacheReadInputAudioTokenCost, override.CacheReadInputAudioTokenCost},
		{&merged.CacheCreationInputAudioTokenCost, override.CacheCreationInputAudioTokenCost},
		{&merged.InputCostPerAudioPerSecond, override.InputCostPerAudioPerSecond},
		{&merged.InputCostPerAudioPerSecondAbove128kTokens, override.InputCostPerAudioPerSecondAbove128kTokens},
		{&merged.InputCostPerCharacter, override.InputCostPerCharacter},
		{&merged.OutputCostPerCharacter, override.OutputCostPerCharacter},
		{&merged.InputCostPerCharacterAbove128kTokens, override.InputCostPerCharacterAbove128kTokens},
		{&merged.OutputCostPerCharacterAbove128kTokens, override.OutputCostPerCharacterAbove128kTokens},
		{&merged.InputCostPerImage, override.InputCostPerImage},
		{&merged.OutputCostPerImage, override.OutputCostPerImage},
		{&merged.InputCostPerImageAbove128kTokens, override.InputCostPerImageAbove128kTokens},
		{&merged.InputCostPerImageToken, override.InputCostPerImageToken},
		{&merged.OutputCostPerImageToken, override.OutputCostPerImageToken},
		{&merged.InputCostPerPixel, override.InputCostPerPixel},
		{&merged.OutputCostPerPixel, override.OutputCostPerPixel},
		{&merged.OutputCostPerImageLowQuality, override.OutputCostPerImageLowQuality},
		{&merged.OutputCostPerImageMediumQuality, override.OutputCostPerImageMediumQuality},
		{&merged.OutputCostPerImageHighQuality, override.OutputCostPerImageHighQuality},
		{&merged.OutputCostPerImageAutoQuality, override.OutputCostPerImageAutoQuality},
		{&merged.OutputCostPerImageAbove1024And1024Pixels, override.OutputCostPerImageAbove1024And1024Pixels},
		{&merged.OutputCostPerImageAbove2048And2048Pixels, override.OutputCostPerImageAbove2048And2048Pixels},
		{&merged.OutputCostPerImageAbove4096And4096Pixels, override.OutputCostPerImageAbove4096And4096Pixels},
		{&merged.OutputCostPerImageAbove2048Pixels, override.OutputCostPerImageAbove2048Pixels},
		{&merged.OutputCostPerImageAbove4096Pixels, override.OutputCostPerImageAbove4096Pixels},
		{&merged.InputCostPerVideoPerSecond, override.InputCostPerVideoPerSecond},
		{&merged.InputCostPerVideoPerSecondAbove128kTokens, override.InputCostPerVideoPerSecondAbove128kTokens},
		{&merged.InputCostPerVideoPerSecondAbove8sInterval, override.InputCostPerVideoPerSecondAbove8sInterval},
		{&merged.InputCostPerVideoPerSecondAbove15sInterval, override.InputCostPerVideoPerSecondAbove15sInterval},
		{&merged.OutputCostPerVideoPerSecond, override.OutputCostPerVideoPerSecond},
		{&merged.OutputCostPerVideoPerSecond360p, override.OutputCostPerVideoPerSecond360p},
		{&merged.OutputCostPerVideoPerSecond360pAudio, override.OutputCostPerVideoPerSecond360pAudio},
		{&merged.OutputCostPerVideoPerSecond480p, override.OutputCostPerVideoPerSecond480p},
		{&merged.OutputCostPerVideoPerSecond480pVideoIn, override.OutputCostPerVideoPerSecond480pVideoIn},
		{&merged.OutputCostPerVideoPerSecond540p, override.OutputCostPerVideoPerSecond540p},
		{&merged.OutputCostPerVideoPerSecond540pAudio, override.OutputCostPerVideoPerSecond540pAudio},
		{&merged.OutputCostPerVideoPerSecond720p, override.OutputCostPerVideoPerSecond720p},
		{&merged.OutputCostPerVideoPerSecond720pAudio, override.OutputCostPerVideoPerSecond720pAudio},
		{&merged.OutputCostPerVideoPerSecond720pVideoIn, override.OutputCostPerVideoPerSecond720pVideoIn},
		{&merged.OutputCostPerVideoPerSecond1080p, override.OutputCostPerVideoPerSecond1080p},
		{&merged.OutputCostPerVideoPerSecond1080pAudio, override.OutputCostPerVideoPerSecond1080pAudio},
		{&merged.OutputCostPerVideoPerSecond1080pVideoIn, override.OutputCostPerVideoPerSecond1080pVideoIn},
		{&merged.OutputCostPerVideoPerSecond4k, override.OutputCostPerVideoPerSecond4k},
		{&merged.OutputCostPerVideoPerSecond4kAudio, override.OutputCostPerVideoPerSecond4kAudio},
		{&merged.OutputCostPerVideoPerSecond4kVideoIn, override.OutputCostPerVideoPerSecond4kVideoIn},
		{&merged.OutputCostPerVideoPerSecondPro, override.OutputCostPerVideoPerSecondPro},
		{&merged.OutputCostPerVideoPerSecondProAudio, override.OutputCostPerVideoPerSecondProAudio},
		{&merged.OutputCostPerVideoPerSecondStandard, override.OutputCostPerVideoPerSecondStandard},
		{&merged.OutputCostPerVideoPerSecondStandardAudio, override.OutputCostPerVideoPerSecondStandardAudio},
		{&merged.OutputCostPerSecond1080p, override.OutputCostPerSecond1080p},
		{&merged.OutputCostPerVideoToken, override.OutputCostPerVideoToken},
		{&merged.OutputCostPerVideo, override.OutputCostPerVideo},
		{&merged.OutputCostPerVideo768p6s, override.OutputCostPerVideo768p6s},
		{&merged.OutputCostPerVideo768p10s, override.OutputCostPerVideo768p10s},
		{&merged.OutputCostPerVideo1080p6s, override.OutputCostPerVideo1080p6s},
		{&merged.InputCostPerSecond, override.InputCostPerSecond},
		{&merged.OutputCostPerSecond, override.OutputCostPerSecond},
		{&merged.InputCostPerQuery, override.InputCostPerQuery},
		{&merged.SearchContextCostPerQueryLow, override.SearchContextCostPerQueryLow},
		{&merged.SearchContextCostPerQueryMedium, override.SearchContextCostPerQueryMedium},
		{&merged.SearchContextCostPerQueryHigh, override.SearchContextCostPerQueryHigh},
		{&merged.CostPerRequest, override.CostPerRequest},
		{&merged.InputCostPerRequest, override.InputCostPerRequest},
		{&merged.OCRCostPerPage, override.OCRCostPerPage},
		{&merged.OCRCostPerCredit, override.OCRCostPerCredit},
		{&merged.AnnotationCostPerPage, override.AnnotationCostPerPage},
		{&merged.CodeInterpreterCostPerSession, override.CodeInterpreterCostPerSession},
		{&merged.InputDBUCostPerToken, override.InputDBUCostPerToken},
		{&merged.OutputDBUCostPerToken, override.OutputDBUCostPerToken},
		{&merged.OutputCostPerUnit, override.OutputCostPerUnit},
	}
}

func MergePricing(base, override Pricing) Pricing {
	merged := base
	for _, field := range pricingFieldPairs(&merged, &override) {
		if field.src != nil {
			*field.dst = field.src
		}
	}
	return merged
}

type CustomPricing struct {
	ID        string    `json:"id,omitempty"`
	Name      string    `json:"name"`
	ModelName string    `json:"model_name"`
	ModelType ModelType `json:"model_type"`

	Pricing Pricing `json:"pricing"`

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

	Pricing Pricing `json:"pricing"`

	ScopeType         ScopeType `json:"scope_type"             binding:"required,oneof=global provider virtualkey"`
	ScopeVirtualkeyID *string   `json:"scope_virtual_key_id,omitempty"`
	ScopeProvider     *Provider `json:"scope_provider,omitempty"`
}

type CustomPricingResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ModelName string `json:"model_name"`
	ModelType string `json:"model_type"`

	Pricing Pricing `json:"pricing"`

	ScopeType         ScopeType `json:"scope_type"`
	ScopeVirtualkeyID *string   `json:"scope_virtual_key_id,omitempty"`
	ScopeProvider     *Provider `json:"scope_provider,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
