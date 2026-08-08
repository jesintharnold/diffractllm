package core

import (
	"fmt"
	"sort"
)

type Meter string

const (
	// text tokens
	MeterInputToken              Meter = "input_token"
	MeterOutputToken             Meter = "output_token"
	MeterReasoningToken          Meter = "reasoning_token"
	MeterCachedInputToken        Meter = "cached_input_token"
	MeterCacheCreationInputToken Meter = "cache_creation_input_token"
	MeterCitationToken           Meter = "citation_token"

	// audio
	MeterInputAudioToken         Meter = "input_audio_token"
	MeterOutputAudioToken        Meter = "output_audio_token"
	MeterCachedAudioToken        Meter = "cached_audio_token"
	MeterCacheCreationAudioToken Meter = "cache_creation_audio_token"
	MeterInputAudioSecond        Meter = "input_audio_second"

	// characters
	MeterInputCharacter  Meter = "input_character"
	MeterOutputCharacter Meter = "output_character"

	// images
	MeterInputImage       Meter = "input_image"
	MeterOutputImage      Meter = "output_image"
	MeterInputImageToken  Meter = "input_image_token"
	MeterOutputImageToken Meter = "output_image_token"
	MeterOutputPixel      Meter = "output_pixel"

	MeterGeneratedPixel Meter = "generated_pixel"

	// time and video
	MeterInputSecond      Meter = "input_second"
	MeterOutputSecond     Meter = "output_second"
	MeterInputVideoSecond Meter = "input_video_second"
	MeterVideoSecond      Meter = "video_second"
	MeterVideoToken       Meter = "video_token"

	MeterVideo Meter = "video" // priced per whole video, not per second

	// per-unit
	MeterQuery          Meter = "query"
	MeterSearchQuery    Meter = "search_query"
	MeterRequest        Meter = "request"
	MeterPage           Meter = "page"
	MeterAnnotationPage Meter = "annotation_page"
	MeterOCRCredit      Meter = "ocr_credit"
	MeterCodeSession    Meter = "code_session"
	MeterInputDBU       Meter = "input_dbu"
	MeterOutputDBU      Meter = "output_dbu"
	MeterUnit           Meter = "unit" // provider-defined generic unit
)

func (m Meter) Valid() bool {
	_, ok := meterNames[m]
	return ok
}

var meterNames = map[Meter]struct{}{
	MeterInputToken:              {},
	MeterOutputToken:             {},
	MeterReasoningToken:          {},
	MeterCachedInputToken:        {},
	MeterCacheCreationInputToken: {},
	MeterCitationToken:           {},
	MeterInputAudioToken:         {},
	MeterOutputAudioToken:        {},
	MeterCachedAudioToken:        {},
	MeterCacheCreationAudioToken: {},
	MeterInputAudioSecond:        {},
	MeterInputCharacter:          {},
	MeterOutputCharacter:         {},
	MeterInputImage:              {},
	MeterOutputImage:             {},
	MeterInputImageToken:         {},
	MeterOutputImageToken:        {},
	MeterOutputPixel:             {},
	MeterGeneratedPixel:          {},
	MeterInputSecond:             {},
	MeterOutputSecond:            {},
	MeterInputVideoSecond:        {},
	MeterVideoSecond:             {},
	MeterVideoToken:              {},
	MeterVideo:                   {},
	MeterQuery:                   {},
	MeterSearchQuery:             {},
	MeterRequest:                 {},
	MeterPage:                    {},
	MeterAnnotationPage:          {},
	MeterOCRCredit:               {},
	MeterCodeSession:             {},
	MeterInputDBU:                {},
	MeterOutputDBU:               {},
	MeterUnit:                    {},
}

type RateCondition string

const (
	CondStandard RateCondition = "standard"
	CondPriority RateCondition = "priority"
	CondFlex     RateCondition = "flex"
	CondBatch    RateCondition = "batch"
	CondFast     RateCondition = "fast"
	CondCache1h  RateCondition = "cache_1h"
)

func ParseRateCondition(value string) RateCondition {
	switch value {
	case "priority":
		return CondPriority
	case "flex":
		return CondFlex
	case "batch", "batches":
		return CondBatch
	case "fast":
		return CondFast
	case "cache_1h", "above_1hr":
		return CondCache1h
	default:
		return CondStandard
	}
}

