package core

type RequestKind string

const (
	ChatRequest          RequestKind = "chat"
	CompletionRequest    RequestKind = "completion"
	ResponsesRequest     RequestKind = "responses"
	EmbeddingRequest     RequestKind = "embedding"
	ImageGenRequest      RequestKind = "image_generation"
	ImageEditRequest     RequestKind = "image_edit"
	SpeechRequest        RequestKind = "speech"
	TranscriptionRequest RequestKind = "transcription"
	ModerationRequest    RequestKind = "moderation"
	ModelsRequest        RequestKind = "models"
)

func (k RequestKind) ModelType() ModelType {
	switch k {
	case ChatRequest:
		return ModelTypeChat
	case CompletionRequest:
		return ModelTypeCompletion
	case ResponsesRequest:
		return ModelTypeResponses
	case EmbeddingRequest:
		return ModelTypeEmbedding
	case ImageGenRequest:
		return ModelTypeImageGeneration
	case ImageEditRequest:
		return ModelTypeImageEdit
	case SpeechRequest:
		return ModelTypeAudioSpeech
	case TranscriptionRequest:
		return ModelTypeAudioTranscription
	case ModerationRequest:
		return ModelTypeModeration
	default:
		return ModelTypeUnknown
	}
}

type DiffractLLMContextKey string

const (
	DiffractLLMSDKProvider      DiffractLLMContextKey = "rute-sdk-provider"
	DiffractLLMProvider         DiffractLLMContextKey = "rute-provider"
	DiffractLLMResponseProvider DiffractLLMContextKey = "rute-res-provider"
	DiffractLLMRequestKind      DiffractLLMContextKey = "rute-req-type"
	DiffractLLMRouteParams      DiffractLLMContextKey = "rute-route-params"
	DiffractLLMBodyBytes        DiffractLLMContextKey = "rute-body-bytes"
)
