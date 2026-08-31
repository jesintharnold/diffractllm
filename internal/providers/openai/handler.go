package openaiprovider

import (
	"bufio"
	"bytes"
	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"
	"errors"
	"io"
	"net/http"

	"github.com/bytedance/sonic"
)

const maxProviderErrorBodyBytes = 1 << 20

type ChatCompletionConfig struct {
	Provider core.Provider
	URL      string
	Model    string
	Request  *core.DiffractLLMChatCompletionRequest
	Headers  map[string]string
}

func HandleChatCompletion(rctx *core.DiffractLLMContext, transport *dataplane.DiffractLLMTransport, cfg *ChatCompletionConfig) (*core.DiffractLLMChatCompletionResponse, *core.DiffractLLMError) {
	safeURL := core.SanitizeBackendURL(cfg.URL)
	body, derr := BuildChatCompletionPayload(cfg.Request, cfg.Model, false)
	if derr != nil {
		return nil, derr
	}
	result, derr := transport.ServeHTTP(rctx, &dataplane.DiffractLLMTransportRequest{
		Method:  http.MethodPost,
		URL:     cfg.URL,
		Body:    body,
		Headers: cfg.Headers,
	})
	if derr != nil {
		return nil, derr
	}
	respBody, err := io.ReadAll(result.Body)
	result.Body.Close()
	if err != nil {
		return nil, core.NewUpstreamError(string(cfg.Provider), safeURL, result.Status, "reading response", err)
	}
	if result.Status != http.StatusOK {
		return nil, ParseError(cfg.Provider, safeURL, result.Status, respBody)
	}

	var wire OpenAIChatCompletionResponse
	if err := sonic.Unmarshal(respBody, &wire); err != nil {
		return nil, core.NewUpstreamError(string(cfg.Provider), safeURL, result.Status, "unmarshalling response", err)
	}
	wire.Raw = respBody
	return wire.ToDMChatCompletionResponse(), nil

}

func HandleChatCompletionStream(rctx *core.DiffractLLMContext, transport *dataplane.DiffractLLMTransport, cfg *ChatCompletionConfig) (<-chan *core.DiffractLLMChatCompletionStreamResponse, *core.DiffractLLMError) {
	safeURL := core.SanitizeBackendURL(cfg.URL)
	body, derr := BuildChatCompletionPayload(cfg.Request, cfg.Model, true)
	if derr != nil {
		return nil, derr
	}
	result, derr := transport.ServeHTTP(rctx, &dataplane.DiffractLLMTransportRequest{
		Method:      http.MethodPost,
		URL:         cfg.URL,
		Body:        body,
		Headers:     cfg.Headers,
		IsStreaming: true,
	})
	if derr != nil {
		return nil, derr
	}

	if result.Status != http.StatusOK {
		respBody, err := readProviderErrorBody(result.Body)
		if err != nil {
			return nil, core.NewUpstreamError(string(cfg.Provider), safeURL, result.Status, "reading streaming error response", err)
		}
		return nil, ParseError(cfg.Provider, safeURL, result.Status, respBody)
	}

	passthrough := rctx.SDKProvider == core.ProviderOpenAI
	responseChan := make(chan *core.DiffractLLMChatCompletionStreamResponse, 256)

	dataPrefix := []byte("data:")
	dataEnd := []byte("[DONE]")

	go func() {
		defer close(responseChan)
		defer result.Body.Close()

		sc := bufio.NewScanner(result.Body)
		sc.Buffer(make([]byte, 64*1024), 1024*1024) // 64kb --> 1MB grow size for each stream delta
		streamClosed := false
		emitError := func(streamErr *core.DiffractLLMError) {
			select {
			case responseChan <- &core.DiffractLLMChatCompletionStreamResponse{
				Type:  core.StreamEventError,
				Error: streamErr,
			}:
			case <-rctx.Context().Done():
			}
		}

		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 || line[0] == ':' {
				continue
			}
			if !bytes.HasPrefix(line, dataPrefix) {
				continue
			}

			delta := bytes.TrimSpace(bytes.TrimPrefix(line, dataPrefix))
			if len(delta) == 0 {
				continue
			}

			if bytes.Equal(delta, dataEnd) {
				streamClosed = true
				break
			}

			var wire OpenAIChatCompletionStreamResponse
			if err := sonic.Unmarshal(delta, &wire); err != nil {
				emitError(core.NewUpstreamError(string(cfg.Provider), safeURL, http.StatusBadGateway, "invalid OpenAI SSE data frame", err))
				return
			}
			if wire.Object != objectChatCompletionChunk {
				emitError(ParseError(cfg.Provider, safeURL, http.StatusBadGateway, delta))
				return
			}
			chunk := wire.ToDMChatCompletionStreamResponse()
			if passthrough {
				chunk.Raw = append([]byte(nil), delta...)
			}

			select {
			case responseChan <- chunk:
			case <-rctx.Context().Done():
				return
			}

		}

		if rctx.Context().Err() != nil {
			return
		}

		var err error
		switch {
		case sc.Err() != nil:
			err = sc.Err()
		case !streamClosed:
			err = errors.New("stream ended before [DONE]")
		default:
			return
		}

		streamErr := core.NewUpstreamUnavailable(string(cfg.Provider), safeURL, err)
		if errors.Is(err, dataplane.ErrStreamIdle) {
			streamErr = core.NewUpstreamTimeout(string(cfg.Provider), safeURL, err)
		}
		emitError(streamErr)
	}()
	return responseChan, nil
}

func readProviderErrorBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()

	reader := io.LimitReader(body, maxProviderErrorBodyBytes+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(data) > maxProviderErrorBodyBytes {
		return nil, errors.New("provider error response exceeds size limit")
	}
	return data, nil
}
