package core

import (
	"fmt"
	"slices"
	"time"
)

type CredentialSettings struct {
	Azure *AzureSettings `json:"azure,omitempty"`
}

func (cs *CredentialSettings) IsEmpty() bool {
	return cs.Azure == nil
}

func (cs *CredentialSettings) Validate(provider Provider) error {
	check := 0

	if cs.Azure != nil {
		check++
		if provider != ProviderAzure {
			return fmt.Errorf("azure provider settings on a %q credential", provider)
		}

		if err := cs.Azure.Validate(); err != nil {
			return err
		}
	}

	// when other settings are added here

	if check > 1 {
		return fmt.Errorf("only one provider settings block may be set, got %d", check)
	}

	return nil
}

type Credential struct {
	ID            string             `json:"id,omitempty"`
	Provider      Provider           `json:"provider" binding:"required"`
	Name          string             `json:"name,omitempty"`
	APIKey        string             `json:"api_key,omitempty"`
	Enabled       bool               `json:"enabled"`
	ExpiryAt      *time.Time         `json:"expires_at,omitempty"`
	AllowedModels []string           `json:"allowed_models" binding:"required,min=1"`
	BlockedModels []string           `json:"blocked_models,omitempty"`
	Endpoint      string             `json:"endpoint,omitempty"`
	Aliases       map[string]string  `json:"aliases,omitempty"`
	Settings      CredentialSettings `json:"settings,omitzero"`
}

func (cred *Credential) CheckModel(model string) bool {
	if cred == nil || model == "" {
		return false
	}
	if slices.Contains(cred.BlockedModels, "*") || slices.Contains(cred.BlockedModels, model) {
		return false
	}
	return slices.Contains(cred.AllowedModels, "*") || slices.Contains(cred.AllowedModels, model)
}

func (cred *Credential) CheckValidity() bool {
	return cred != nil && cred.Enabled && (cred.ExpiryAt == nil || cred.ExpiryAt.After(time.Now()))
}

func (cred *Credential) CheckModelAlias(model string) string {
	if cred == nil {
		return model
	}
	if alias, ok := cred.Aliases[model]; ok {
		return alias
	}
	return model
}

func (cred *Credential) Validate() error {

	if cred.Provider == "" {
		return fmt.Errorf("provider is required")
	}

	if !IsKnownProvider(string(cred.Provider)) {
		return fmt.Errorf("provider %q is not supported", cred.Provider)
	}

	if cred.Endpoint == "" {
		return fmt.Errorf("credentials require an endpoint")
	}

	if len(cred.AllowedModels) == 0 {
		return fmt.Errorf("at least one allowed model is required")
	}

	for _, model := range cred.AllowedModels {
		if model == "" {
			return fmt.Errorf("allowed models contains an empty entry")
		}
	}

	// Validating indivudal settings here
	if !cred.Settings.IsEmpty() {
		return cred.Settings.Validate(cred.Provider)
	}

	return nil
}
