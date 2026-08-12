package dataplane

import (
	"diffractllm/internal/core"
	"diffractllm/internal/registry"
	"math/rand"
	"sync"

	"go.uber.org/zap"
)

type Selector interface {
	Deployment(key core.CatalogKey, deployments []*core.Deployment) *core.Deployment
}

type SelectionEngine struct {
	registry  *registry.DeploymentRegistry
	selectors map[core.LBKind]Selector
	logger    *zap.Logger
}

func NewSelectionEngine(registry *registry.DeploymentRegistry, logger *zap.Logger) *SelectionEngine {
	return &SelectionEngine{
		registry: registry,
		selectors: map[core.LBKind]Selector{
			core.LBRoundRobin: newRoundRobin(),
		},
		logger: logger,
	}
}

func (se *SelectionEngine) kind(kind core.LBKind) Selector {
	if selector, ok := se.selectors[kind]; ok {
		return selector
	}
	return se.selectors[core.LBRoundRobin]
}

func (se *SelectionEngine) hasHealthyDeployment(key core.CatalogKey) bool {
	deployments, ok := se.registry.LookupModel(key)
	if !ok {
		return false
	}
	for _, deployment := range deployments {
		if isHealthy(deployment) {
			return true
		}
	}
	return false
}

func (se *SelectionEngine) Resolve(rctx *core.DiffractLLMContext) (*core.Deployment, *core.DiffractLLMError) {
	if rctx == nil || rctx.VirtualKeyID == "" || rctx.VirtualKeyPolicy == nil {
		return nil, core.NewAuthFailed("virtual key policy missing ,contact the adminstrator")
	}
	vk := rctx.VirtualKeyPolicy
	kind := se.kind(vk.LoadBalancer)
	rkey := rctx.Modelkey
	providerConfigs := vk.ProviderConfigs

	if providerConfigs == nil || len(providerConfigs) == 0 {
		return nil, core.NewAuthFailed("virtual key policy provider configs are missing ,contact the adminstrator")
	}

	// Explicit mode - we already know the model key here
	if rkey.ModelName != "" && rkey.Provider != "" {
		deployments, ok := se.registry.LookupModel(rkey)
		if !ok {
			return nil, core.NewNoHealthyBackends(rkey.SlashKey())
		}
		SelectedDeployment := kind.Deployment(rkey, deployments)
		if SelectedDeployment == nil {
			return nil, core.NewNoHealthyBackends(rkey.SlashKey())
		}
		return se.commit(rctx, rkey, SelectedDeployment), nil
	}

	//Bare model mode - No providers only model , in this case we need to check the weights if present
	if rkey.Provider == "" && rkey.ModelName != "" {
		var provider string
		if len(providerConfigs) > 1 {
			provider = string(
				selectWeightedProvider(providerConfigs, func(pc *core.ProviderConfig) bool {
					if pc == nil {
						return false
					}
					key := core.CatalogKey{Provider: pc.Provider, ModelName: rkey.ModelName}
					if !pc.IsModelAllowed(key) {
						return false
					}
					return se.hasHealthyDeployment(key)
				}).Provider)
		} else {
			provider = string(providerConfigs[0].Provider)
		}

		rkey = core.CatalogKey{
			Provider:  core.Provider(provider),
			ModelName: rkey.ModelName,
		}

		deployments, ok := se.registry.LookupModel(rkey)
		if !ok {
			return nil, core.NewNoHealthyBackends(rkey.SlashKey())
		}
		SelectedDeployment := kind.Deployment(rkey, deployments)
		if SelectedDeployment == nil {
			return nil, core.NewNoHealthyBackends(rkey.SlashKey())
		}
		return se.commit(rctx, rkey, SelectedDeployment), nil

	}

	if rkey.ModelName == "" && rkey.Provider == "" {

	}

	return nil, core.NewInternalError("model selection engine", "model key is unknown or its fields doesn`t exist", nil)
}

func (se *SelectionEngine) commit(rctx *core.DiffractLLMContext, key core.CatalogKey, deployment *core.Deployment) *core.Deployment {
	rctx.Modelkey = key
	rctx.SelectedDeployment = deployment
	return deployment
}

// --------- core algorithms -----------------

type roundRobin struct {
	deploymentCursors sync.Map
	poolModelCursors  sync.Map
}

func newRoundRobin() *roundRobin {
	return &roundRobin{}
}

func (r *roundRobin) Deployment(
	key core.CatalogKey,
	deployments []*core.Deployment,
) *core.Deployment {
	return nil
}

func selectWeightedProvider(configs []*core.ProviderConfig, healthChk func(*core.ProviderConfig) bool) *core.ProviderConfig {
	var totalWeight float64
	var healthyConfigs int

	for _, cfg := range configs {
		if cfg == nil || !healthChk(cfg) {
			continue
		}
		healthyConfigs++
		if cfg.Weight > 0 {
			totalWeight += float64(cfg.Weight)
		}
	}

	if healthyConfigs == 0 {
		return nil
	}

	if totalWeight <= 0 {
		return nil
	}

	targetWeight := rand.Float64() * totalWeight
	// performing the measuring cut
	for _, cfg := range configs {
		if cfg == nil || !healthChk(cfg) || cfg.Weight <= 0 {
			continue
		}
		targetWeight -= float64(cfg.Weight)
		if targetWeight < 0 {
			return cfg
		}
	}

	// Safe guard incase of the above target weight comes to > 0.00000001 (float rounding error)
	for index := 0; index < len(configs); index++ {
		cfg := configs[index]
		if cfg != nil && cfg.Weight > 0 && healthChk(cfg) {
			return cfg
		}
	}
	return nil
}

func isHealthy(deployment *core.Deployment) bool {
	return deployment != nil &&
		deployment.State != nil &&
		deployment.State.IsHealthy()
}
