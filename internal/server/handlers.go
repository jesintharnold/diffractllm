package server

import (
	"bytes"
	"diffractllm/internal/core"
	"diffractllm/internal/providers"
	"io"
	"net/http"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

func (ds *DiffractLLMServer) logRequest(rctx *core.DiffractLLMContext) {
	fields := []zap.Field{
		zap.String("event", "request"),
		zap.String("request_id", rctx.RequestID),
		zap.String("sdk", string(rctx.SDKProvider)),
		zap.String("kind", string(rctx.RequestKind)),
		zap.String("model", rctx.Modelkey.SlashKey()),
		zap.String("requested_model", rctx.RequestedModel),
		zap.Int("response_status", rctx.ResponseStatus),
		zap.Int("upstream_status", rctx.UpstreamStatus),
		zap.Duration("ttfb", rctx.TTFB),
		zap.Int("response_bytes", rctx.ResponseBytes),
		zap.Bool("completed", rctx.RequestCompleted),
		zap.Duration("hooks_pre_call", rctx.HookLog.PreCallTotal),
		zap.Duration("hooks_pre_provider", rctx.HookLog.PreProviderTotal),
		zap.Duration("hooks_post_provider", rctx.HookLog.PostProviderTotal),
		zap.Duration("hooks_post_call", rctx.HookLog.PostCallTotal),
	}

	if rctx.VirtualKeyID != "" {
		fields = append(fields,
			zap.String("virtual_key_id", rctx.VirtualKeyID),
			zap.String("client_id", rctx.ClientID),
			zap.String("budget_ref", rctx.BudgetRef))
	}
	if cred := rctx.SelectedCredential; cred != nil {
		fields = append(fields,
			zap.String("credential_id", cred.ID),
			zap.String("credential_name", cred.Name))
	}
	if rctx.StreamChunks > 0 || rctx.StreamAborted {
		fields = append(fields,
			zap.Int32("stream_chunks", rctx.StreamChunks),
			zap.String("finish_reason", string(rctx.StreamFinishReason)),
			zap.Bool("stream_aborted", rctx.StreamAborted))
	}
	if u := rctx.Usage; u != nil {
		fields = append(fields,
			zap.Int64("input_tokens", u.InputTokens),
			zap.Int64("output_tokens", u.OutputTokens),
			zap.Int64("cached_input_tokens", u.CachedInputTokens),
			zap.Int64("reasoning_tokens", u.ReasoningTokens),
			zap.Float64("cost", rctx.Cost))
	}
	if e := rctx.Error; e != nil {
		fields = append(fields,
			zap.String("error_code", string(e.Code)),
			zap.String("error_category", string(e.ErrorCategory)),
			zap.String("error_message", e.Message))
	}

	ds.logger.Info("request complete", fields...)
}

func (ds *DiffractLLMServer) readBody(rctx *core.DiffractLLMContext) ([]byte, *core.DiffractLLMError) {
	maxBody := int64(ds.config.MaxBodySize) << 10
	size := rctx.Request.ContentLength
	if size > maxBody {
		return nil, core.NewPayloadTooLarge(maxBody)
	}
	if size < 0 { // chunked; a declared 0 needs no buffer at all
		size = 64 << 10
	}
	buf := bytes.NewBuffer(make([]byte, 0, size))
	if _, err := buf.ReadFrom(io.LimitReader(rctx.Request.Body, maxBody)); err != nil {
		return nil, core.NewInvalidRequestBody("reading body", err)
	}
	if int64(buf.Len()) >= maxBody {
		var probe [1]byte
		if n, _ := rctx.Request.Body.Read(probe[:]); n > 0 {
			return nil, core.NewPayloadTooLarge(maxBody)
		}
	}
	return buf.Bytes(), nil
}

func (ds *DiffractLLMServer) GenericRequestHandler(w http.ResponseWriter, r *http.Request, desc *providers.RouteDescriptor) {
	rctx := ds.CtxPool.Acquire(r.Context(), r, w)
	defer func() {
		ds.HookEngine.RunPostCallHooks(rctx)
		ds.logRequest(rctx)
		ds.CtxPool.Release(rctx)
	}()

	rctx.RequestID = w.Header().Get("X-Request-ID")
	rctx.SDKProvider = desc.SDK
	rctx.RequestKind = desc.RequestKind
	rctx.StartedAt = time.Now()

	// Check virtual key authentication
	if authErr := ds.governance.ValidatevKeyAuth(rctx); authErr != nil {
		writeErr(rctx, desc, authErr)
		return
	}

	// Read the complete body
	body, err := ds.readBody(rctx)
	if err != nil {
		writeErr(rctx, desc, err)
		return
	}
	rctx.BodyBytes = body

	// Now convert the json into struct
	payloadStruct := desc.NewRequest()
	if err := sonic.Unmarshal(body, payloadStruct); err != nil {
		writeErr(rctx, desc, core.NewInvalidRequestBody("parsing request", err))
		return
	}

	// Now convert the sdk rquest into the Internal diffract LLM rquest
	dfRequest := desc.ToDiffract(payloadStruct, rctx)

	// Execute Pre call hooks
	if err = ds.HookEngine.RunPreCallHooks(rctx); err != nil {
		writeErr(rctx, desc, err)
		return
	}

	// Pick the provider and credential , pricing and provider instance + transport
	cred, err := ds.selectionEngine.Resolve(rctx)
	if err != nil {
		writeErr(rctx, desc, err)
		return
	}

	pricing := ds.ModelCatalog.ResolvePrice(rctx.VirtualKeyID, rctx.Modelkey, core.EmptySelectorKey)
	if pricing == nil {
		writeErr(rctx, desc, core.NewUnpricedModel(rctx.Modelkey))
		return
	}

	provInstance, dErr := ds.ProviderRegistry.Get(rctx.Modelkey.Provider)
	if dErr != nil {
		writeErr(rctx, desc, dErr)
		return
	}

	// Run Pre-provider hooks here
	if err = ds.HookEngine.RunPreProviderHooks(rctx); err != nil {
		writeErr(rctx, desc, err)
		return
	}

	defer ds.HookEngine.RunPostProviderHooks(rctx)

	// Redirect to the respective method

	switch desc.RequestKind {
	case core.ChatRequest:
		req := dfRequest.(*core.DiffractLLMChatCompletionRequest)
		ds.chatCompletion(rctx, desc, provInstance, cred, pricing, req)
	default:
		writeErr(rctx, desc, core.NewInternalError("server handler", "unknown request kind", nil))
		return
	}

}
