package governance

import (
	"diffractllm/internal/core"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

type BasePricing map[core.ModelKey]*core.Pricing
type CustomPricing map[core.ModelKey][]*core.CustomPricing

// As above in custom pricing we can simply add things like
// All the override for the respective model ,
// like global , virtual key or provider , anyway i will be perform a comparable operation such that global and provider will be kept here , 0 and 1 ,starting from 2 it will be multiple like supporing keys right ?

type PricingCache struct {
	Base   atomic.Pointer[BasePricing]
	Custom atomic.Pointer[CustomPricing]
	mu     sync.Mutex
	Logger *zap.Logger
}
