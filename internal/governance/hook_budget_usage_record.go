package governance

import (
	"diffractllm/internal/core"

	"go.uber.org/zap"
)

type RecordHook struct {
	budgetcache *BudgetCache
	usage       *UsageBuffer
	logger      *zap.Logger
}

func NewRecordHook(budgetcache *BudgetCache, UsageBuffer *UsageBuffer, logger *zap.Logger) *RecordHook {
	return &RecordHook{budgetcache: budgetcache, usage: UsageBuffer, logger: logger}
}

func (r *RecordHook) Name() string { return "record_usage" }

func (r *RecordHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	if rctx.Usage == nil {
		return nil
	}

	nano := core.ToNanoUSD(rctx.Cost)

	r.usage.Append(UsageRecord{
		ClientID:       rctx.ClientID,
		BudgetID:       rctx.BudgetRef,
		Backend:        string(rctx.Modelkey.Provider),
		ModelID:        rctx.UpstreamModel,
		ModelName:      rctx.Modelkey.ModelName,
		InputTokens:    rctx.Usage.InputTokens,
		OutputTokens:   rctx.Usage.OutputTokens,
		ResponseBytes:  rctx.ResponseBytes,
		ResponseStatus: rctx.ResponseStatus,
		Cost:           nano,
		RequestedAt:    rctx.StartedAt,
	})

	if budget, ok := r.budgetcache.LookupBudget(rctx.BudgetRef); ok {
		budget.RecordUsage(nano)
	}
	return nil

}
