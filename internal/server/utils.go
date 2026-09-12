package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/providers"
)

func writeErr(rctx *core.DiffractLLMContext, desc *providers.RouteDescriptor, err *core.DiffractLLMError) {
	rctx.ResponseStatus = err.StatusCode
	rctx.Error = err
	_, payload := desc.FromDiffractError(err)
	rctx.JSON(err.StatusCode, payload)
}
