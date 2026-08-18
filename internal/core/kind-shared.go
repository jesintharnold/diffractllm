package core

import (
	"encoding/json"
	"strings"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentType string

const (
	ContentText             ContentType = "text"
	ContentImageURL         ContentType = "image_url"
	ContentInputAudio       ContentType = "input_audio"
	ContentFile             ContentType = "file"
	ContentRefusal          ContentType = "refusal"
	ContentThinking         ContentType = "thinking"
	ContentServerToolUse    ContentType = "server_tool_use"
	ContentServerToolResult ContentType = "server_tool_result"
)

type ServerToolType string

const (
	ServerToolWebSearch       ServerToolType = "web_search"
	ServerToolWebFetch        ServerToolType = "web_fetch"
	ServerToolCodeInterpreter ServerToolType = "code_interpreter"
	ServerToolComputer        ServerToolType = "computer"
	ServerToolFileSearch      ServerToolType = "file_search"
	ServerToolMemory          ServerToolType = "memory"
	ServerToolMCP             ServerToolType = "mcp"
)

type CacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type ContentPart struct {
	Type           ContentType     `json:"type"`
	Text           *string         `json:"text,omitempty"`
	Signature      *string         `json:"signature,omitempty"`
	ImageURL       *string         `json:"image_url,omitempty"`
	ImageDetail    *string         `json:"image_detail,omitempty"`
	AudioData      *string         `json:"audio_data,omitempty"`
	AudioFormat    *string         `json:"audio_format,omitempty"`
	FileID         *string         `json:"file_id,omitempty"`
	FileName       *string         `json:"file_name,omitempty"`
	FileData       *string         `json:"file_data,omitempty"`
	ServerToolName ServerToolType  `json:"server_tool_name,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	CacheControl   *CacheControl   `json:"cache_control,omitempty"`
}

type ToolKind string

const (
	ToolKindFunction ToolKind = "function"
	ToolKindServer   ToolKind = "server"
)

type ServerTool struct {
	Type    ServerToolType  `json:"type"`
	Version string          `json:"version,omitempty"`
	Config  json.RawMessage `json:"config,omitempty"`
}

type Tool struct {
	Kind        ToolKind        `json:"kind"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
	Server      *ServerTool     `json:"server,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCallDelta struct {
	Index     int     `json:"index"`
	ID        *string `json:"id,omitempty"`
	Name      *string `json:"name,omitempty"`
	Arguments string  `json:"arguments,omitempty"`
}

type Message struct {
	Role       Role          `json:"role"`
	Content    []ContentPart `json:"content,omitempty"`
	Name       string        `json:"name,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	Refusal    *string       `json:"refusal,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

func TextMessage(role Role, text string) Message {
	return Message{Role: role, Content: []ContentPart{{Type: ContentText, Text: &text}}}
}

func (m *Message) Text() string {
	if len(m.Content) == 1 && m.Content[0].Type == ContentText && m.Content[0].Text != nil {
		return *m.Content[0].Text
	}
	var b strings.Builder
	for i := range m.Content {
		if m.Content[i].Type == ContentText && m.Content[i].Text != nil {
			b.WriteString(*m.Content[i].Text)
		}
	}
	return b.String()
}

type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	FinishRefusal       FinishReason = "refusal"
	FinishError         FinishReason = "error"
)

type ResponseFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict *bool           `json:"strict,omitempty"`
}

type StreamEventType string

const (
	StreamDelta    StreamEventType = "delta"
	StreamComplete StreamEventType = "complete"
	StreamError    StreamEventType = "error"
)

type Metadata struct {
	Provider     Provider      `json:"provider"`
	Model        string        `json:"model"`
	CredentialID string        `json:"credential_id"`
	RequestID    string        `json:"request_id,omitempty"`
	TTFB         time.Duration `json:"ttfb,omitempty"`
	Latency      time.Duration `json:"latency,omitempty"`
	ChunkIndex   int64         `json:"chunk_index,omitempty"`
	Dropped      []string      `json:"dropped_params,omitempty"`
}
