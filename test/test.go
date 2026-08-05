package diffractllm_test

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"

	"diffractllm/internal/core"
	"diffractllm/internal/registry"

	"go.uber.org/zap"
)

// Selector picks one healthy deployment from a candidate list. Model selection
// for pools is the resolver's job (stage 2), not the algorithm's — so this is
// the only method every algorithm implements.
type Selector interface {
	Deployment(key core.ModelKey, deployments []*core.Deployment) *core.Deployment
}

type roundRobin struct {
	deploymentCursors sync.Map // core.ModelKey -> *atomic.Uint64
}

func newRoundRobin() *roundRobin {
	return &roundRobin{}
}

func (r *roundRobin) Deployment(
	key core.ModelKey,
	deployments []*core.Deployment,
) *core.Deployment {
	n := len(deployments)
	if n == 0 {
		return nil
	}
	start := int(r.next(&r.deploymentCursors, key) % uint64(n))

	for offset := 0; offset < n; offset++ {
		deployment := deployments[(start+offset)%n]
		if isHealthy(deployment) {
			return deployment
		}
	}

	return nil
}

func (r *roundRobin) next(cursors *sync.Map, key any) uint64 {
	value, ok := cursors.Load(key)
	if !ok {
		value, _ = cursors.LoadOrStore(key, new(atomic.Uint64))
	}

	// First call returns slot 0.
	return value.(*atomic.Uint64).Add(1) - 1
}

type Resolver struct {
	registry         *registry.DeploymentRegistry
	selectors        map[core.LBKind]Selector
	poolModelCursors sync.Map // poolKey -> *atomic.Uint64 (stage-2 model rotation)
	logger           *zap.Logger
}

func NewResolver(
	deployments *registry.DeploymentRegistry,
	logger *zap.Logger,
) *Resolver {
	return &Resolver{
		registry: deployments,
		selectors: map[core.LBKind]Selector{
			core.LBRoundRobin: newRoundRobin(),
		},
		logger: logger,
	}
}

func (r *Resolver) selectorFor(kind core.LBKind) Selector {
	if selector, found := r.selectors[kind]; found {
		return selector
	}
	return r.selectors[core.LBRoundRobin]
}

func (r *Resolver) Resolve(
	rctx *core.DiffractLLMContext,
) (*core.Deployment, *core.DiffractLLMError) {
	if rctx == nil || rctx.VirtualKeyPolicy == nil {
		return nil, core.NewAuthFailed("virtual key policy missing")
	}

	vk := rctx.VirtualKeyPolicy
	selector := r.selectorFor(vk.LoadBalancer)
	requested := rctx.Modelkey

	switch {
	case requested.Provider != "" && requested.ModelName != "":
		return r.resolveExplicit(rctx, selector, requested)

	case requested.ModelName != "":
		return r.resolveBare(rctx, selector, vk, requested.ModelName)

	default:
		return r.resolvePool(rctx, selector, vk)
	}
}

// Explicit request:
//
//	Azure/gpt-4o -> one healthy deployment in Azure/gpt-4o bucket
func (r *Resolver) resolveExplicit(
	rctx *core.DiffractLLMContext,
	selector Selector,
	key core.ModelKey,
) (*core.Deployment, *core.DiffractLLMError) {
	deployments, found := r.registry.LookupModel(key)
	if !found {
		return nil, core.NewNoHealthyBackends(key.SlashKey())
	}

	selected := selector.Deployment(key, deployments)
	if selected == nil {
		return nil, core.NewNoHealthyBackends(key.SlashKey())
	}

	return r.commit(rctx, key, selected), nil
}

// Bare-model request:
//
//	gpt-4o -> select eligible provider by weight -> select its deployment
func (r *Resolver) resolveBare(
	rctx *core.DiffractLLMContext,
	selector Selector,
	vk *core.VirtualKey,
	modelName string,
) (*core.Deployment, *core.DiffractLLMError) {
	provider := selectWeightedProvider(
		vk.ProviderConfigs,
		func(cfg *core.ProviderConfig) bool {
			if cfg == nil {
				return false
			}

			key := core.ModelKey{
				Provider:  cfg.Provider,
				ModelName: modelName,
			}

			// The hook only verified SOME provider permits this model. Here we
			// narrow to the providers that actually permit it for this key AND
			// currently have a healthy deployment — otherwise a bare request
			// could be weight-routed to a provider the key can't use.
			return cfg.IsModelAllowed(key) && r.hasHealthyDeployment(key)
		},
	)

	if provider == nil {
		return nil, core.NewNoHealthyBackends(modelName)
	}

	key := core.ModelKey{
		Provider:  provider.Provider,
		ModelName: modelName,
	}

	deployments, found := r.registry.LookupModel(key)
	if !found {
		return nil, core.NewNoHealthyBackends(modelName)
	}

	selected := selector.Deployment(key, deployments)
	if selected == nil {
		// Health may have changed after provider eligibility was checked.
		return nil, core.NewNoHealthyBackends(modelName)
	}

	return r.commit(rctx, key, selected), nil
}

