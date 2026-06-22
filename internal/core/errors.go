package core

import (
	"fmt"
	"net/http"

	"github.com/bytedance/sonic"
)

type ErrorCategory string

const (
	ErrorCategoryClient   ErrorCategory = "client_error"
	ErrorCategoryGateway  ErrorCategory = "gateway_error"
	ErrorCategoryProvider ErrorCategory = "provider_error"
)

type ErrorCode string

const (
	CodeNoHealthyBackends ErrorCode = "no_healthy_backends"
	CodeRouteNotFound     ErrorCode = "route_not_found"
	CodeRouteTableEmpty   ErrorCode = "route_table_empty"
	CodeConfigValidation  ErrorCode = "config_validation_failed"
	CodeConfigReload      ErrorCode = "config_reload_failed"
	CodeProxyError        ErrorCode = "proxy_error"
	CodeInternalError     ErrorCode = "internal_error"

	CodeUpstreamTimeout     ErrorCode = "upstream_timeout"
	CodeUpstreamError       ErrorCode = "upstream_error"
	CodeUpstreamAuth        ErrorCode = "upstream_auth_error"
	CodeUpstreamRateLimit   ErrorCode = "upstream_rate_limit"
	CodeUpstreamUnavailable ErrorCode = "upstream_unavailable"

	CodeMissingParameter   ErrorCode = "missing_parameter"
	CodeInvalidParameter   ErrorCode = "invalid_parameter"
	CodeInvalidRequestBody ErrorCode = "invalid_request_body"
	CodeAuthFailed         ErrorCode = "auth_failed"
	CodeForbidden          ErrorCode = "forbidden"
	CodeBudgetExceeded     ErrorCode = "budget_exceeded"
	CodeInvalidBudget      ErrorCode = "invalid_budget"
)

type ExtraDetails map[string]interface{}

type DiffractLLMError struct {
	ErrorCategory     ErrorCategory `json:"error_category"`
	Code              ErrorCode     `json:"error_code"`
	Internal          error         `json:"-"`
	StatusCode        int           `json:"-"`
	Message           string        `json:"message"`
	Type              string        `json:"type"`
	Component         string        `json:"component,omitempty"`
	Provider          string        `json:"provider,omitempty"`
	Backend           string        `json:"backend,omitempty"`
	BackendURL        string        `json:"backend_url,omitempty"`
	RequestID         string        `json:"request_id,omitempty"`
	Parameter         *string       `json:"parameter,omitempty"`
	Details           ExtraDetails  `json:"extra_details,omitempty"`
	RetryAfter        int           `json:"retry_after,omitempty"`
	ProviderErrorCode string        `json:"provider_error_code,omitempty"`
	ProviderErrorType string        `json:"provider_error_type,omitempty"`
}

func (r *DiffractLLMError) Error() string {
	if r.Internal != nil {
		return fmt.Sprintf("[%s] : Status Code - [%d] Message - %s Internal - %v", r.ErrorCategory, r.StatusCode, r.Message, r.Internal)
	}
	return fmt.Sprintf("[%s] : Status Code - [%d] Message - %s", r.ErrorCategory, r.StatusCode, r.Message)
}

func (r *DiffractLLMError) IsGateway() bool {
	return r.ErrorCategory == ErrorCategoryGateway
}

func (r *DiffractLLMError) IsProvider() bool {
	return r.ErrorCategory == ErrorCategoryProvider
}

func (r *DiffractLLMError) IsClient() bool {
	return r.ErrorCategory == ErrorCategoryClient
}

// Marshal is only needed here that too for the uploading the native logs directly
// Unmarshal is not needed for now

func (r *DiffractLLMError) MarshalJSON() ([]byte, error) {
	type Alias DiffractLLMError
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	}

	if r.Internal != nil {
		if r.Details == nil {
			r.Details = make(ExtraDetails)
		}
		r.Details["raw_error"] = r.Internal.Error()
	}

	return sonic.Marshal(aux)
}

func NewNoHealthyBackends(backendName string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryGateway,
		Code:          CodeNoHealthyBackends,
		Message:       fmt.Sprintf("No healthy instances available for backend '%s'", backendName),
		Type:          "gateway_error",
		StatusCode:    http.StatusServiceUnavailable,
		Component:     "loadbalancer",
		Backend:       backendName,
	}
}

func NewRouteNotFound(path, method string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryGateway,
		Code:          CodeRouteNotFound,
		Message:       fmt.Sprintf("No route found for %s %s", method, path),
		Type:          "gateway_error",
		StatusCode:    http.StatusNotFound,
		Component:     "routeengine",
		Details: map[string]interface{}{
			"path":   path,
			"method": method,
		},
	}
}

func NewRouteTableEmpty() *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryGateway,
		Code:          CodeRouteTableEmpty,
		Message:       "Routing table is not initialized",
		Type:          "gateway_error",
		StatusCode:    http.StatusServiceUnavailable,
		Component:     "routeengine",
		Details: map[string]interface{}{
			"internal_detail": "Check if policy.yaml is loaded correctly",
		},
	}
}

