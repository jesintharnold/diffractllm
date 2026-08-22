package core

import "encoding/json"

type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleFunction  Role = "function"
)

type ChatContentType string

const (
	ContentText       ChatContentType = "text"
	ContentImageURL   ChatContentType = "image_url"
	ContentInputAudio ChatContentType = "input_audio"
	ContentRefusal    ChatContentType = "refusal"
	ContentInputFile  ChatContentType = "file"
)

type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	FinishRefusal       FinishReason = "refusal"
)

type ChatImageURL struct {
	URL    string  `json:"url,omitempty"`
	Detail *string `json:"detail,omitempty"`
}

type ChatInputAudio struct {
	Data   string  `json:"data"`
	Format *string `json:"format,omitempty"`
}

type ChatInputFile struct {
	FileID   *string `json:"file_id,omitempty"`
	FileName *string `json:"filename,omitempty"`
	FileData *string `json:"file_data,omitempty"`
	FileType *string `json:"file_type,omitempty"`
}

type ChatPromptCacheBreakpoint struct {
	Mode *string `json:"mode,omitempty"`
}

type ChatContentPart struct {
	Type                  ChatContentType            `json:"type"`
	Text                  *string                    `json:"text,omitempty"`
	ImageURL              *ChatImageURL              `json:"image_url,omitempty"`
	InputAudio            *ChatInputAudio            `json:"input_audio,omitempty"`
	Refusal               *string                    `json:"refusal,omitempty"`
	File                  *ChatInputFile             `json:"file,omitempty"`
	PromptCacheBreakpoint *ChatPromptCacheBreakpoint `json:"prompt_cache_breakpoint,omitempty"`
}

type ToolFunctionCall struct {
	Name      *string `json:"name"`
	Arguments string  `json:"arguments"`
}

type ToolCustomCall struct {
	Input string `json:"input"`
	Name  string `json:"name"`
}

type ToolCall struct {
	Index    *uint16           `json:"index,omitempty"`
	Type     *string           `json:"type,omitempty"`
	ID       *string           `json:"id,omitempty"`
	Function *ToolFunctionCall `json:"function,omitempty"`
	Custom   *ToolCustomCall   `json:"custom,omitempty"`
}

type ChatAssistantAudio struct {
	ID         string `json:"id"`
	ExpiresAt  int64  `json:"expires_at"`
	Data       string `json:"data"`
	Transcript string `json:"transcript"`
}

type ChatAudioParams struct {
	Format string `json:"format"` // wav | mp3 | flac | opus | pcm16
	Voice  string `json:"voice"`
}

type DiffractLLMChatMessage struct {
	Name       *string             `json:"name,omitempty"`
	Role       Role                `json:"role"`
	Content    []ChatContentPart   `json:"content,omitempty"`
	Refusal    *string             `json:"refusal,omitempty"`
	ToolCallID *string             `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall          `json:"tool_calls,omitempty"`
	Audio      *ChatAssistantAudio `json:"audio,omitempty"`
	Reasoning  *string             `json:"reasoning,omitempty"`
}

type ChatPrediction struct {
	Type    string `json:"type"`
	Content any    `json:"content"`
}

type ChatStreamOptions struct {
	IncludeUsage       bool `json:"include_usage,omitempty"`
	IncludeObfuscation bool `json:"include_obfuscation"`
}

type ChatPromptCacheOptions struct {
	Mode *string `json:"mode,omitempty"`
	TTL  *string `json:"ttl,omitempty"`
}

type ChatToolUserLocation struct {
	Type     *string `json:"type,omitempty"`
	City     *string `json:"city,omitempty"`
	Country  *string `json:"country,omitempty"`
	Region   *string `json:"region,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
}

type ChatWebSearchOptions struct {
	SearchContextSize *string               `json:"search_context_size,omitempty"`
	UserLocation      *ChatToolUserLocation `json:"user_location,omitempty"`
}

type ChatToolChoiceType string

const (
	ToolChoiceNone         ChatToolChoiceType = "none"
	ToolChoiceAuto         ChatToolChoiceType = "auto"
	ToolChoiceRequired     ChatToolChoiceType = "required"
	ToolChoiceFunction     ChatToolChoiceType = "function"
	ToolChoiceCustom       ChatToolChoiceType = "custom"
	ToolChoiceAllowedTools ChatToolChoiceType = "allowed_tools"
)

type ChatToolChoiceFunction struct {
	Name string `json:"name"`
}

type ChatToolChoiceCustom struct {
	Name string `json:"name"`
}

type ChatToolType string

const (
	ToolTypeFunction ChatToolType = "function"
	ToolTypeCustom   ChatToolType = "custom"
)

type ChatToolChoiceAllowedTool struct {
	Type     ChatToolType            `json:"type"`
	Function *ChatToolChoiceFunction `json:"function,omitempty"`
	Custom   *ChatToolChoiceCustom   `json:"custom,omitempty"`
}

type ChatToolChoiceAllowedTools struct {
	Mode  string                      `json:"mode"`
	Tools []ChatToolChoiceAllowedTool `json:"tools"`
}

type ChatToolChoice struct {
	Type         ChatToolChoiceType          `json:"type"`
	Function     *ChatToolChoiceFunction     `json:"function,omitempty"`
	Custom       *ChatToolChoiceCustom       `json:"custom,omitempty"`
	AllowedTools *ChatToolChoiceAllowedTools `json:"allowed_tools,omitempty"`
}