// Pool request (no model in the request):
//
//	stage 1: weighted provider  (business/cost decision)
//	stage 2: round-robin a model within that provider's pool models
//	stage 3: deployment via the configured algorithm
func (r *Resolver) resolvePool(
	rctx *core.DiffractLLMContext,
	selector Selector,
	vk *core.VirtualKey,
) (*core.Deployment, *core.DiffractLLMError) {
	// STAGE 1 — provider by weight (only those with a viable model).
	provider := selectWeightedProvider(vk.ProviderConfigs, r.hasViableModel)
	if provider == nil {
		return nil, core.NewNoHealthyBackends(vk.CustomPoolName)
	}

	// Cursor scoped to this VK + provider — a different VK may have a different
	// allowed-model list, so its rotation must be independent.
	poolKey := vk.ID + "|" + string(provider.Provider)

	// STAGE 2 — round-robin a model within the provider's pool models.
	// A wildcard "*" expands to the provider's registry models (see providerModels).
	// Model rotation (poolModelCursors) is independent of deployment rotation,
	// so model share does not get skewed by how many keys a model happens to have.
	models := r.providerModels(provider)
	n := len(models)
	if n == 0 {
		return nil, core.NewNoHealthyBackends(vk.CustomPoolName)
	}
	start := int(r.nextPoolModel(poolKey) % uint64(n))
	for offset := 0; offset < n; offset++ {
		key := models[(start+offset)%n]

		deployments, found := r.registry.LookupModel(key)
		if !found {
			continue
		}

		// STAGE 3 — deployment via the configured algorithm. Fall through to
		// the next model if this one has no healthy deployment.
		if selected := selector.Deployment(key, deployments); selected != nil {
			return r.commit(rctx, key, selected), nil
		}
	}

	return nil, core.NewNoHealthyBackends(vk.CustomPoolName)
}

// nextPoolModel returns the next round-robin slot for a pool's model rotation,
// keyed by poolKey (VK + provider). Independent from deployment rotation.
func (r *Resolver) nextPoolModel(poolKey string) uint64 {
	value, ok := r.poolModelCursors.Load(poolKey)
	if !ok {
		value, _ = r.poolModelCursors.LoadOrStore(poolKey, new(atomic.Uint64))
	}
	return value.(*atomic.Uint64).Add(1) - 1
}

// providerModels returns the candidate model keys for a provider in a pool.
// A wildcard "*" expands to every model the provider currently serves in the
// deployment registry; otherwise the configured concrete models are used.
func (r *Resolver) providerModels(cfg *core.ProviderConfig) []core.ModelKey {
	if cfg == nil {
		return nil
	}

	for _, name := range cfg.AllowedModels {
		if name == "*" {
			return r.registry.ModelsForProvider(cfg.Provider)
		}
	}

	models := make([]core.ModelKey, 0, len(cfg.AllowedModels))
	for _, name := range cfg.AllowedModels {
		if name == "" {
			continue
		}
		models = append(models, core.ModelKey{Provider: cfg.Provider, ModelName: name})
	}
	return models
}

func (r *Resolver) hasHealthyDeployment(key core.ModelKey) bool {
	deployments, found := r.registry.LookupModel(key)
	if !found {
		return false
	}

	for _, deployment := range deployments {
		if isHealthy(deployment) {
			return true
		}
	}

	return false
}

// hasViableModel reports whether a provider has at least one pool model with a
// healthy deployment (wildcard-aware). Used to filter providers before the
// weighted draw.
func (r *Resolver) hasViableModel(cfg *core.ProviderConfig) bool {
	for _, key := range r.providerModels(cfg) {
		if r.hasHealthyDeployment(key) {
			return true
		}
	}
	return false
}

func (r *Resolver) commit(
	rctx *core.DiffractLLMContext,
	key core.ModelKey,
	deployment *core.Deployment,
) *core.Deployment {
	rctx.Modelkey = key
	rctx.SelectedDeployment = deployment
	return deployment
}

// Select among only the providers that are currently viable.
//
// There is no candidate-slice allocation. It makes two very small passes over
// provider configs: one to total weights, one to choose the random interval.
func selectWeightedProvider(
	configs []*core.ProviderConfig,
	viable func(*core.ProviderConfig) bool,
) *core.ProviderConfig {
	var totalWeight float64
	var viableCount int

	for _, cfg := range configs {
		if cfg == nil || !viable(cfg) {
			continue
		}

		viableCount++
		if cfg.Weight > 0 {
			totalWeight += float64(cfg.Weight)
		}
	}

	if viableCount == 0 {
		return nil
	}

	// Defensive fallback. Normal weighted VK validation should disallow this.
	if totalWeight <= 0 {
		target := rand.IntN(viableCount)

		for _, cfg := range configs {
			if cfg != nil && viable(cfg) {
				if target == 0 {
					return cfg
				}
				target--
			}
		}

		return nil
	}

	target := rand.Float64() * totalWeight

	for _, cfg := range configs {
		if cfg == nil || !viable(cfg) || cfg.Weight <= 0 {
			continue
		}

		target -= float64(cfg.Weight)
		if target < 0 {
			return cfg
		}
	}

	// Float rounding guard.
	for index := len(configs) - 1; index >= 0; index-- {
		cfg := configs[index]
		if cfg != nil && cfg.Weight > 0 && viable(cfg) {
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
