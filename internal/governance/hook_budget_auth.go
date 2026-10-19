package governance

import (
	"diffractllm/internal/observability/logging"
	"diffractllm/internal/core"

	"go.uber.org/zap"
)

type BudgetHook struct {
	BudgetCache *BudgetCache
	logger      *zap.Logger
}

func NewBudgetCheckHook(budgetcache *BudgetCache) *BudgetHook {
	return &BudgetHook{BudgetCache: budgetcache, logger: logging.For("governance")}
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
		b.logger.Debug("budget exceeded", zap.String("client", rctx.ClientID), zap.String("budget_ref", rctx.BudgetRef))
		return core.NewBudgetExceeded("window spend reached the budget limit")
	}
	b.logger.Debug("budget ok", zap.String("client", rctx.ClientID), zap.String("budget_ref", rctx.BudgetRef))
	return nil
}
