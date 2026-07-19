package governance

// dummyhooks.go — temporary hooks for validating the 4-stage pipeline.
// Every hook logs what it sees on RuteContext and passes through.
// Delete this file once real hook implementations (auth, budget, usage) are wired.

import (
	"diffractllm/internal/core"

	"go.uber.org/zap"
)

// ── PreCall ───────────────────────────────────────────────────────────────────

// dummyAuthHook simulates the future AuthHook.
// In PreCall: rctx.Provider, BodyBytes, Model, Backend are all populated.
// A real auth hook would validate the virtual key from rctx.Request.Header and
// hydrate ClientID, AllowedModels, BudgetRef, VirtualKeyID on rctx.
type dummyAuthHook struct{ logger *zap.Logger }

func (h *dummyAuthHook) Name() string { return "dummy-auth" }

func (h *dummyAuthHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	authHeader := rctx.Request.Header.Get("Authorization")

	h.logger.Info("[PRE-CALL] dummy-auth hook executing",
		zap.String("provider", string(rctx.Modelkey.Provider)),
		zap.String("authorization_header", authHeader),
		zap.String("route_model", routeModel(rctx)),
	)

	// Simulate hydrating governance fields that a real AuthHook would set
	rctx.ClientID = "dummy-client-001"
	rctx.VirtualKeyID = "vk-dummy-abc123"
	rctx.BudgetRef = "budget-dummy-001"

	h.logger.Info("[PRE-CALL] dummy-auth passed — governance fields hydrated",
		zap.String("client_id", rctx.ClientID),
		zap.String("virtual_key_id", rctx.VirtualKeyID),
		zap.String("budget_ref", rctx.BudgetRef),
	)

	return nil // pass through
}

// ── PreProvider ───────────────────────────────────────────────────────────────

// dummyBudgetHook simulates the future BudgetEnforcementHook.
// In PreProvider: governance fields are hydrated, backend is confirmed.
// A real budget hook would check spend against the BudgetRef limit.
type dummyBudgetHook struct{ logger *zap.Logger }

func (h *dummyBudgetHook) Name() string { return "dummy-budget" }

func (h *dummyBudgetHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	h.logger.Info("[PRE-PROVIDER] dummy-budget hook executing",
		zap.String("client_id", rctx.ClientID),
		zap.String("budget_ref", rctx.BudgetRef),
		zap.Bool("auth_frozen", rctx.AuthFrozen),
	)

	// Simulate a budget check — always passes in dummy mode
	h.logger.Info("[PRE-PROVIDER] dummy-budget passed — spend within limit")

	return nil // pass through
}

// ── PostProvider ──────────────────────────────────────────────────────────────

// dummyUsageHook simulates the future UsageCaptureHook.
// In PostProvider: RequestCompleted, ResponseStatus, ResponseBytes are set.
// A real usage hook would write a record to ClickHouse or the usage store.
type dummyUsageHook struct{ logger *zap.Logger }

func (h *dummyUsageHook) Name() string { return "dummy-usage" }

func (h *dummyUsageHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	h.logger.Info("[POST-PROVIDER] dummy-usage hook executing",
		zap.String("client_id", rctx.ClientID),
		zap.String("budget_ref", rctx.BudgetRef),
		zap.Bool("request_completed", rctx.RequestCompleted),
		zap.Int("response_status", rctx.ResponseStatus),
		zap.Int("response_bytes", rctx.ResponseBytes),
	)

	if !rctx.RequestCompleted {
		h.logger.Warn("[POST-PROVIDER] dummy-usage skipped — provider was not reached")
		return nil
	}

	h.logger.Info("[POST-PROVIDER] dummy-usage recorded",
		zap.Int("bytes_delivered", rctx.ResponseBytes),
		zap.Int("http_status", rctx.ResponseStatus),
	)

	return nil // best-effort, never surfaces to caller
}

// ── PostCall ──────────────────────────────────────────────────────────────────

// dummyAuditHook simulates the future AuditLogHook.
// In PostCall: everything on rctx is final. Runs even if request was aborted.
// A real audit hook would write a structured request log entry.
type dummyAuditHook struct{ logger *zap.Logger }

func (h *dummyAuditHook) Name() string { return "dummy-audit" }

func (h *dummyAuditHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	h.logger.Info("[POST-CALL] dummy-audit hook executing",
		zap.String("provider", string(rctx.Modelkey.Provider)),
		zap.String("client_id", rctx.ClientID),
		zap.String("virtual_key_id", rctx.VirtualKeyID),
		zap.Bool("request_completed", rctx.RequestCompleted),
		zap.Int("response_status", rctx.ResponseStatus),
		zap.Int("response_bytes", rctx.ResponseBytes),
		zap.Int("pre_call_hooks_ran", rctx.HookLog.PreCallCount),
		zap.Int("pre_provider_hooks_ran", rctx.HookLog.PreProviderCount),
		zap.Int("post_provider_hooks_ran", rctx.HookLog.PostProviderCount),
	)

	return nil
}

func routeModel(rctx *core.DiffractLLMContext) string {
	if rctx.Modelkey.ModelName == "" {
		return "<none>"
	}
	return rctx.Modelkey.ModelName
}
