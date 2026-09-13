package governance

import (
	"diffractllm/internal/core"

	"go.uber.org/zap"
)

func RegisterHooks(engine *core.HookEngine, logger *zap.Logger, governance *Governance, catalog ModelLookup) {
	engine.AddPreCallHook(NewModelAccessHook(catalog, logger))
	engine.AddPreCallHook(NewBudgetCheckHook(governance.BudgetCache, logger))
	engine.AddPostProviderHook(NewRecordHook(governance.BudgetCache, governance.UsageBuffer, logger))
}
