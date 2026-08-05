package modelcatalog

import (
	"context"
	config "diffractllm/configs"
	"diffractllm/internal/core"
	"fmt"
	"net/http"
)

// we will be sync the model to get the parsed from the litellm and bifrost here.

type SyncSource interface {
	Name() string
	Fetch(client *http.Client, ctx context.Context) (string, []core.ModelMetaData, []core.BasePricing, error)
}

type LiteLLMSource struct{}

func (lite *LiteLLMSource) Name() string {
	return "litellm"
}

func (lite *LiteLLMSource) Fetch(client *http.Client, ctx context.Context, cfg *config.ModelCatalogConfig) (string, []core.ModelMetaData, []core.BasePricing, error) {
	if cfg.SourceName != lite.Name() {
		return "", nil, nil, fmt.Errorf("Invalid sync source configured ")
	}

	if cfg.SourceURL == "" {
		return "", nil, nil, fmt.Errorf("Litellm sync source URL configured is incorrect or empty ")
	}

}
