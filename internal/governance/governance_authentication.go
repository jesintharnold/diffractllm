package governance

import (
	"diffractllm/internal/core"
	"strings"
	"time"

	"go.uber.org/zap"
)

func (g *Governance) ValidatevKeyAuth(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	key := extractKey(rctx)
	if key == "" {
		return core.NewAuthFailed("missing api key — provide x-diffract-key or Authorization: Bearer <key> or x-api-key or x-goog-api-key")
	}

	if !core.ValidateKeySignature(key) {
		return core.NewAuthFailed("Invalid DiffractLLM API key format")
	}

	vk, found := g.KeyCache.LookupVkey(key)
	if !found {
		return core.NewAuthFailed("DiffractLLM API key not recognised")
	}

	if !vk.IsActive || (vk.ExpiresAt != nil && time.Now().After(*vk.ExpiresAt)) {
		return core.NewAuthFailed("Invalid DiffractLLM API key")
	}

	rctx.ClientID = vk.ClientID
	rctx.VirtualKeyID = vk.ID
	rctx.VirtualKeyPolicy = vk // pointer to the shared immutable policy
	rctx.BudgetRef = vk.BudgetID
	rctx.AuthFrozen = true
	g.logger.Debug("auth ok", zap.String("client", vk.ClientID), zap.String("virtual_key_id", vk.ID))
	return nil
}

func extractKey(rctx *core.DiffractLLMContext) string {
	if v := strings.TrimSpace(rctx.Request.Header.Get("x-diffract-key")); v != "" {
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
