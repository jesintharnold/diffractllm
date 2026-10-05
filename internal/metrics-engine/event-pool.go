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

	total := time.Since(rctx.StartedAt)
	e.Timing = Timing{
		Start:   rctx.StartedAt.UTC(),
		TotalUS: total.Microseconds(),
		TTFBUS:  rctx.TTFB.Microseconds(),
		TTFTUS:  rctx.Marks[core.StageFirstToken].Microseconds(),
		Stages:  stages(rctx.Marks, total),
	}

	if rctx.SelectedCredential != nil {
		e.Routing = &Routing{
			CredentialID:   rctx.SelectedCredential.ID,
			CredentialName: rctx.SelectedCredential.Name,
			AttemptCount:   rctx.UpstreamAttempts,
		}
		if rctx.VirtualKeyPolicy != nil {
			e.Routing.Mode = rctx.VirtualKeyPolicy.Mode
		}
	}

	for _, uh := range core.UpstreamHeaders {
		if v, ok := rctx.Get(uh.Key); ok {
			if e.Headers == nil {
				e.Headers = make(map[string]string, len(core.UpstreamHeaders))
			}
			e.Headers[uh.Name], _ = v.(string)
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

func stages(marks [core.NumStages]time.Duration, total time.Duration) []Stage {
	var out []Stage
	var prev time.Duration
	for s, end := range marks {
		if end == 0 {
			continue
		}
		out = append(out, span(core.StageNames[s], prev, end))
		prev = end
	}
	if out != nil && total > prev {
		out = append(out, span("response", prev, total))
	}
	return out
}

func span(name string, start, end time.Duration) Stage {
	return Stage{Name: name, StartUS: start.Microseconds(), EndUS: end.Microseconds(), DurationUS: (end - start).Microseconds()}
}
