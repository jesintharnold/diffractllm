package core

type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderAzure     Provider = "azure"
	ProviderCohere    Provider = "cohere"
	ProviderOllama    Provider = "ollama"
	ProviderCustom    Provider = "custom"
)
