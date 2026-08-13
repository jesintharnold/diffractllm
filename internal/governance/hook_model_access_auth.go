package governance

import (
	"diffractllm/internal/core"
	"strings"

	"go.uber.org/zap"
)

type ModelLookup interface {
	Ready() bool
	HasModel(provider core.Provider, modelName string) bool
}

type ModelAccessHook struct {
	catalog ModelLookup
	logger  *zap.Logger
}

func NewModelAccessHook(catalog ModelLookup, logger *zap.Logger) *ModelAccessHook {
	return &ModelAccessHook{
		catalog: catalog,
		logger:  logger,
	}
}

func (hook *ModelAccessHook) catalogReady() bool {
	return hook.catalog != nil && hook.catalog.Ready()
}

func (hook *ModelAccessHook) modelInCatalog(virtualKey *core.VirtualKey, modelName string) bool {
	for _, config := range virtualKey.ProviderConfigs {
		probe := core.CatalogKey{Provider: config.Provider, ModelName: modelName}
		if config.IsModelAllowed(probe) && hook.catalog.HasModel(config.Provider, modelName) {
			return true
		}
	}
	return false
}

func (hook *ModelAccessHook) Name() string {
	return "model-access"
}

func (hook *ModelAccessHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	virtualKey := rctx.VirtualKeyPolicy
	if virtualKey == nil {
		hook.logger.Warn("model access rejected", zap.String("reason", "virtual key policy missing"))
		return core.NewAuthFailed("virtual key policy is missing")
	}

	requested := strings.TrimSpace(rctx.RequestedModel)
	if requested == "" {
		hook.logger.Warn("model access rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("reason", "model missing"))
		return core.NewMissingParameter("model")
	}

	if providerName, modelName := core.ParseModelString(requested, ""); providerName != "" {
		if config := virtualKey.ProviderConfig(providerName); config != nil {
			requestedKey := core.CatalogKey{Provider: config.Provider, ModelName: modelName}
			if config.IsModelAllowed(requestedKey) {
				if hook.catalogReady() && !hook.catalog.HasModel(config.Provider, modelName) {
					hook.logger.Warn("model access rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("provider", string(providerName)), zap.String("model", modelName),
						zap.String("reason", "model is not in the catalog"))
					return core.NewInvalidParameter("model", "unknown model "+requestedKey.SlashKey())
				}
				rctx.Modelkey = requestedKey
				hook.logger.Debug("model access allowed", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("provider", string(providerName)), zap.String("model", modelName))
				return nil
			}

			hook.logger.Warn("model access rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("provider", string(providerName)), zap.String("model", modelName),
				zap.String("reason", "model not permitted on requested provider"))
			return core.NewForbidden("requested provider/model is not permitted")
		}
	}

	requestedKey := core.CatalogKey{ModelName: requested}
	if virtualKey.IsModelKeyAllowed(requestedKey) {
		if hook.catalogReady() && !hook.modelInCatalog(virtualKey, requested) {
			hook.logger.Warn("model access rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("model", requested),
				zap.String("reason", "model is not in the catalog for any permitted provider"))
			return core.NewInvalidParameter("model", "unknown model "+requested)
		}
		rctx.Modelkey = requestedKey
		hook.logger.Debug("model access allowed", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("model", requested))
		return nil
	}

	hook.logger.Warn("model access rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("model", requested), zap.String("reason", "model not permitted"))
	return core.NewForbidden("requested model is not permitted")
}
