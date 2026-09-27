package metricsengine

import (
	"diffractllm/internal/core"
	"encoding/json"
	"time"
)

const SchemaVersion = 1

type EventKind string

const KindLLM EventKind = "llm"

type Result string

const (
	ResultOK            Result = "ok"
	ResultRejected      Result = "rejected"
	ResultUpstreamError Result = "upstream_error"
	ResultGatewayError  Result = "gateway_error"
	ResultClientAbort   Result = "client_abort"
)

type PayloadState string

const (
	PayloadNone      PayloadState = "none"
	PayloadReady     PayloadState = "ready"
	PayloadTruncated PayloadState = "truncated"
	PayloadDropped   PayloadState = "dropped"
)

type Governance struct {
	ClientID     string `json:"client_id,omitempty"`
	VirtualKeyID string `json:"virtual_key_id,omitempty"`
	BudgetID     string `json:"budget_id,omitempty"`
}

type Outcome struct {
	Result             Result `json:"result"`
	HTTPStatus         int    `json:"http_status"`
	UpstreamHTTPStatus int    `json:"upstream_http_status,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	ErrorCategory      string `json:"error_category,omitempty"`
	ErrorMessage       string `json:"error_message,omitempty"`
}

type Timing struct {
	Start   time.Time `json:"start"`
	TotalUS int64     `json:"total_us"`
	TTFBUS  int64     `json:"ttfb_us,omitempty"`
	TTFTUS  int64     `json:"ttft_us,omitempty"`
	Stages  []Stage   `json:"stages,omitempty"`
}

type Stage struct {
	Name      string `json:"name"`
	OffsetUS  int64  `json:"offset_us"`
	ElapsedUS int64  `json:"elapsed_us"`
}

type Attempt struct {
	OffsetUS   int64  `json:"offset_us"`
	ElapsedUS  int64  `json:"elapsed_us"`
	HTTPStatus int    `json:"http_status,omitempty"`
	End        string `json:"end"`
	Error      string `json:"error,omitempty"`
}

type LLM struct {
	RequestedModel string           `json:"requested_model"`
	Provider       string           `json:"provider"`
	Model          string           `json:"model"`
	Stream         bool             `json:"stream"`
	Params         json.RawMessage  `json:"params,omitempty"`
	Usage          *core.Usage      `json:"usage,omitempty"`
	Pricing        *core.Pricing    `json:"pricing,omitempty"`
	FinishReason   string           `json:"finish_reason,omitempty"`
	ToolCalls      []string         `json:"tool_calls,omitempty"`
}

type Payload struct {
	ClientRequest      json.RawMessage `json:"client_request,omitempty"`
	NormalizedRequest  json.RawMessage `json:"normalized_request,omitempty"`
	ProviderRequest    json.RawMessage `json:"provider_request,omitempty"`
	ProviderResponse   json.RawMessage `json:"provider_response,omitempty"`
	NormalizedResponse json.RawMessage `json:"normalized_response,omitempty"`
	ClientResponse     json.RawMessage `json:"client_response,omitempty"`
}

type Routing struct {
	Mode         core.VKMode `json:"mode"`
	CredentialID string      `json:"credential_id"`
	Attempts     []Attempt   `json:"attempts,omitempty"`
}

type Event struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	RequestID     string `json:"request_id,omitempty"`

	Kind            EventKind        `json:"kind"`
	RequestKind     core.RequestKind `json:"request_kind"` // chat | embedding | speech | … — set by the route
	RequestEndpoint string           `json:"request_endpoint"`

	CostNanoUSD  int64             `json:"cost_nano_usd"`
	
	Governance   Governance        `json:"governance"`
	Outcome      Outcome           `json:"outcome"`
	Timing       Timing            `json:"timing"`
	Routing      *Routing          `json:"routing,omitempty"`
	LLM          *LLM              `json:"llm,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	PayloadState PayloadState      `json:"payload_state"`
	Payload      *Payload          `json:"payload,omitempty"`
}
