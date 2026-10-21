package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/providers"
	"encoding/json"
	"strconv"

	"github.com/bytedance/sonic"
)

func (ds *DiffractLLMServer) price(rctx *core.DiffractLLMContext, pricing *core.Pricing) {
	if rctx.Usage == nil || pricing == nil {
		return
	}
	rctx.Cost = core.FromNanoUSD(core.ToNanoUSD(core.CalculateCost(*pricing, *rctx.Usage)))
}

func writeSSE(rctx *core.DiffractLLMContext, event string, payload any) error {
	body, ok := payload.(json.RawMessage)
	if !ok {
		marshalled, err := sonic.Marshal(payload)
		if err != nil {
			return err
		}
		body = marshalled
	}
	return rctx.WriteSSE(event, body)
}

// writeJSON sends raw upstream bytes as they are and marshals everything else.
func writeJSON(rctx *core.DiffractLLMContext, code int, payload any) {
	raw, ok := payload.(json.RawMessage)
	if !ok {
		rctx.JSON(code, payload)
		return
	}
	rctx.ResponseBody = raw
	rctx.WriteData(code, "application/json; charset=utf-8", raw)
}

func writeErr(rctx *core.DiffractLLMContext, desc *providers.RouteDescriptor, err *core.DiffractLLMError) {
	rctx.ResponseStatus = err.StatusCode
	rctx.Error = err
	if err.RetryAfter > 0 {
		rctx.SetHeader("Retry-After", strconv.Itoa(err.RetryAfter))
	}
	_, payload := desc.FromDiffractError(err)
	rctx.JSON(err.StatusCode, payload)
}
