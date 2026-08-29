package azureprovider

import (
	"diffractllm/internal/core"
	openaiprovider "diffractllm/internal/providers/openai"
	"fmt"
	neturl "net/url"
	"strings"
)

func (ap *AzureProvider) openaichatConfig(req *core.DiffractLLMChatCompletionRequest, cred *core.Credential, alias *core.Alias, stream bool) (*openaiprovider.ChatCompletionConfig, *core.DiffractLLMError) {
	var requestURL string

	//https://{your-resource-name}.openai.azure.com/openai/deployments/{your-deployment-name}/chat/completions?api-version={api-version}

	headers := openaiprovider.ProviderHeaders(stream)
	path, ok := openaiprovider.PathFor(core.ChatRequest)
	if !ok {
		return nil, core.NewInternalError("azure-provider", fmt.Sprintf("unsupported request kind %s", core.ChatRequest), nil)
	}

	endpoint := strings.TrimRight(cred.Endpoint, "/")
	if alias.RouteStyle == core.AzureRouteDeployment {
		requestURL = fmt.Sprintf("%s/openai/deployments/%s%s?api-version=%s", endpoint, neturl.PathEscape(alias.ModelID), strings.TrimPrefix(path, "/v1"), neturl.QueryEscape(alias.APIVersion))
	} else {
		requestURL = fmt.Sprintf("%s/openai%s", endpoint, path)
	}

	return &openaiprovider.ChatCompletionConfig{
		Provider: core.ProviderAzure,
		URL:      requestURL,
		Model:    alias.ModelID,
		Request:  req,
		Headers:  headers,
	}, nil
}
