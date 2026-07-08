package dbstore

import (
	"diffractllm/internal/core"
	"fmt"

	"gorm.io/gorm"
)

type StoreProvider struct {
	ID   string `gorm:"primaryKey;type:text" json:"id"`
	Name string `gorm:"not null;type:text" json:"name"`
}

func (StoreProvider) TableName() string { return "providers" }

func (s *Store) resolveProvider(tx *gorm.DB, provider core.Provider) (StoreProvider, error) {
	var rowProvider StoreProvider
	if err := tx.Where("name = ?", string(provider)).First(&rowProvider).Error; err != nil {
		return rowProvider, fmt.Errorf("provider %q not found: %w", provider, err)
	}
	return rowProvider, nil
}
