package openaiprovider

import (
	"diffractllm/internal/core"
	"errors"
	"net/http"

	"github.com/bytedance/sonic"
)

type OpenAIErrorResponse struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
		Param   string `json:"param"`
	} `json:"error"`
}

func ParseError(provider core.Provider, safeURL string, status int, body []byte) *core.DiffractLLMError {
	var e OpenAIErrorResponse
	_ = sonic.Unmarshal(body, &e)
	msg := string(body)
	if e.Error != nil && e.Error.Message != "" {
		msg = e.Error.Message
	}
	name := string(provider)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return core.NewUpstreamAuth(name, safeURL, msg)
	case http.StatusTooManyRequests:
		return core.NewUpstreamRateLimit(name, safeURL, 0)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return core.NewUpstreamTimeout(name, safeURL, errors.New(msg))
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return core.NewUpstreamUnavailable(name, safeURL, errors.New(msg))
	default:
		out := core.NewUpstreamError(name, safeURL, status, msg, nil)
		if e.Error != nil {
			out.ProviderErrorType = e.Error.Type
			out.ProviderErrorCode = e.Error.Code
		}
		return out
	}
}
