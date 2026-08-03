package core

type ModelKey struct {
	Provider  Provider
	ModelName string
}

func (m ModelKey) SlashKey() string {
	return string(m.Provider) + "/" + m.ModelName
}

type ModelMetaData struct {
	Provider             Provider
	ModelName            string
	BaseModel            string
	ContextWindow        int32
	MaxInputTokens       int32
	MaxOutputTokens      int32
	LongContextThreshold int32
	Capability           Capability
	ModelType            ModelType
	// Leaving endpoints as of now
}

func (md *ModelMetaData) Key() ModelKey {
	return ModelKey{Provider: md.Provider, ModelName: md.ModelName}
}

func (md *ModelMetaData) Supports(c Capability) bool { return md.Capability.Has(c) }
