package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/providers"
	"net/http"

	"go.uber.org/zap"
)

const statusClientClosed = 499

func (ds *DiffractLLMServer) chatCompletion(
	rctx *core.DiffractLLMContext, desc *providers.RouteDescriptor,
	providerInstance providers.Provider, cred *core.Credential,
	pricing *core.Pricing, req *core.DiffractLLMChatCompletionRequest) {

	if req.IsStreaming() {

		if _, ok := rctx.Writer.(http.Flusher); !ok {
			writeErr(rctx, desc, core.NewInternalError("server-handler", "client does not support streaming", nil))
			return
		}

		streamResCh, err := providerInstance.ChatCompletionStream(rctx, req, cred)
		if err != nil {
			writeErr(rctx, desc, err)
			return
		}

		rctx.SetHeader("Content-Type", "text/event-stream")
		rctx.SetHeader("Cache-Control", "no-cache")
		rctx.SetHeader("Connection", "keep-alive")
		rctx.SetHeader("X-Accel-Buffering", "no")
		rctx.Writer.WriteHeader(http.StatusOK)
		rctx.ResponseStatus = http.StatusOK
		rctx.Flush()

		ctx := rctx.Context()

		for chunk := range streamResCh {
			select {
			case <-ctx.Done():
				rctx.StreamAborted = true
				rctx.ResponseStatus = statusClientClosed
				awaitStreamEnd(streamResCh)
				ds.price(rctx, pricing)
				return
			default:
			}

			if chunk == nil {
				continue
			}

			if chunk.Type == core.StreamEventError {
				event, payload := desc.FromDiffractError(chunk.Error)
				writeSSE(rctx, event, payload)
				rctx.Error = chunk.Error
				rctx.ResponseStatus = chunk.Error.StatusCode
				awaitStreamEnd(streamResCh)
				ds.price(rctx, pricing)
				return
			}

			if usage := chunk.Usage; usage != nil {
				if rctx.Usage == nil || usage.OutputTokens >= rctx.Usage.OutputTokens {
					rctx.Usage = usage
				}
			}
			if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != nil {
				rctx.StreamFinishReason = *chunk.Choices[0].FinishReason
			}

			rctx.StreamChunks++

			event, payload := desc.FromDiffractStream(chunk)
			if err := writeSSE(rctx, event, payload); err != nil {
				rctx.StreamAborted = true
				rctx.ResponseStatus = statusClientClosed
				ds.logger.Warn("sse write failed", zap.Error(err), zap.Int32("chunks", rctx.StreamChunks))
				awaitStreamEnd(streamResCh)
				ds.price(rctx, pricing)
				return
			}

		}

		if ctx.Err() != nil {
			rctx.StreamAborted = true
			rctx.ResponseStatus = statusClientClosed
			ds.price(rctx, pricing)
			return
		}

		event, payload := desc.StreamDone()
		if err := rctx.WriteSSE(event, payload); err != nil {
			rctx.StreamAborted = true
			rctx.ResponseStatus = statusClientClosed
			ds.price(rctx, pricing)
			return
		}

		rctx.RequestCompleted = true
		ds.price(rctx, pricing)
		return
	} else {
		upstreamRes, err := providerInstance.ChatCompletion(rctx, req, cred)
		if err != nil {
			writeErr(rctx, desc, err)
			return
		}

		rctx.Usage = upstreamRes.Usage
		ds.price(rctx, pricing)
		rctx.ResponseStatus = http.StatusOK
		rctx.JSON(http.StatusOK, desc.FromDiffract(upstreamRes))
		rctx.RequestCompleted = true
	}
}

func awaitStreamEnd(ch <-chan *core.DiffractLLMChatCompletionStreamResponse) {
	for range ch {
	}
}