func NewConfigValidationError(message string, cause error) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryGateway,
		Code:          CodeConfigValidation,
		Message:       "Configuration validation failed",
		Type:          "gateway_error",
		StatusCode:    http.StatusInternalServerError,
		Component:     "configmanager",
		Internal:      cause,
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewConfigReloadError(message string, cause error) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryGateway,
		Code:          CodeConfigReload,
		Message:       "Failed to reload configuration",
		Type:          "gateway_error",
		StatusCode:    http.StatusInternalServerError,
		Component:     "configmanager",
		Internal:      cause,
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewProxyError(message string, cause error) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryGateway,
		Code:          CodeProxyError,
		Message:       "Proxy error occurred",
		Type:          "gateway_error",
		StatusCode:    http.StatusBadGateway,
		Component:     "reverseproxy",
		Internal:      cause,
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewInternalError(component, message string, cause error) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryGateway,
		Code:          CodeInternalError,
		Message:       "Internal server error",
		Type:          "gateway_error",
		StatusCode:    http.StatusInternalServerError,
		Component:     component,
		Internal:      cause,
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewUpstreamTimeout(provider, backendURL string, cause error) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryProvider,
		Code:          CodeUpstreamTimeout,
		Message:       fmt.Sprintf("Request to %s timed out", provider),
		Type:          "provider_error",
		StatusCode:    http.StatusGatewayTimeout,
		Component:     "provider",
		Provider:      provider,
		BackendURL:    backendURL,
		Internal:      cause,
		Details: map[string]interface{}{
			"internal_detail": fmt.Sprintf("Backend %s did not respond in time", backendURL),
		},
	}
}

func NewUpstreamError(provider, backendURL string, statusCode int, message string, cause error) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryProvider,
		Code:          CodeUpstreamError,
		Message:       fmt.Sprintf("Provider %s returned an error", provider),
		Type:          "provider_error",
		StatusCode:    statusCode,
		Component:     "provider",
		Provider:      provider,
		BackendURL:    backendURL,
		Internal:      cause,
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewUpstreamAuth(provider, backendURL, message string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryProvider,
		Code:          CodeUpstreamAuth,
		Message:       fmt.Sprintf("Authentication failed with %s", provider),
		Type:          "provider_error",
		StatusCode:    http.StatusBadGateway,
		Component:     "provider",
		Provider:      provider,
		BackendURL:    backendURL,
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewUpstreamRateLimit(provider, backendURL string, retryAfter int) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryProvider,
		Code:          CodeUpstreamRateLimit,
		Message:       fmt.Sprintf("Rate limit exceeded on %s", provider),
		Type:          "provider_error",
		StatusCode:    http.StatusTooManyRequests,
		Component:     "provider",
		Provider:      provider,
		BackendURL:    backendURL,
		RetryAfter:    retryAfter,
	}
}

func NewUpstreamUnavailable(provider, backendURL string, cause error) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryProvider,
		Code:          CodeUpstreamUnavailable,
		Message:       fmt.Sprintf("Provider %s is currently unavailable", provider),
		Type:          "provider_error",
		StatusCode:    http.StatusBadGateway,
		Component:     "provider",
		Provider:      provider,
		BackendURL:    backendURL,
		Internal:      cause,
		Details: map[string]interface{}{
			"internal_detail": "Service is down or unreachable",
		},
	}
}

func NewMissingParameter(parameter string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryClient,
		Code:          CodeMissingParameter,
		Message:       fmt.Sprintf("Missing required parameter: %s", parameter),
		Type:          "invalid_request_error",
		StatusCode:    http.StatusBadRequest,
		Component:     "validator",
		Parameter:     &parameter,
	}
}

func NewInvalidParameter(parameter, reason string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryClient,
		Code:          CodeInvalidParameter,
		Message:       fmt.Sprintf("Invalid parameter '%s': %s", parameter, reason),
		Type:          "invalid_request_error",
		StatusCode:    http.StatusBadRequest,
		Component:     "validator",
		Parameter:     &parameter,
	}
}

func NewInvalidRequestBody(message string, cause error) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryClient,
		Code:          CodeInvalidRequestBody,
		Message:       "Invalid request body",
		Type:          "invalid_request_error",
		StatusCode:    http.StatusBadRequest,
		Component:     "validator",
		Internal:      cause,
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewAuthFailed(message string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryClient,
		Code:          CodeAuthFailed,
		Message:       "Authentication failed",
		Type:          "authentication_error",
		StatusCode:    http.StatusUnauthorized,
		Component:     "auth",
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewForbidden(message string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryClient,
		Code:          CodeForbidden,
		Message:       "Access forbidden",
		Type:          "permission_error",
		StatusCode:    http.StatusForbidden,
		Component:     "auth",
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewInvalidBudget(message string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryGateway,
		Code:          CodeInvalidBudget,
		Message:       "Budget configuration error",
		Type:          "budget_error",
		StatusCode:    http.StatusInternalServerError,
		Component:     "budget",
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}

func NewBudgetExceeded(message string) *DiffractLLMError {
	return &DiffractLLMError{
		ErrorCategory: ErrorCategoryClient,
		Code:          CodeBudgetExceeded,
		Message:       "Budget limit exceeded",
		Type:          "budget_error",
		StatusCode:    http.StatusBadRequest,
		Component:     "budget",
		Details: map[string]interface{}{
			"internal_detail": message,
		},
	}
}
