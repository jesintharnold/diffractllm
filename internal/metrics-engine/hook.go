package metricsengine

import (
	"diffractllm/internal/core"
)

type EventHook struct {
	metrics *MetricsEngine
}

func NewEventHook(metrics *MetricsEngine) *EventHook {
	return &EventHook{metrics: metrics}
}

func (h *EventHook) Name() string { return "metrics_event" }

func (h *EventHook) Execute(rctx *core.DiffractLLMContext) *core.DiffractLLMError {
	h.metrics.AddEvent(rctx)
	return nil
}
func RegisterHooks(engine *core.HookEngine, metrics *MetricsEngine) error {
	return engine.AddPostCallHook(NewEventHook(metrics))
}
