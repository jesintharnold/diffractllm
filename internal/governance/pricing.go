package governance

import (
	"diffractllm/internal/core"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type BasePricing map[core.ModelKey]*core.BasePricing

type CustomPricing map[string]*core.CustomScopePricing

type PricingCache struct {
	Base           atomic.Pointer[BasePricing]
	Custom         atomic.Pointer[CustomPricing]
	mu             sync.Mutex
	LastBaseSync   time.Time
	LastCustomSync time.Time
	logger         *zap.Logger
}

func NewPricingCache(logger *zap.Logger) *PricingCache {
	return &PricingCache{
		logger: logger,
	}
}

func (pc *PricingCache) LoadBasePricing(baseprice []*core.BasePricing) {

	tempBase := make(BasePricing, len(baseprice))
	for _, mp := range baseprice {
		model_key := core.ModelKey{
			Provider:  mp.Provider,
			ModelName: mp.ModelName,
		}
		tempBase[model_key] = mp
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.Base.Store(&tempBase)
	pc.LastBaseSync = time.Now()
	pc.logger.Debug("Base pricing cache hot-swapped successfully :", zap.Time("time", time.Now()))
}

func (pc *PricingCache) LoadCustomPricing(customprice []*core.CustomPricing) {
	tempCustom := make(CustomPricing, len(customprice))
	for _, cp := range customprice {
		customoverride, exists := tempCustom[cp.ModelName]
		if !exists {
			customoverride = &core.CustomScopePricing{
				Provider:   make(map[core.Provider]*core.CustomPricing),
				VirtualKey: make(map[string]*core.CustomPricing),
			}
		}

		switch cp.ScopeType {
		case core.ScopeGlobal:
			customoverride.Global = cp
		case core.ScopeProvider:
			if cp.ScopeProvider != nil {
				customoverride.Provider[*cp.ScopeProvider] = cp
			}
		case core.ScopeVirtualKey:
			if cp.ScopeVirtualkeyID != nil {
				customoverride.VirtualKey[*cp.ScopeVirtualkeyID] = cp
			}
		}
		tempCustom[cp.ModelName] = customoverride
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.Custom.Store(&tempCustom)
	pc.LastCustomSync = time.Now()
	pc.logger.Debug("Custom pricing cache hot-swapped successfully :", zap.Time("time", time.Now()))

}

func (pc *PricingCache) ResolvePrice(rctx *core.DiffractLLMContext) *core.Pricing {
	basepricePtr := pc.Base.Load()
	if basepricePtr == nil {
		return nil
	}
	bp, ok := (*basepricePtr)[rctx.Modelkey]
	if !ok {
		return nil
	}

	custompricePtr := pc.Custom.Load()
	if custompricePtr == nil {
		return &bp.Pricing
	}
	cp, ok := (*custompricePtr)[rctx.Modelkey.ModelName]
	if !ok {
		pc.logger.Debug("Custom Price not detected for ", zap.String("requestID", time.Now().String()))
		return &bp.Pricing
	}

	if vkprice, vok := cp.VirtualKey[rctx.VirtualKeyID]; vok {
		return pc.mergePrice(&bp.Pricing, &vkprice.Pricing)
	}

	if proprice, pok := cp.Provider[rctx.Modelkey.Provider]; pok {
		return pc.mergePrice(&bp.Pricing, &proprice.Pricing)
	}

	if cp.Global != nil {
		return pc.mergePrice(&bp.Pricing, &cp.Global.Pricing)
	}
	return &bp.Pricing
}

func (pc *PricingCache) mergePrice(base *core.Pricing, custom *core.Pricing) *core.Pricing {
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
