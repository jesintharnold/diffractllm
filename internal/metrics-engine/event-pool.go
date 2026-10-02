package metricsengine

import (
	"sync"
	"time"

	"diffractllm/internal/core"

	"github.com/google/uuid"
)

var eventPool = sync.Pool{New: func() any { return new(Event) }}

func acquireEvent() *Event {
	return eventPool.Get().(*Event)
}

func releaseEvent(e *Event) {
	*e = Event{}
	eventPool.Put(e)
}

func (e *Event) fill(rctx *core.DiffractLLMContext) {
	id := uuid.Must(uuid.NewV7()).String()

	e.SchemaVersion = SchemaVersion
	e.ID = id
	e.RequestID = rctx.RequestID
	e.Kind = KindLLM
	e.RequestKind = rctx.RequestKind
	if rctx.Request != nil && rctx.Request.URL != nil {
		e.RequestEndpoint = rctx.Request.URL.Path
	}
	e.CostNanoUSD = core.ToNanoUSD(rctx.Cost)

	e.Governance = Governance{
		ClientID:     rctx.ClientID,
		VirtualKeyID: rctx.VirtualKeyID,
		BudgetID:     rctx.BudgetRef,
	}

	e.Outcome = Outcome{
		Result:             classify(rctx),
		HTTPStatus:         rctx.ResponseStatus,
		UpstreamHTTPStatus: rctx.UpstreamStatus,
	}
	if err := rctx.Error; err != nil {
		e.Outcome.ErrorCode = string(err.Code)
		e.Outcome.ErrorCategory = string(err.ErrorCategory)
		e.Outcome.ErrorMessage = err.Message
	}

	e.Timing = Timing{
		Start:   rctx.StartedAt.UTC(),
		TotalUS: time.Since(rctx.StartedAt).Microseconds(),
		TTFBUS:  rctx.TTFB.Microseconds(),
	}

	if rctx.SelectedCredential != nil {
		e.Routing = &Routing{CredentialID: rctx.SelectedCredential.ID, CredentialName: rctx.SelectedCredential.Name}
		if rctx.VirtualKeyPolicy != nil {
			e.Routing.Mode = rctx.VirtualKeyPolicy.Mode
		}
	}

	e.LLM = &LLM{
		RequestedModel: rctx.RequestedModel,
		Provider:       string(rctx.Modelkey.Provider),
		Model:          rctx.Modelkey.ModelName,
		UpstreamModel:  rctx.UpstreamModel,
		Stream:         rctx.StreamChunks > 0 || rctx.StreamAborted || rctx.StreamFinishReason != "",
		FinishReason:   string(rctx.StreamFinishReason),
		Pricing:        rctx.Pricing,
	}
	if rctx.Usage != nil {
		usage := *rctx.Usage
		e.LLM.Usage = &usage
	}

	e.PayloadState = PayloadNone
}

func classify(rctx *core.DiffractLLMContext) Result {
	switch {
	case rctx.StreamAborted:
		return ResultClientAbort
	case rctx.Error == nil && rctx.RequestCompleted:
		return ResultOK
	case rctx.Error == nil:
		return ResultGatewayError
	case rctx.Error.IsClient():
		return ResultRejected
	case rctx.Error.IsProvider():
		return ResultUpstreamError
	default:
		return ResultGatewayError
	}
}
