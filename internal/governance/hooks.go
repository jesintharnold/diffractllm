package governance

import (
	"diffractllm/internal/core"
)

func RegisterHooks(engine *core.HookEngine, governance *Governance, catalog ModelLookup) {
	engine.AddPreCallHook(NewModelAccessHook(catalog))
	engine.AddPreCallHook(NewBudgetCheckHook(governance.BudgetCache))
	engine.AddPostProviderHook(NewRecordHook(governance.BudgetCache, governance.UsageBuffer))
}
