package core

type ChatParams struct {
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	TopK                *int            `json:"top_k,omitempty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	Seed                *int64          `json:"seed,omitempty"`
	Stop                []string        `json:"stop,omitempty"`
	N                   *int            `json:"n,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	StreamUsage         bool            `json:"stream_usage,omitempty"`
	LogProbs            bool            `json:"logprobs,omitempty"`
	TopLogProbs         *int            `json:"top_logprobs,omitempty"`
	Tools               []Tool          `json:"tools,omitempty"`
	ToolChoice          *string         `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
	ResponseFormat      *ResponseFormat `json:"response_format,omitempty"`
	ReasoningEffort     *string         `json:"reasoning_effort,omitempty"`
	ServiceTier         ServiceTier     `json:"service_tier,omitempty"`
	User                *string         `json:"user,omitempty"`
}

type DiffractLLMChatRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Params   ChatParams     `json:"params,omitzero"`
	Raw      []byte         `json:"-"`
	Extra    map[string]any `json:"-"`
}

func (r *DiffractLLMChatRequest) IsRaw() bool { return len(r.Raw) > 0 }

// --- chat completion  response --

type ChatChoice struct {
	Index        int          `json:"index"`
	Message      Message      `json:"message,omitzero"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	LogProbs     *LogProbs    `json:"logprobs,omitempty"`
}

type LogProbs struct {
	Content []TokenLogProb `json:"content,omitempty"`
	Refusal []TokenLogProb `json:"refusal,omitempty"`
}

type TokenLogProb struct {
	Token       string         `json:"token"`
	LogProb     float64        `json:"logprob"`
	Bytes       []byte         `json:"bytes,omitempty"`
	TopLogProbs []TokenLogProb `json:"top_logprobs,omitempty"`
}

type DiffractLLMChatResponse struct {
	ID       string         `json:"id"`
	Created  int64          `json:"created"`
	Choices  []ChatChoice   `json:"choices"`
	Usage    Usage          `json:"usage,omitzero"`
	Metadata Metadata       `json:"metadata,omitzero"`
	Raw      []byte         `json:"-"`
	Extra    map[string]any `json:"-"`
}

func (r *DiffractLLMChatResponse) IsRaw() bool { return len(r.Raw) > 0 }

// -- chat stream schema --

type MessageDelta struct {
	Role      Role            `json:"role,omitempty"`
	Content   *string         `json:"content,omitempty"`
	Refusal   *string         `json:"refusal,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature *string         `json:"signature,omitempty"`
	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

type DiffractLLMChatStreamChunk struct {
	Type         StreamEventType   `json:"type"`
	ID           string            `json:"id,omitempty"`
	Created      int64             `json:"created,omitempty"`
	Index        int               `json:"index"`
	Delta        MessageDelta      `json:"delta,omitzero"`
	FinishReason FinishReason      `json:"finish_reason,omitempty"`
	Usage        Usage             `json:"usage,omitzero"`
	Metadata     Metadata          `json:"metadata,omitzero"`
	Error        *DiffractLLMError `json:"error,omitempty"`
	Raw          []byte            `json:"-"`
}

// -- completion schema --

type CompletionChoice struct {
	Index        int          `json:"index"`
	Text         string       `json:"text"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	LogProbs     *LogProbs    `json:"logprobs,omitempty"`
}

type DiffractLLMCompletionRequest struct {
	Model  string     `json:"model"`
	Prompt []string   `json:"prompt"`
	Params ChatParams `json:"params,omitzero"`

	Raw   []byte         `json:"-"`
	Extra map[string]any `json:"-"`
}

type DiffractLLMCompletionResponse struct {
	ID       string             `json:"id"`
	Created  int64              `json:"created"`
	Choices  []CompletionChoice `json:"choices"`
	Usage    Usage              `json:"usage,omitzero"`
	Metadata Metadata           `json:"metadata,omitzero"`
	Raw      []byte             `json:"-"`
	Extra    map[string]any     `json:"-"`
}