type RateTerm struct {
	Meter       Meter         `json:"meter"`
	Condition   RateCondition `json:"condition,omitempty"`
	MinTokens   int64         `json:"min_tokens,omitempty"`
	Qualifier   string        `json:"qualifier,omitempty"`
	PriceUSD    float64       `json:"price_usd"`
	SourceField string        `json:"source_field,omitempty"`
}

type rateTermID struct {
	Meter     Meter
	Condition RateCondition
	MinTokens int64
	Qualifier string
}

func (t RateTerm) id() rateTermID {
	condition := t.Condition
	if condition == "" {
		condition = CondStandard
	}
	return rateTermID{
		Meter:     t.Meter,
		Condition: condition,
		MinTokens: t.MinTokens,
		Qualifier: t.Qualifier,
	}
}

type RateCard struct {
	Terms []RateTerm `json:"terms"`
}

func NewRateCard(terms []RateTerm) (RateCard, error) {
	seen := make(map[rateTermID]struct{}, len(terms))
	canonical := make([]RateTerm, 0, len(terms))

	for _, term := range terms {
		if !term.Meter.Valid() {
			return RateCard{}, fmt.Errorf("unknown meter %q", term.Meter)
		}
		if term.PriceUSD < 0 {
			return RateCard{}, fmt.Errorf("negative rate for meter %q", term.Meter)
		}
		if term.Condition == "" {
			term.Condition = CondStandard
		}
		if _, exists := seen[term.id()]; exists {
			return RateCard{}, fmt.Errorf(
				"duplicate rate for meter %q condition %q above %d tokens",
				term.Meter, term.Condition, term.MinTokens)
		}
		seen[term.id()] = struct{}{}
		canonical = append(canonical, term)
	}

	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].Meter != canonical[j].Meter {
			return canonical[i].Meter < canonical[j].Meter
		}
		if canonical[i].Condition != canonical[j].Condition {
			return canonical[i].Condition < canonical[j].Condition
		}
		if canonical[i].MinTokens != canonical[j].MinTokens {
			return canonical[i].MinTokens < canonical[j].MinTokens
		}
		return canonical[i].Qualifier < canonical[j].Qualifier
	})
	return RateCard{Terms: canonical}, nil
}

func (c RateCard) IsEmpty() bool { return len(c.Terms) == 0 }

func (c RateCard) Meters() []Meter {
	meters := make([]Meter, 0, len(c.Terms))
	seen := make(map[Meter]struct{}, len(c.Terms))
	for _, term := range c.Terms {
		if _, exists := seen[term.Meter]; exists {
			continue
		}
		seen[term.Meter] = struct{}{}
		meters = append(meters, term.Meter)
	}
	return meters
}

func (c RateCard) HasMeter(meter Meter) bool {
	for _, term := range c.Terms {
		if term.Meter == meter {
			return true
		}
	}
	return false
}

func (c RateCard) Select(meter Meter, condition RateCondition, contextTokens int64, qualifier string) (RateTerm, bool) {
	if condition == "" {
		condition = CondStandard
	}

	var best RateTerm
	bestScore := -1
	for _, term := range c.Terms {
		if term.Meter != meter || term.MinTokens > contextTokens {
			continue
		}
		if term.Condition != condition && term.Condition != CondStandard {
			continue
		}
		if term.Qualifier != "" && term.Qualifier != qualifier {
			continue
		}

		score := 0
		if term.Condition == condition {
			score += 4
		}
		if term.Qualifier == qualifier && qualifier != "" {
			score += 2
		}
		if score > bestScore || (score == bestScore && term.MinTokens > best.MinTokens) {
			best, bestScore = term, score
		}
	}
	return best, bestScore >= 0
}

func MergeRateCards(base, override RateCard) RateCard {
	byID := make(map[rateTermID]RateTerm, len(base.Terms)+len(override.Terms))
	for _, term := range base.Terms {
		byID[term.id()] = term
	}
	for _, term := range override.Terms {
		byID[term.id()] = term
	}

	merged := make([]RateTerm, 0, len(byID))
	for _, term := range byID {
		merged = append(merged, term)
	}
	card, _ := NewRateCard(merged)
	return card
}
