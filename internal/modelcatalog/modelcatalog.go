package modelcatalog

import (
	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type ModelSnapshot struct {
	entries  []core.ModelMetaData
	metadata map[core.ModelKey]*core.ModelMetaData
}

type BasePricingSnapshot map[core.ModelKey]*core.BasePricing

type CustomPriceSnapshot map[string]*core.CustomScopePricing

type ModelCatalog struct {
	models        atomic.Pointer[ModelSnapshot]
	basePricing   atomic.Pointer[BasePricingSnapshot]
	customPricing atomic.Pointer[CustomPriceSnapshot]

	lastModelSync  atomic.Int64
	lastBaseSync   atomic.Int64
	lastCustomSync atomic.Int64

	store  *dbstore.Store
	logger *zap.Logger

	mu      sync.Mutex
	cfg     config.ModelCatalogConfig
	started bool

	customMu sync.Mutex

	client *http.Client

	done chan struct{}
	wg   sync.WaitGroup
}

func NewModelCatalog(store *dbstore.Store, cfg config.ModelCatalogConfig, logger *zap.Logger) *ModelCatalog {
	return &ModelCatalog{
		store:  store,
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *ModelCatalog) LoadModels() error {
	start := time.Now()

	rows, err := c.store.ListModelMetadata()
	if err != nil {
		return fmt.Errorf("load model metadata: %w", err)
	}

	models := make([]core.ModelMetaData, 0, len(rows))
	for i := range rows {
		models = append(models, rows[i].ToCore())
	}
	metadata := make(map[core.ModelKey]*core.ModelMetaData, len(models))
	for i := range models {
		md := &models[i]
		metadata[md.Key()] = md
	}

	c.models.Store(&ModelSnapshot{entries: models, metadata: metadata})
	c.lastModelSync.Store(time.Now().UnixNano())
	c.logger.Debug("model metadata hot-swapped",
		zap.Int("models", len(models)), zap.Duration("took", time.Since(start)))
	return nil
}

func (c *ModelCatalog) LoadBasePricing() error {
	start := time.Now()

	rows, err := c.store.ListBasePricing()
	if err != nil {
		return fmt.Errorf("load base pricing: %w", err)
	}

	tempBase := make(BasePricingSnapshot, len(rows))
	for i := range rows {
		mp := rows[i].ToCore()
		tempBase[core.ModelKey{Provider: mp.Provider, ModelName: mp.ModelName}] = mp
	}
	c.basePricing.Store(&tempBase)
	c.lastBaseSync.Store(time.Now().UnixNano())
	c.logger.Debug("base pricing hot-swapped",
		zap.Int("rows", len(tempBase)), zap.Duration("took", time.Since(start)))
	return nil
}

func (c *ModelCatalog) LoadCustomPricing() error {
	start := time.Now()

	c.customMu.Lock()
	defer c.customMu.Unlock()

	rows, err := c.store.ListCustomPricing()
	if err != nil {
		return fmt.Errorf("load custom pricing: %w", err)
	}

	tempCustom := make(CustomPriceSnapshot, len(rows))
	for i := range rows {
		cp := rows[i].ToCore()
		scoped, exists := tempCustom[cp.ModelName]
		if !exists {
			scoped = &core.CustomScopePricing{
				Provider:   make(map[core.Provider]*core.CustomPricing),
				VirtualKey: make(map[string]*core.CustomPricing),
			}
			tempCustom[cp.ModelName] = scoped
		}

		switch cp.ScopeType {
		case core.ScopeGlobal:
			scoped.Global = cp
		case core.ScopeProvider:
			if cp.ScopeProvider != nil {
				scoped.Provider[*cp.ScopeProvider] = cp
			}
		case core.ScopeVirtualKey:
			if cp.ScopeVirtualkeyID != nil {
				scoped.VirtualKey[*cp.ScopeVirtualkeyID] = cp
			}
		}
	}

	c.customPricing.Store(&tempCustom)
	c.lastCustomSync.Store(time.Now().UnixNano())
	c.logger.Debug("custom pricing hot-swapped",
		zap.Int("models", len(tempCustom)), zap.Duration("took", time.Since(start)))
	return nil
}

func (c *ModelCatalog) Lookup(key core.ModelKey) (*core.ModelMetaData, bool) {
	snap := c.models.Load()
	if snap == nil {
		return nil, false
	}
	md, ok := snap.metadata[key]
	return md, ok
}

func (c *ModelCatalog) ModelsForProvider(provider core.Provider) []string {
	snap := c.models.Load()
	if snap == nil {
		return nil
	}
	out := make([]string, 0, 16)
	for i := range snap.entries {
		if snap.entries[i].Provider == provider {
			out = append(out, snap.entries[i].ModelName)
		}
	}
	return out
}

func (c *ModelCatalog) ResolvePrice(virtualKeyID string, key core.ModelKey) *core.Pricing {
	basePtr := c.basePricing.Load()
	if basePtr == nil {
		return nil
	}
	bp, ok := (*basePtr)[key]
	if !ok {
		return nil
	}

	customPtr := c.customPricing.Load()
	if customPtr == nil {
		return &bp.Pricing
	}
	scoped, ok := (*customPtr)[key.ModelName]
	if !ok {
		return &bp.Pricing
	}

	if vkPrice, found := scoped.VirtualKey[virtualKeyID]; found {
		return core.MergePricing(&bp.Pricing, &vkPrice.Pricing)
	}
	if provPrice, found := scoped.Provider[key.Provider]; found {
		return core.MergePricing(&bp.Pricing, &provPrice.Pricing)
	}
	if scoped.Global != nil {
		return core.MergePricing(&bp.Pricing, &scoped.Global.Pricing)
	}
	return &bp.Pricing
}

func (c *ModelCatalog) LastModelSync() time.Time  { return time.Unix(0, c.lastModelSync.Load()) }
func (c *ModelCatalog) LastBaseSync() time.Time   { return time.Unix(0, c.lastBaseSync.Load()) }
func (c *ModelCatalog) LastCustomSync() time.Time { return time.Unix(0, c.lastCustomSync.Load()) }
