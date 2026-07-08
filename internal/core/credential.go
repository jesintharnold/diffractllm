package core

import "time"

type ModelAPIRegistry struct {
	ID                 string     `json:"id,omitempty"`
	Provider           Provider   `json:"provider" binding:"required"`
	BaseURL            string     `json:"base_url" binding:"required"`
	APIkey             string     `json:"api_key,omitempty"`
	EnableCustomHeader bool       `json:"enable_custom_header"`
	CustomHeader       string     `json:"custom_header,omitempty"`
	ExpiryAt           *time.Time `json:"expires_at,omitempty"`
	AllowedModels      []string   `json:"allowed_models" binding:"required,min=1"`
}

type UpdateModelAPIRegistryRequest struct {
	BaseURL            *string    `json:"base_url,omitempty"`
	APIkey             *string    `json:"api_key,omitempty"`
	EnableCustomHeader *bool      `json:"enable_custom_header,omitempty"`
	CustomHeader       *string    `json:"custom_header,omitempty"`
	ExpiryAt           *time.Time `json:"expires_at,omitempty"`
	AllowedModels      []string   `json:"allowed_models,omitempty"`
}
