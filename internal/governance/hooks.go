package governance

import (
	"diffractllm/internal/core"
	"strings"
	"time"

	"go.uber.org/zap"
)

type AuthHook struct {
	keyCache *VirtualkeyCache
	logger   *zap.Logger
}

func NewAuthHook(keyCache *VirtualkeyCache, logger *zap.Logger) *AuthHook {
	return &AuthHook{keyCache: keyCache, logger: logger}
}

func (a *AuthHook) Name() string { return "auth" }

func (a *AuthHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	key := extractKey(rctx)
	if key == "" {
		return core.NewAuthFailed("missing api key — provide x-rute-key or Authorization: Bearer <key> or x-api-key or x-goog-api-key")
	}

	if !ValidateKeySignature(key) {
		return core.NewAuthFailed("Invalid RUTE API key format")
	}

	vk, found := a.keyCache.LookupVkey(key)
	if !found {
		return core.NewAuthFailed("RUTE API key not recognised")
	}

	if !vk.IsActive || (vk.ExpiresAt != nil && time.Now().After(*vk.ExpiresAt)) {
		return core.NewAuthFailed("Invalid RUTE API key")
	}

	rctx.ClientID = vk.ClientID
	rctx.VirtualKeyID = vk.ID
	rctx.VirtualKey = key
	rctx.BudgetRef = vk.BudgetID

	rctx.Mode = vk.Mode
	rctx.AllowedModels = vk.AllowedModels
	rctx.ModelPools = vk.ModelPools

	rctx.AuthFrozen = true

	if vk.Mode == core.VKAllowedModel && len(vk.AllowedModels) > 0 && rctx.Modelkey.ModelName != "" {
		if _, ok := vk.AllowedModels[rctx.Modelkey]; !ok {
			return core.NewForbidden("model " + rctx.Modelkey.ModelName + " is not permitted for this RUTE API key")
		}
	}

	if vk.Mode == core.VKModelPool && len(vk.ModelPools) > 0 && rctx.Modelkey.ModelName != "" {
		if _, ok := vk.ModelPools[rctx.Modelkey.ModelName]; !ok {
			return core.NewForbidden("model pool " + rctx.Modelkey.ModelName + " is not permitted for this RUTE API key")
		}
	}

	a.logger.Info("auth ok", zap.String("client", vk.ClientID), zap.String("RUTE API key prefix", vk.Key[:11]))
	return nil
}

func extractKey(rctx *core.DiffractLLMContext) string {
	if v := strings.TrimSpace(rctx.Request.Header.Get("x-rute-key")); v != "" {
		return v
	}
	auth := rctx.Request.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	if anthropicKey := strings.TrimSpace(rctx.Request.Header.Get("x-api-key")); anthropicKey != "" {
		return anthropicKey
	}
	if geminikey := strings.TrimSpace(rctx.Request.Header.Get("x-goog-api-key")); geminikey != "" {
		return geminikey
	}
	return ""
}

type BudgetHook struct {
	BudgetCache *BudgetCache
	logger      *zap.Logger
}

func NewBudgetHook(budgetcache *BudgetCache, logger *zap.Logger) *BudgetHook {
	return &BudgetHook{BudgetCache: budgetcache, logger: logger}
}

func (b *BudgetHook) Name() string { return "budget" }
func (b *BudgetHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	if rctx.BudgetRef == "" {
		return core.NewInvalidBudget("budget reference not set on virtual key")
	}

	budget, ok := b.BudgetCache.LookupBudget(rctx.BudgetRef)
	if !ok {
		return core.NewInvalidBudget(rctx.BudgetRef)
	}

	if !budget.CheckBudgetUsage() {
		b.logger.Warn("budget exceeded", zap.String("client", rctx.ClientID), zap.String("budget_ref", rctx.BudgetRef))
		return core.NewBudgetExceeded("Budget exceeded for the RUTE API key used")
	}
	b.logger.Info("budget ok", zap.String("client", rctx.ClientID), zap.String("budget_ref", rctx.BudgetRef))
	return nil
}

func RegisterHooks(engine *core.HookEngine, logger *zap.Logger, governance *Governance) {
	// engine.AddPreCallHook(NewAuthHook(governance.KeyCache, logger))
	// engine.AddPreCallHook(NewBudgetHook(governance.BudgetCache, logger))
	engine.AddPreProviderHook(&dummyBudgetHook{logger: logger})
	engine.AddPostProviderHook(&dummyUsageHook{logger: logger})
	engine.AddPostCallHook(&dummyAuditHook{logger: logger})
}
