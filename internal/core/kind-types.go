package core

type RequestKind string

const (
	SpeechRequest RequestKind = "speech"
)

type DiffractLLMContextKey string

const (
	DiffractLLMSDKProvider      DiffractLLMContextKey = "rute-sdk-provider"
	DiffractLLMProvider         DiffractLLMContextKey = "rute-provider"
	DiffractLLMResponseProvider DiffractLLMContextKey = "rute-res-provider"
	DiffractLLMRequestKind      DiffractLLMContextKey = "rute-req-type"
	DiffractLLMRouteParams      DiffractLLMContextKey = "rute-route-params"
	DiffractLLMBodyBytes        DiffractLLMContextKey = "rute-body-bytes"
)
