package governance

import (
	"diffractllm/internal/core"

	"go.uber.org/zap"
)

// RegisterHooks wires the governance hooks into the pipeline stages.
// Real hook implementations live in their own hook_*.go files; the dummy
// pass-through hooks remain until the full pipeline is enabled.
func RegisterHooks(engine *core.HookEngine, logger *zap.Logger, governance *Governance) {
	engine.AddPreCallHook(NewVirutalkeyAuthHook(governance.KeyCache, logger))
	engine.AddPreCallHook(NewModelAccessHook(logger))
	engine.AddPreCallHook(NewBudgetCheckHook(governance.BudgetCache, logger))
	engine.AddPreProviderHook(&dummyBudgetHook{logger: logger})
	engine.AddPostProviderHook(&dummyUsageHook{logger: logger})
	engine.AddPostCallHook(&dummyAuditHook{logger: logger})
}
