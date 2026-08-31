package core

import "fmt"

type AzureAuthMode string

const (
	AzureAuthKeyMode          AzureAuthMode = "azure_api_key"
	AzureAuthServicePrincipal AzureAuthMode = "azure_service_principal"
	AzureDefaultCredential    AzureAuthMode = "azure_default_credential"
)

const DefaultAzureScope = "https://cognitiveservices.azure.com/.default"

// Which Azure URL surface a deployment answers on. Set per alias, not per
// credential - one resource can host both.
type OpenAIAzureRouteStyle string

const (
	AzureRouteV1         OpenAIAzureRouteStyle = "v1"
	AzureRouteDeployment OpenAIAzureRouteStyle = "deployment"
)

type AzureSettings struct {
	AuthMode     AzureAuthMode `json:"auth_mode"`
	TenantID     string        `json:"tenant_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	ClientSecret string        `json:"client_secret,omitempty"`
	Scopes       []string      `json:"scopes,omitempty"`
}

func (azure *AzureSettings) Validate() error {

	switch azure.AuthMode {
	case AzureAuthKeyMode:
		if azure.TenantID != "" || azure.ClientID != "" || azure.ClientSecret != "" {
			return fmt.Errorf("azure provider : API key mode takes no tenant ,client-id or client-secret")
		}

	case AzureAuthServicePrincipal:
		if azure.TenantID == "" || azure.ClientID == "" || azure.ClientSecret == "" {
			return fmt.Errorf("azure provider : service principal requires tenant-id, client-id and client-secret")
		}

	case AzureDefaultCredential:
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
