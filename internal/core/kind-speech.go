package core

import (
	"time"
)

type DiffractLLMSpeechMetadata struct {
	Provider   string        `json:"provider,omitempty"`
	Model      string        `json:"model,omitempty"`
	Voice      string        `json:"voice,omitempty"`
	ChunkIndex int64         `json:"chunk_index"`
	Latency    time.Duration `json:"latency"`
}

type DiffractLLMSpeechUsage struct {
	InputTokens  int32 `json:"input_tokens,omitempty"`
	OutputTokens int32 `json:"output_tokens,omitempty"`
	TotalTokens  int32 `json:"total_tokens,omitempty"`
}

type DiffractLLMSpeechStreamEvent string

const (
	DiffractLLMSpeechStreamEventInProgress DiffractLLMSpeechStreamEvent = "speech.audio.delta"
	DiffractLLMSpeechStreamEventComplete   DiffractLLMSpeechStreamEvent = "speech.audio.done"
	DiffractLLMSpeechStreamEventError      DiffractLLMSpeechStreamEvent = "speech.audio.error"
)

type DiffractLLMSpeechRequest struct {
	Provider         Provider `json:"provider"`
	Model            string   `json:"model"`
	Input            string   `json:"input"`
	Voice            string   `json:"voice"`
	StreamingRequest bool
	Params           DiffractLLMSpeechExtraParams
}

type DiffractLLMSpeechExtraParams struct {
	Instructions   string   `json:"instructions,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
	Speed          *float32 `json:"speed,omitempty"`
	StreamFormat   string   `json:"stream_format,omitempty"`
}

type DiffractLLMSpeechResponse struct {
	Audio    []byte                     `json:"audio"`
	Usage    *DiffractLLMSpeechUsage    `json:"usage"`
	Metadata *DiffractLLMSpeechMetadata `json:"metadata"`
}

type DiffractLLMSpeechStreamResponse struct {
	Type     DiffractLLMSpeechStreamEvent `json:"type"`
	Audio    []byte                       `json:"audio"`
	Usage    *DiffractLLMSpeechUsage      `json:"usage"`
	Metadata *DiffractLLMSpeechMetadata   `json:"metadata"`
	Error    *DiffractLLMError            `json:"error,omitempty"`
}
