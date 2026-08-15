package core

import "fmt"

type AzureAuthMode string

const (
	AzureAuthKeyMode          AzureAuthMode = "azure_api_key"
	AzureAuthServicePrincipal AzureAuthMode = "azure_service_principal"
	AzureManagedIdentity      AzureAuthMode = "azure_managed_identity"
)

const DefaultAzureScope = "https://cognitiveservices.azure.com/.default"

type AzureSettings struct {
	AuthMode     AzureAuthMode `json:"auth_mode"`
	APIVersion   string        `json:"api_version,omitempty"`
	TenantID     string        `json:"tenant_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	ClientSecret string        `json:"client_secret,omitempty"`
	Scopes       []string      `json:"scopes,omitempty"`
}

func (azure *AzureSettings) Validate() error {
	if azure.APIVersion == "" {
		return fmt.Errorf("azure provider : api-version is required")
	}

	switch azure.AuthMode {
	case AzureAuthKeyMode:
		if azure.TenantID != "" || azure.ClientID != "" || azure.ClientSecret != "" {
			return fmt.Errorf("azure provider : API key mode takes no tenant ,client-id or client-secret")
		}

	case AzureAuthServicePrincipal:
		if azure.TenantID == "" || azure.ClientID == "" || azure.ClientSecret == "" {
			return fmt.Errorf("azure provider : service principal requires tenant-id, client-id and client-secret")
		}

	case AzureManagedIdentity:
		if azure.ClientSecret != "" {
			return fmt.Errorf("azure provider : managed_identity takes no client_secret")
		}
		if azure.TenantID != "" {
			return fmt.Errorf("azure provider : managed_identity takes no tenant_id")
		}

	default:
		return fmt.Errorf("azure provider : unknown auth_mode %q", azure.AuthMode)
	}
	return nil
}
