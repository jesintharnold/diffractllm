package providers

import "diffractllm/internal/core"

func SanitizeProviderEndpoint(raw string) string {
	return core.SanitizeBackendURL(raw)
}
