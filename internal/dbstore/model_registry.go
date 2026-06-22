package dbstore

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type StoreModelRegistry struct {
	ID                  string `gorm:"primaryKey;type:text"               json:"id"`
	ModelName           string `gorm:"not null;type:text;index:idx_model_name" json:"model_name"`
	BaseModelName       string `gorm:"not null;type:text"                                     json:"base_model"`
	ModelEndpoint       string `gorm:"not null;type:text"                                     json:"model_endpoint"`
	IsActive            bool   `gorm:"not null;default:true"                                  json:"is_active"`
	IsPricingEnabled    bool   `gorm:"not null;default:false"                                 json:"is_pricing_enabled"`
	EnableCustomPricing bool   `gorm:"not null;default:false"                                 json:"enable_custom_pricing"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ProviderID string        `gorm:"not null;type:text;index:idx_provider_id" json:"provider_id"`
	Provider   StoreProvider `gorm:"foreignKey:ProviderID;references:ID" json:"provider"`

	APIKeyID string              `gorm:"type:text" json:"api_key_id"`
	APIKey   StoreAPIKeyRegistry `gorm:"foreignKey:APIKeyID;references:ID" json:"api_key"`

	HealthCheck    bool   `gorm:"not null;default:false" json:"enable_health_check"`
	HealthEndpoint string `gorm:"null;type:text" json:"health_endpoint"`
}

func (StoreModelRegistry) TableName() string { return "model_registry" }

type StoreAPIKeyRegistry struct {
	ID                 string     `gorm:"primaryKey;type:text" json:"id"`
	Key                string     `gorm:"type:text;not null"   json:"key"`
	CreatedAt          time.Time  `json:"created_at"`
	ExpiryAt           *time.Time `json:"expiry_at"`
	EnableCustomHeader bool       `gorm:"not null;default:false"`
	CustomHeader       string     `gorm:"null;type:text"`
}

func (StoreAPIKeyRegistry) TableName() string { return "api_key_registry" }

func (s *StoreAPIKeyRegistry) BeforeCreate(tx *gorm.DB) error {
	encKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	var err error
	if s.Key, err = encryptKey(s.Key, encKey); err != nil {
		return fmt.Errorf("error while encrypting model API key: %w", err)
	}
	return nil
}

func (s *StoreAPIKeyRegistry) AfterFind(tx *gorm.DB) error {
	decKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	var err error
	if s.Key, err = decryptKey(s.Key, decKey); err != nil {
		return fmt.Errorf("error while decrypting model API key: %w", err)
	}
	return nil
}
