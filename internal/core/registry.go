package core

import (
	"time"
)

// Model Plane Snapshot
type ModelPlaneSnapshot struct {
	APIRegistries []ModelAPIRegistry
}

type ModelCatalog struct {
	ID         string    `json:"id,omitempty"`
	ModelName  string    `json:"model_name"`
	Kind       string    `json:"kind"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	ProviderID string    `json:"provider_id"`
	Provider   Provider  `json:"provider"`
}

type ModelCatalogRequest struct {
	ModelName string `json:"model_name"`
	Kind      string `json:"kind"`
	Provider  string `json:"provider"`
}

type Deployment struct {
	ID            string           `json:"id,omitempty"`
	ModelProvider Provider         `json:"provider" binding:"required"`
	ModelName     string           `json:"model_name"     binding:"required"`
	APIRegistryID string           `json:"api_registry_id,omitempty"`
	State         *DeploymentState `json:"-"`
	CreatedAt     time.Time        `json:"created_at,omitempty"`
	UpdatedAt     time.Time        `json:"updated_at,omitempty"`
}

func (d *Deployment) Key() ModelKey {
	return ModelKey{Provider: d.ModelProvider, ModelName: d.ModelName}
}

type ModelAPIRegistryResponse struct {
	ID                 string     `json:"id"`
	Provider           Provider   `json:"provider"`
	BaseURL            string     `json:"base_url"`
	DisplayKey         string     `json:"display_key,omitempty"`
	HasKey             bool       `json:"has_key"`
	EnableCustomHeader bool       `json:"enable_custom_header"`
	CustomHeader       string     `json:"custom_header,omitempty"`
	ExpiryAt           *time.Time `json:"expiry_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}
