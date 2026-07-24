package governance

import (
	"diffractllm/internal/core"
	"strings"
	"time"

	"go.uber.org/zap"
)

type VirutalkeyHook struct {
	keyCache *VirtualkeyCache
	logger   *zap.Logger
}

func NewVirutalkeyAuthHook(keyCache *VirtualkeyCache, logger *zap.Logger) *VirutalkeyHook {
	return &VirutalkeyHook{keyCache: keyCache, logger: logger}
}

func (a *VirutalkeyHook) Name() string { return "auth" }

func (a *VirutalkeyHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
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
	rctx.VirtualKeyPolicy = vk // pointer to the shared immutable policy
	rctx.BudgetRef = vk.BudgetID
	rctx.AuthFrozen = true

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