type ChatToolFunction struct {
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type ChatToolCustomGrammar struct {
	Syntax     string `json:"syntax"`
	Definition string `json:"definition"`
}

type ChatToolCustomFormat struct {
	Type    string                 `json:"type"`
	Grammar *ChatToolCustomGrammar `json:"grammar,omitempty"`
}

type ChatToolCustom struct {
	Name        string                `json:"name"`
	Description *string               `json:"description,omitempty"`
	Format      *ChatToolCustomFormat `json:"format,omitempty"`
}

type ChatTool struct {
	Type     ChatToolType      `json:"type"`
	Function *ChatToolFunction `json:"function,omitempty"`
	Custom   *ChatToolCustom   `json:"custom,omitempty"`
}

type Mode struct {
	Mode string `json:"mode"`
}

type ChatModerationPolicy struct {
	Input  *Mode `json:"input,omitempty"`
	Output *Mode `json:"output,omitempty"`
}

type ChatModerationRequest struct {
	Model  string                `json:"model"`
	Policy *ChatModerationPolicy `json:"policy,omitempty"`
}

type ChatModerationResult struct {
}

type DiffractLLMChatParameters struct {
	Store                *bool                   `json:"store,omitempty"`
	ReasoningEffort      *string                 `json:"reasoning_effort,omitempty"`
	Metadata             *map[string]any         `json:"metadata,omitempty"`
	FrequencyPenalty     *float64                `json:"frequency_penalty,omitempty"`
	LogitBias            *map[string]float64     `json:"logit_bias,omitempty"`
	LogProbs             *bool                   `json:"logprobs,omitempty"`
	TopLogprobs          *int                    `json:"top_logprobs,omitempty"`
	MaxCompletionTokens  *int                    `json:"max_completion_tokens,omitempty"`
	Modalities           []string                `json:"modalities,omitempty"`
	Moderation           *ChatModerationRequest  `json:"moderation,omitempty"`
	N                    *int                    `json:"n,omitempty"`
	ParallelToolCalls    *bool                   `json:"parallel_tool_calls,omitempty"`
	PresencePenalty      *float64                `json:"presence_penalty,omitempty"`
	Seed                 *int64                  `json:"seed,omitempty"`
	ServiceTier          *string                 `json:"service_tier,omitempty"`
	Stop                 []string                `json:"stop,omitempty"`
	Stream               *bool                   `json:"stream,omitempty"`
	TopP                 *float64                `json:"top_p,omitempty"`
	User                 *string                 `json:"user,omitempty"`
	Prediction           *ChatPrediction         `json:"prediction,omitempty"`
	Audio                *ChatAudioParams        `json:"audio,omitempty"`
	ResponseFormat       any                     `json:"response_format,omitempty"`
	StreamOptions        *ChatStreamOptions      `json:"stream_options,omitempty"`
	Temperature          *float64                `json:"temperature,omitempty"`
	PromptCacheKey       *string                 `json:"prompt_cache_key,omitempty"`
	PromptCacheOptions   *ChatPromptCacheOptions `json:"prompt_cache_options,omitempty"`
	PromptCacheRetention *string                 `json:"prompt_cache_retention,omitempty"`
	WebSearchOptions     *ChatWebSearchOptions   `json:"web_search_options,omitempty"`
	SafetyIdentifier     *string                 `json:"safety_identifier,omitempty"`
	ToolChoice           *ChatToolChoice         `json:"tool_choice,omitempty"`
	Tools                []ChatTool              `json:"tools,omitempty"`
}

type DiffractLLMChatCompletionRequest struct {
	Model      string                     `json:"model"`
	Messages   []DiffractLLMChatMessage   `json:"messages"`
	Parameters *DiffractLLMChatParameters `json:"parameters,omitempty"`
	Raw        json.RawMessage            `json:"-"`
}

type ChatLogProb struct {
	Token   string  `json:"token"`
	LogProb float64 `json:"logprob"`
	Bytes   []int   `json:"bytes"`
}

type ChatContentLogProb struct {
	Bytes       []int         `json:"bytes"`
	LogProb     float64       `json:"logprob"`
	Token       string        `json:"token"`
	TopLogProbs []ChatLogProb `json:"top_logprobs"`
}

type ChatLogProbs struct {
	Content []ChatContentLogProb `json:"content,omitempty"`
	Refusal []ChatContentLogProb `json:"refusal,omitempty"`
}

type ChatResponseChoice struct {
	Index        int                    `json:"index"`
	Message      DiffractLLMChatMessage `json:"message"`
	FinishReason *FinishReason          `json:"finish_reason,omitempty"`
	LogProbs     *ChatLogProbs          `json:"logprobs,omitempty"`
}

type DiffractLLMChatCompletionResponse struct {
	ID                string               `json:"id"`
	Choices           []ChatResponseChoice `json:"choices"`
	Created           int                  `json:"created"`
	Model             string               `json:"model"`
	Object            string               `json:"object"`
	Metadata          *map[string]any      `json:"metadata,omitempty"`
	Moderation        any                  `json:"moderation,omitempty"`
	ServiceTier       *string              `json:"service_tier,omitempty"`
	SystemFingerprint string               `json:"system_fingerprint"`
	Usage             *Usage               `json:"usage"`
	Raw               json.RawMessage      `json:"-"`
}
