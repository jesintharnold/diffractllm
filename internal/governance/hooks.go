package governance

import (
	"diffractllm/internal/core"

	"go.uber.org/zap"
)

func RegisterHooks(engine *core.HookEngine, logger *zap.Logger, governance *Governance, catalog ModelLookup) {
	engine.AddPreCallHook(NewVirutalkeyAuthHook(governance.KeyCache, logger))
	engine.AddPreCallHook(NewModelAccessHook(catalog, logger))
	engine.AddPreCallHook(NewBudgetCheckHook(governance.BudgetCache, logger))
	engine.AddPreProviderHook(&dummyBudgetHook{logger: logger})
	engine.AddPostProviderHook(&dummyUsageHook{logger: logger})
	engine.AddPostCallHook(&dummyAuditHook{logger: logger})
}
