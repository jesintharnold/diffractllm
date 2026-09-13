package openaiprovider

import (
	"diffractllm/internal/core"
	"encoding/json"

	"github.com/bytedance/sonic"
)

type OpenAIErrorBody struct {
	Message    string          `json:"message"`
	Type       string          `json:"type"`
	Code       string          `json:"code"`
	Param      string          `json:"param"`
	InnerError json.RawMessage `json:"innererror,omitempty"`
}

type OpenAIErrorResponse struct {
	Error *OpenAIErrorBody `json:"error"`
}

func ToOpenAIError(e *core.DiffractLLMError) *OpenAIErrorResponse {
	if e == nil {
		return nil
	}
	body := &OpenAIErrorBody{
		Message: e.Message,
		Type:    e.Type,
		Code:    string(e.Code),
	}

	if e.ProviderErrorType != "" {
		body.Type = e.ProviderErrorType
	}
	if e.ProviderErrorCode != "" {
		body.Code = e.ProviderErrorCode
	}
	if e.Parameter != nil {
		body.Param = *e.Parameter
	}
	body.InnerError = e.ProviderErrorDetail
	return &OpenAIErrorResponse{Error: body}
}


func ParseError(provider core.Provider, safeURL string, status int, body []byte) *core.DiffractLLMError {
	var e OpenAIErrorResponse
	_ = sonic.Unmarshal(body, &e)

	msg := string(body)
	if e.Error != nil && e.Error.Message != "" {
		msg = e.Error.Message
	}

	out := core.NewUpstreamError(string(provider), safeURL, status, msg, nil)
	out.Message = msg
	if e.Error != nil {
		out.ProviderErrorType = e.Error.Type
		out.ProviderErrorCode = e.Error.Code
		if e.Error.Param != "" {
			out.Parameter = &e.Error.Param
		}
		if len(e.Error.InnerError) > 0 && string(e.Error.InnerError) != "null" {
			out.ProviderErrorDetail = e.Error.InnerError
		}
	}
	return out
}
