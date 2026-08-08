package core

import (
	"math/bits"
)

type Capability uint32

const (
	CapFunctionCalling Capability = 1 << iota
	CapParallelToolCalls
	CapToolChoice
	CapReasoning
	CapVision
	CapImageInput
	CapAudioInput
	CapAudioOutput
	CapPDFInput
	CapVideoInput
	CapPromptCaching
	CapResponseSchema
	CapSystemMessages
	CapStreaming
	CapWebSearch
	CapComputerUse
	CapAssistantPrefill
	CapEmbeddingImageInput
)

var capabilityMap = map[string]Capability{
	"function_calling":      CapFunctionCalling,     // CapFunctionCalling
	"parallel_tool_calls":   CapParallelToolCalls,   // CapParallelToolCalls
	"tool_choice":           CapToolChoice,          // CapToolChoice
	"reasoning":             CapReasoning,           // CapReasoning
	"vision":                CapVision,              // CapVision
	"image_input":           CapImageInput,          // CapImageInput
	"audio_input":           CapAudioInput,          // CapAudioInput
	"audio_output":          CapAudioOutput,         // CapAudioOutput
	"pdf_input":             CapPDFInput,            // CapPDFInput
	"video_input":           CapVideoInput,          // CapVideoInput
	"prompt_caching":        CapPromptCaching,       // CapPromptCaching
	"response_schema":       CapResponseSchema,      // CapResponseSchema
	"system_messages":       CapSystemMessages,      // CapSystemMessages
	"streaming":             CapStreaming,           // CapStreaming
	"web_search":            CapWebSearch,           // CapWebSearch
	"computer_use":          CapComputerUse,         // CapComputerUse
	"assistant_prefill":     CapAssistantPrefill,    // CapAssistantPrefill
	"embedding_image_input": CapEmbeddingImageInput, // CapEmbeddingImageInput
}

var capabilityStrings = [...]string{
	"function_calling",
	"parallel_tool_calls",
	"tool_choice",
	"reasoning",
	"vision",
	"image_input",
	"audio_input",
	"audio_output",
	"pdf_input",
	"video_input",
	"prompt_caching",
	"response_schema",
	"system_messages",
	"streaming",
	"web_search",
	"computer_use",
	"assistant_prefill",
	"embedding_image_input",
}

func (c Capability) Has(f Capability) bool                { return c&f != 0 }
func (c Capability) SupportsAll(required Capability) bool { return c&required == required }

func (c Capability) Empty() bool { return c == 0 }

func (c Capability) String() []string {
	out := make([]string, 0, bits.OnesCount32(uint32(c)))
	for i := 0; i < len(capabilityStrings); i++ {
		if c&(1<<i) != 0 {
			out = append(out, capabilityStrings[i])
		}
	}
	return out
}

func ParseCapabilityStrings(cap []string) Capability {
	var c Capability
	for _, n := range cap {
		if bit, ok := capabilityMap[n]; ok {
			c |= bit
		}
	}
	return c
}

type ModelType uint8

const (
	ModelTypeUnknown ModelType = iota
	ModelTypeChat
	ModelTypeCompletion
	ModelTypeResponses
	ModelTypeEmbedding
	ModelTypeImageGeneration
	ModelTypeAudioTranscription
	ModelTypeAudioSpeech
	ModelTypeModeration
	ModelTypeRerank
	ModelTypeSearch
)

func (t ModelType) String() string {
	switch t {
	case ModelTypeChat:
		return "chat"
	case ModelTypeCompletion:
		return "completion"
	case ModelTypeResponses:
		return "responses"
	case ModelTypeEmbedding:
		return "embedding"
	case ModelTypeImageGeneration:
		return "image_generation"
	case ModelTypeAudioTranscription:
		return "audio_transcription"
	case ModelTypeAudioSpeech:
		return "audio_speech"
	case ModelTypeModeration:
		return "moderation"
	case ModelTypeRerank:
		return "rerank"
	case ModelTypeSearch:
		return "search"
	default:
		return "unknown"
	}
}

func ParseModelType(mode string) ModelType {
	switch mode {
	case "chat":
		return ModelTypeChat
	case "completion":
		return ModelTypeCompletion
	case "responses":
		return ModelTypeResponses
	case "embedding":
		return ModelTypeEmbedding
	case "image_generation":
		return ModelTypeImageGeneration
	case "audio_transcription":
		return ModelTypeAudioTranscription
	case "audio_speech":
		return ModelTypeAudioSpeech
	case "moderation":
		return ModelTypeModeration
	case "rerank":
		return ModelTypeRerank
	case "search":
		return ModelTypeSearch
	default:
		return ModelTypeUnknown
	}
}
