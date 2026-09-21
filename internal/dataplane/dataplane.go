package dataplane

import (
	"diffractllm/internal/core"
	"math/rand/v2"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

type CredentialSource interface {
	Candidates(key core.CatalogKey) []*core.Credential
}

type Selector interface {
	Pick(key core.CatalogKey, creds []*core.Credential) *core.Credential
}

type SelectionEngine struct {
	providerplane CredentialSource
	selectors     map[core.LBKind]Selector
	logger        *zap.Logger
}

func NewSelectionEngine(plane CredentialSource, logger *zap.Logger) *SelectionEngine {
	return &SelectionEngine{
		providerplane: plane,
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
	se.logger.Warn("load balancer not implemented, using round robin", zap.String("requested", kind.String()))
	return se.selectors[core.LBRoundRobin]
}

func (se *SelectionEngine) Resolve(rctx *core.DiffractLLMContext) (*core.Credential, *core.DiffractLLMError) {
	if rctx == nil || rctx.VirtualKeyID == "" || rctx.VirtualKeyPolicy == nil {
		return nil, core.NewAuthFailed("virtual key policy missing ,contact the adminstrator")
	}

	vk := rctx.VirtualKeyPolicy
	if len(vk.ProviderConfigs) == 0 {
		return nil, core.NewAuthFailed("virtual key policy provider configs are missing ,contact the adminstrator")
	}

	rkey := rctx.Modelkey
	if rkey.ModelName == "" {
		return nil, core.NewMissingParameter("model")
	}

	// Explicit mode: the client named the provider, so no weighting runs and no
	if rkey.Provider != "" {
		return se.resolveExplicit(rctx, rkey, vk)
	}
	return se.resolveWeighted(rctx, rkey, vk)
}

func (se *SelectionEngine) resolveExplicit(rctx *core.DiffractLLMContext, key core.CatalogKey, vk *core.VirtualKey) (*core.Credential, *core.DiffractLLMError) {
	if vk.ProviderConfig(key.Provider) == nil {
		se.logger.Warn("selection rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("provider", string(key.Provider)), zap.String("reason", "provider is not configured on this key"))
		return nil, core.NewForbidden("provider " + string(key.Provider) + " is not permitted for this key")
	}

	creds := se.providerplane.Candidates(key)
	if len(creds) == 0 {
		return nil, core.NewNoHealthyBackends(key.SlashKey())
	}

	chosen := se.kind(vk.LoadBalancer).Pick(key, creds)
	if chosen == nil {
		return nil, core.NewNoHealthyBackends(key.SlashKey())
	}
	return se.commit(rctx, key, chosen), nil
}

func (se *SelectionEngine) resolveWeighted(rctx *core.DiffractLLMContext, key core.CatalogKey, vk *core.VirtualKey) (*core.Credential, *core.DiffractLLMError) {
	viable := make(map[core.Provider][]*core.Credential, len(vk.ProviderConfigs))
	for _, cfg := range vk.ProviderConfigs {
		creds := se.providerplane.Candidates(core.CatalogKey{
			Provider:  cfg.Provider,
			ModelName: key.ModelName,
			ModelType: key.ModelType,
		})
		if len(creds) > 0 {
			viable[cfg.Provider] = creds
		}
	}
	if len(viable) == 0 {
		return nil, core.NewNoHealthyBackends(key.ModelName)
	}

	provider := se.pickProvider(viable, vk)
	if provider == "" {
		return nil, core.NewNoHealthyBackends(key.ModelName)
	}

	key.Provider = provider
	chosen := se.kind(vk.LoadBalancer).Pick(key, viable[provider])
	if chosen == nil {
		return nil, core.NewNoHealthyBackends(key.SlashKey())
	}
	return se.commit(rctx, key, chosen), nil
}

func (se *SelectionEngine) pickProvider(viable map[core.Provider][]*core.Credential, vk *core.VirtualKey) core.Provider {
	var total float64
	for _, cfg := range vk.ProviderConfigs {
		if _, ok := viable[cfg.Provider]; ok {
			total += float64(cfg.Weight)
		}
	}

	target := rand.Float64() * total
	var last core.Provider
	for _, cfg := range vk.ProviderConfigs {
		if _, ok := viable[cfg.Provider]; !ok {
			continue
		}
		last = cfg.Provider
		if target -= float64(cfg.Weight); target < 0 {
			return cfg.Provider
		}
	}
	return last
}

func (se *SelectionEngine) commit(rctx *core.DiffractLLMContext, key core.CatalogKey, cred *core.Credential) *core.Credential {
	rctx.Modelkey = key
	rctx.SelectedCredential = cred
	return cred
}

type roundRobin struct {
	cursors sync.Map
}

func newRoundRobin() *roundRobin {
	return &roundRobin{}
}

func (r *roundRobin) Pick(key core.CatalogKey, creds []*core.Credential) *core.Credential {
	switch len(creds) {
	case 0:
		return nil
	case 1:
		return creds[0]
	}

	value, ok := r.cursors.Load(key)
	if !ok {
		value, _ = r.cursors.LoadOrStore(key, new(atomic.Uint64))
	}

	temp := value.(*atomic.Uint64).Add(1)
	return creds[temp%uint64(len(creds))]
}
