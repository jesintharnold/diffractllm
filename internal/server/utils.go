package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/providers"
	"encoding/json"

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

func writeErr(rctx *core.DiffractLLMContext, desc *providers.RouteDescriptor, err *core.DiffractLLMError) {
	rctx.ResponseStatus = err.StatusCode
	rctx.Error = err
	_, payload := desc.FromDiffractError(err)
	rctx.JSON(err.StatusCode, payload)
}
