package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/providers"
)

func (ds *DiffractLLMServer) chatCompletion(
	rctx *core.DiffractLLMContext, desc *providers.RouteDescriptor,
	prov providers.Provider, cred *core.Credential,
	pricing *core.Pricing, req *core.DiffractLLMChatCompletionRequest) {

	if req.IsStreaming() {

	} else {

	}

}
