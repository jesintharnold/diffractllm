package core

type ModelKey struct {
	Provider  Provider
	ModelName string
}

func (m ModelKey) SlashKey() string {
	return string(m.Provider) + "/" + m.ModelName
}
