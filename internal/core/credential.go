package core

import (
	"fmt"
	"slices"
	"strings"
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

	if check > 1 {
		return fmt.Errorf("only one provider settings block may be set, got %d", check)
	}

	return nil
}

type Alias struct {
	ModelID          string                `json:"deploymentID,omitempty"`
	ModelName        string                `json:"model_name,omitempty"`
	EndpointProtocol EndpointProtocol      `json:"endpoint_protocol,omitempty"`
	RouteStyle       OpenAIAzureRouteStyle `json:"route_style,omitempty"`
	APIVersion       string                `json:"api_version,omitempty"`
}

var protocolNameHints = []struct {
	Protocol EndpointProtocol
	Match    string
}{
	{ProtocolAnthropic, "claude"},
	{ProtocolAnthropic, "anthropic."},
}

func (a *Alias) Protocol(requested string) EndpointProtocol {
	if a == nil {
		return ProtocolOpenAI
	}
	if a.EndpointProtocol != "" {
		return a.EndpointProtocol
	}
	for _, candidate := range []string{a.ModelName, a.ModelID, requested} {
		if candidate == "" {
			continue
		}
		lowered := strings.ToLower(candidate)
		for _, hint := range protocolNameHints {
			if strings.Contains(lowered, hint.Match) {
				return hint.Protocol
			}
		}
	}
	return ProtocolOpenAI
}

func (a *Alias) Validate() error {
	if a.ModelID == "" {
		return fmt.Errorf("deploymentID is required")
	}

	switch a.EndpointProtocol {
	case "", ProtocolOpenAI, ProtocolAnthropic:
	default:
		return fmt.Errorf("unknown endpoint_protocol %q", a.EndpointProtocol)
	}

	switch a.RouteStyle {
	case "", AzureRouteV1:
	case AzureRouteDeployment:
		if a.EndpointProtocol == ProtocolAnthropic {
			return fmt.Errorf("route_style applies to openai models only")
		}
		if a.APIVersion == "" {
			return fmt.Errorf("api-version is required for deployment route style")
		}
	default:
		return fmt.Errorf("unknown route_style %q", a.RouteStyle)
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
	Aliases       map[string]Alias   `json:"aliases,omitempty"`
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

func (cred *Credential) CheckModelAlias(model string) *Alias {
	if cred == nil {
		return &Alias{ModelID: model}
	}
	if alias, ok := cred.Aliases[model]; ok {
		return &alias
	}
	return &Alias{ModelID: model}
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

	for model, alias := range cred.Aliases {
		if err := alias.Validate(); err != nil {
			return fmt.Errorf("alias %q : %w", model, err)
		}
	}

	// Azure carries its auth mode here, so the block is not optional.
	if cred.Provider == ProviderAzure && cred.Settings.Azure == nil {
		return fmt.Errorf("azure credentials require a settings.azure block")
	}

	// Validating indivudal settings here
	if !cred.Settings.IsEmpty() {
		return cred.Settings.Validate(cred.Provider)
	}

	return nil
}

type EndpointProtocol Provider

const (
	ProtocolOpenAI    EndpointProtocol = EndpointProtocol(ProviderOpenAI)
	ProtocolAnthropic EndpointProtocol = EndpointProtocol(ProviderAnthropic)
)
