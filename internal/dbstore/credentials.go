package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StoreModelAPIRegistry struct {
	ID                 string        `gorm:"primaryKey;type:text" json:"id"`
	ProviderID         string        `gorm:"not null;type:text;index"`
	Provider           StoreProvider `gorm:"foreignKey:ProviderID;references:ID"`
	BaseURL            string        `gorm:"not null;type:text"`
	APIKey             *string       `gorm:"type:text"   json:"apikey"`
	CreatedAt          time.Time     `json:"created_at"`
	ExpiryAt           *time.Time    `json:"expiry_at"`
	EnableCustomHeader bool          `gorm:"not null;default:false"`
	CustomHeader       string        `gorm:"null;type:text"`
	UpdatedAt          time.Time     `json:"updated_at"`
	AllowedModels      []string      `gorm:"type:json;serializer:json" json:"allowed_models"`
}

func (StoreModelAPIRegistry) TableName() string { return "model_api_registry" }

func (s *StoreModelAPIRegistry) BeforeSave(tx *gorm.DB) error {
	if s.APIKey == nil {
		return nil
	}
	encKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	var err error
	if s.APIKey, err = encryptKey(s.APIKey, encKey); err != nil {
		return fmt.Errorf("error while encrypting model API key: %w", err)
	}
	return nil
}

func (s *StoreModelAPIRegistry) AfterFind(tx *gorm.DB) error {
	if s.APIKey == nil {
		return nil
	}
	decKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	var err error
	if s.APIKey, err = decryptKey(s.APIKey, decKey); err != nil {
		return fmt.Errorf("error while decrypting model API key: %w", err)
	}
	return nil
}

// -------- Model API Registry Store methods ---------

func (s *Store) CreateModelAPIRegistry(m *core.ModelAPIRegistry) (*StoreModelAPIRegistry, error) {
	var out StoreModelAPIRegistry
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		prov, err := s.resolveProvider(tx, m.Provider)
		if err != nil {
			return err
		}
		out = StoreModelAPIRegistry{
			ID:                 uuid.Must(uuid.NewV7()).String(),
			ProviderID:         prov.ID,
			BaseURL:            m.BaseURL,
			APIKey:             optKey(m.APIkey),
			EnableCustomHeader: m.EnableCustomHeader,
			CustomHeader:       m.CustomHeader,
			ExpiryAt:           m.ExpiryAt,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
			AllowedModels:      m.AllowedModels,
		}

		return tx.Select("ID", "ProviderID", "BaseURL", "APIKey", "EnableCustomHeader",
			"CustomHeader", "ExpiryAt", "CreatedAt", "UpdatedAt", "AllowedModels").Create(&out).Error
	})

	if err != nil {
		return nil, fmt.Errorf("create connection failed: %w", err)
	}
	return s.GetModelAPIRegistry(out.ID)
}

func (s *Store) GetModelAPIRegistry(id string) (*StoreModelAPIRegistry, error) {
	var row StoreModelAPIRegistry
	if err := s.DB.Preload("Provider").Where("id = ?", id).First(&row).Error; err != nil {
		return nil, fmt.Errorf("connection %q not found: %w", id, err) // AfterFind decrypts key
	}
	return &row, nil
}

func (s *Store) ListModelAPIRegistries() ([]StoreModelAPIRegistry, error) {
	var rows []StoreModelAPIRegistry
	if err := s.DB.Preload("Provider").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list model api registries: %w", err)
	}
	return rows, nil
}

func (s *Store) UpdateModelAPIRegistry(id string, m *core.UpdateModelAPIRegistryRequest) (*StoreModelAPIRegistry, error) {
	updatecols := []string{"UpdatedAt"}
	payload := StoreModelAPIRegistry{UpdatedAt: time.Now()}

	if m.APIkey != nil {
		payload.APIKey = m.APIkey
		updatecols = append(updatecols, "APIKey")
	}

	if m.BaseURL != nil {
		payload.BaseURL = *m.BaseURL
		updatecols = append(updatecols, "BaseURL")
	}
	if m.CustomHeader != nil {
		payload.CustomHeader = *m.CustomHeader
		updatecols = append(updatecols, "CustomHeader")
	}
	if m.EnableCustomHeader != nil {
		payload.EnableCustomHeader = *m.EnableCustomHeader
		updatecols = append(updatecols, "EnableCustomHeader")
	}
	if m.ExpiryAt != nil {
		payload.ExpiryAt = m.ExpiryAt
		updatecols = append(updatecols, "ExpiryAt")
	}

	if m.AllowedModels != nil {
		payload.AllowedModels = m.AllowedModels
		updatecols = append(updatecols, "AllowedModels")
	}

	if len(updatecols) == 1 {
		return s.GetModelAPIRegistry(id)
	}

	res := s.DB.Model(&StoreModelAPIRegistry{}).Where("id = ?", id).Select(updatecols).Updates(payload)

	if res.Error != nil {
		return nil, fmt.Errorf("update model api registry %q failed: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("model api registry %q not found", id)
	}
	return s.GetModelAPIRegistry(id)
}

func (s *Store) DeleteModelAPIRegistry(id string) error {
	res := s.DB.Where("id = ?", id).Delete(&StoreModelAPIRegistry{})
	if res.Error != nil {
		return fmt.Errorf("delete model api registry %q failed: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("model api registry %q not found", id)
	}
	return nil

}
