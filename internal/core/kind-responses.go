package core

import "encoding/json"

type ResponseItemType string

const (
	ItemMessage         ResponseItemType = "message"
	ItemReasoning       ResponseItemType = "reasoning"
	ItemFunctionCall    ResponseItemType = "function_call"
	ItemFunctionOutput  ResponseItemType = "function_call_output"
	ItemServerToolCall  ResponseItemType = "server_tool_call"
	ItemApprovalRequest ResponseItemType = "approval_request"
	ItemReference       ResponseItemType = "item_reference"
	ItemUnmodelled      ResponseItemType = "unmodelled"
)

type ItemStatus string

const (
	ItemInProgress   ItemStatus = "in_progress"
	ItemCompleted    ItemStatus = "completed"
	ItemIncomplete   ItemStatus = "incomplete"
	ItemInterpreting ItemStatus = "interpreting"
	ItemFailed       ItemStatus = "failed"
)

type ReasoningSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResponseItem struct {
	Type             ResponseItemType   `json:"type"`
	ID               string             `json:"id,omitempty"`
	Status           ItemStatus         `json:"status,omitempty"`
	Role             Role               `json:"role,omitempty"`
	Content          []ContentPart      `json:"content,omitempty"`
	Phase            *string            `json:"phase,omitempty"`
	Summary          []ReasoningSummary `json:"summary,omitempty"`
	EncryptedContent *string            `json:"encrypted_content,omitempty"`
	CallID           string             `json:"call_id,omitempty"`
	Name             string             `json:"name,omitempty"`
	Arguments        json.RawMessage    `json:"arguments,omitempty"`
	Output           *string            `json:"output,omitempty"`
	ServerToolName   ServerToolType     `json:"server_tool_name,omitempty"`
	ApprovalID       string             `json:"approval_id,omitempty"`
	Approve          *bool              `json:"approve,omitempty"`
	Reason           *string            `json:"reason,omitempty"`
	PayloadType      string             `json:"payload_type,omitempty"`
	Payload          json.RawMessage    `json:"payload,omitempty"`
}

type ReasoningConfig struct {
	Effort    *string `json:"effort,omitempty"`
	Summary   *string `json:"summary,omitempty"`
	MaxTokens *int    `json:"max_tokens,omitempty"`
}

type TextConfig struct {
	Format    *ResponseFormat `json:"format,omitempty"`
	Verbosity *string         `json:"verbosity,omitempty"`
}

type ResponsesParams struct {
	MaxOutputTokens   *int             `json:"max_output_tokens,omitempty"`
	MaxToolCalls      *int             `json:"max_tool_calls,omitempty"`
	Temperature       *float64         `json:"temperature,omitempty"`
	TopP              *float64         `json:"top_p,omitempty"`
	TopLogProbs       *int             `json:"top_logprobs,omitempty"`
	Stream            bool             `json:"stream,omitempty"`
	Background        bool             `json:"background,omitempty"`
	Tools             []Tool           `json:"tools,omitempty"`
	ToolChoice        *string          `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool            `json:"parallel_tool_calls,omitempty"`
	Reasoning         *ReasoningConfig `json:"reasoning,omitempty"`
	Text              *TextConfig      `json:"text,omitempty"`
	ServiceTier       ServiceTier      `json:"service_tier,omitempty"`
	Truncation        *string          `json:"truncation,omitempty"`
	PromptCacheKey    *string          `json:"prompt_cache_key,omitempty"`
	User              *string          `json:"user,omitempty"`
}

// -- Responses request --

type DiffractLLMResponsesRequest struct {
	Model              string          `json:"model"`
	Input              []ResponseItem  `json:"input"`
	Instructions       *string         `json:"instructions,omitempty"`
	PreviousResponseID *string         `json:"previous_response_id,omitempty"`
	Store              *bool           `json:"store,omitempty"`
	Include            []string        `json:"include,omitempty"`
	Params             ResponsesParams `json:"params,omitzero"`
	Raw                []byte          `json:"-"`
	Extra              map[string]any  `json:"-"`
}

// -- Responses response --

type ResponseStatus string

const (
	ResponseQueued     ResponseStatus = "queued"
	ResponseInProgress ResponseStatus = "in_progress"
	ResponseCompleted  ResponseStatus = "completed"
	ResponseIncomplete ResponseStatus = "incomplete"
	ResponseFailed     ResponseStatus = "failed"
)

type DiffractLLMResponsesResponse struct {
	ID               string         `json:"id"`
	CreatedAt        int64          `json:"created_at"`
	Status           ResponseStatus `json:"status"`
	IncompleteReason FinishReason   `json:"incomplete_reason,omitempty"`
	Output           []ResponseItem `json:"output"`
	Usage            Usage          `json:"usage,omitzero"`
	Metadata         Metadata       `json:"metadata,omitzero"`
	Raw              []byte         `json:"-"`
	Extra            map[string]any `json:"-"`
}

// -- Responses stream --

type ResponsesDeltaTarget string

const (
	DeltaOutputText       ResponsesDeltaTarget = "output_text"
	DeltaReasoningSummary ResponsesDeltaTarget = "reasoning_summary"
	DeltaFunctionArgs     ResponsesDeltaTarget = "function_arguments"
	DeltaRefusal          ResponsesDeltaTarget = "refusal"
	DeltaCode             ResponsesDeltaTarget = "code"
)

type DiffractLLMResponsesStreamChunk struct {
	Type             StreamEventType      `json:"type"`
	ID               string               `json:"id,omitempty"`
	Sequence         int64                `json:"sequence,omitempty"`
	ItemIndex        int                  `json:"item_index"`
	ItemID           string               `json:"item_id,omitempty"`
	Target           ResponsesDeltaTarget `json:"target,omitempty"`
	Delta            *string              `json:"delta,omitempty"`
	Item             *ResponseItem        `json:"item,omitempty"`
	Status           ResponseStatus       `json:"status,omitempty"`
	IncompleteReason FinishReason         `json:"incomplete_reason,omitempty"`
	Usage            Usage                `json:"usage,omitzero"`
	Metadata         Metadata             `json:"metadata,omitzero"`
	Error            *DiffractLLMError    `json:"error,omitempty"`
	Raw              []byte               `json:"-"`
}
