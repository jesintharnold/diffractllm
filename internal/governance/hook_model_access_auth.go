package governance

import (
	"diffractllm/internal/core"
	"strings"

	"go.uber.org/zap"
)

type ModelAccessHook struct {
	logger *zap.Logger
}

func NewModelAccessHook(logger *zap.Logger) *ModelAccessHook {
	return &ModelAccessHook{
		logger: logger,
	}
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

	// check for format provider/model
	providerName, modelName, explicit := strings.Cut(requested, "/")
	if explicit {
		if providerName == "" || modelName == "" {
			hook.logger.Warn("model access rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("requested_model", requested), zap.String("reason", "invalid provider/model format"))
			return core.NewInvalidParameter("model", "must use provider/model format")
		}

		requestedKey := core.ModelKey{
			Provider:  core.Provider(providerName),
			ModelName: modelName,
		}

		for _, config := range virtualKey.ProviderConfigs {
			if config.IsModelAllowed(requestedKey) {
				rctx.Modelkey = requestedKey
				hook.logger.Debug("model access allowed", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("provider", providerName), zap.String("model", modelName))
				return nil
			}
		}

		hook.logger.Warn("model access rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("provider", providerName), zap.String("model", modelName),
			zap.String("reason", "provider/model not permitted"))
		return core.NewForbidden("requested provider/model is not permitted")
	}

	// check for the custom pool name if used

	if virtualKey.CustomPoolName != "" &&
		requested == virtualKey.CustomPoolName {
		hook.logger.Debug("custom pool access allowed", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("custom_pool", requested))
		return nil
	}

	// Incase simply like model-name is used here
	for _, config := range virtualKey.ProviderConfigs {
		requestedKey := core.ModelKey{
			Provider:  config.Provider,
			ModelName: requested,
		}
		if config.IsModelAllowed(requestedKey) {
			rctx.Modelkey = core.ModelKey{ModelName: requested}
			hook.logger.Debug("model access allowed", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("model", requested))
			return nil
		}
	}

	hook.logger.Warn("model access rejected", zap.String("virtual_key_id", rctx.VirtualKeyID), zap.String("model", requested), zap.String("reason", "model not permitted"))
	return core.NewForbidden("requested model is not permitted")
}
