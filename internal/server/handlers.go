package server

import (
	"bytes"
	"diffractllm/internal/core"
	"diffractllm/internal/modelcatalog"
	"diffractllm/internal/providers"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func (ds *DiffractLLMServer) logRequest(rctx *core.DiffractLLMContext) {
	// 4xx stays Info: bad keys and spent budgets come from clients and must not flood Warn.
	level := zapcore.InfoLevel
	if rctx.ResponseStatus >= http.StatusInternalServerError {
		level = zapcore.ErrorLevel
	}
	ce := ds.logger.Check(level, "request complete")
	if ce == nil {
		return
	}

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

	ce.Write(fields...)
}

const (
	maxPreallocBody = 1 << 20             // trust Content-Length this far; bigger bodies grow as bytes arrive
	chunkedBodyHint = 16 << 10            // no Content-Length
	maxPooledBody   = 2 * maxPreallocBody // headroom for MinRead and size-class rounding; bigger is dropped
)

var bodyPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func putBody(buf *bytes.Buffer) {
	if buf == nil || buf.Cap() > maxPooledBody {
		return
	}
	bodyPool.Put(buf)
}

func (ds *DiffractLLMServer) readBody(rctx *core.DiffractLLMContext) (*bytes.Buffer, *core.DiffractLLMError) {
	maxBody := int64(ds.config.MaxBodySize) << 10
	size := rctx.Request.ContentLength
	if size > maxBody {
		return nil, core.NewPayloadTooLarge(maxBody)
	}
	switch {
	case size < 0:
		size = chunkedBodyHint
	case size > maxPreallocBody:
		size = maxPreallocBody
	}

	buf := bodyPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.Grow(int(size) + bytes.MinRead) // MinRead spare stops ReadFrom doubling at EOF
	if _, err := buf.ReadFrom(io.LimitReader(rctx.Request.Body, maxBody)); err != nil {
		putBody(buf)
		return nil, core.NewInvalidRequestBody("reading body", err)
	}
	if int64(buf.Len()) >= maxBody {
		var probe [1]byte
		if n, _ := rctx.Request.Body.Read(probe[:]); n > 0 {
			putBody(buf)
			return nil, core.NewPayloadTooLarge(maxBody)
		}
	}
	return buf, nil
}

func (ds *DiffractLLMServer) GenericRequestHandler(w http.ResponseWriter, r *http.Request, desc *providers.RouteDescriptor) {
	rctx := ds.CtxPool.Acquire(r.Context(), r, w)
	var body *bytes.Buffer
	defer func() {
		ds.HookEngine.RunPostCallHooks(rctx)
		ds.logRequest(rctx)
		rctx.BodyBytes = nil // never alias a buffer another request may own
		putBody(body)        // after post-call hooks: metrics copies BodyBytes there
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
	rctx.Mark(core.StageAuth)

	// Read the complete body
	buf, err := ds.readBody(rctx)
	if err != nil {
		writeErr(rctx, desc, err)
		return
	}
	body = buf
	rctx.BodyBytes = buf.Bytes()
	rctx.Mark(core.StageReadBody)

	// Now convert the json into struct
	payloadStruct := desc.NewRequest()
	if err := sonic.Unmarshal(rctx.BodyBytes, payloadStruct); err != nil {
		writeErr(rctx, desc, core.NewInvalidRequestBody("parsing request", err))
		return
	}

	// Now convert the sdk rquest into the Internal diffract LLM rquest
	dfRequest := desc.ToDiffract(payloadStruct, rctx)
	rctx.Mark(core.StageParse)

	// Execute Pre call hooks
	if err = ds.HookEngine.RunPreCallHooks(rctx); err != nil {
		writeErr(rctx, desc, err)
		return
	}
	rctx.Mark(core.StageAdmission)

	// Pick the provider and credential , pricing and provider instance + transport
	cred, err := ds.selectionEngine.Resolve(rctx)
	if err != nil {
		writeErr(rctx, desc, err)
		return
	}

	pricing := ds.ModelCatalog.ResolvePrice(rctx.VirtualKeyID, rctx.Modelkey, core.EmptySelectorKey)
	if pricing == nil {
		// Catalog setting "When a model has no price": reject (default) or serve and bill $0.
		if ds.ModelCatalog.MissingPrice() != modelcatalog.MissingPriceChargeZero {
			writeErr(rctx, desc, core.NewUnpricedModel(rctx.Modelkey))
			return
		}
		model := rctx.Modelkey.SlashKey()
		if _, seen := ds.logWarnedUnpriced.LoadOrStore(model, struct{}{}); !seen {
			ds.logger.Warn("serving an unpriced model at zero cost", zap.String("model", model))
		}
		pricing = &core.Pricing{}
	}
	rctx.Pricing = pricing

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
	rctx.Mark(core.StageRouting)

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
