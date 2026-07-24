package governance

import (
	"diffractllm/internal/core"

	"go.uber.org/zap"
)

type BudgetHook struct {
	BudgetCache *BudgetCache
	logger      *zap.Logger
}

func NewBudgetCheckHook(budgetcache *BudgetCache, logger *zap.Logger) *BudgetHook {
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
