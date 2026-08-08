package dbstore

import (
	"diffractllm/internal/core"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StoreProvider struct {
	ID           string `gorm:"primaryKey;type:text"                           json:"id"`
	Name         string `gorm:"not null;type:text;uniqueIndex:uq_provider_name" json:"name"`
	IsConfigured bool   `gorm:"not null;default:false"                         json:"is_configured"`
}

func (StoreProvider) TableName() string { return "providers" }

func (s *Store) resolveProvider(tx *gorm.DB, provider core.Provider) (StoreProvider, error) {
	var rowProvider StoreProvider
	if err := tx.Where("name = ?", string(provider)).First(&rowProvider).Error; err != nil {
		return rowProvider, fmt.Errorf("provider %q not found: %w", provider, err)
	}
	return rowProvider, nil
}

func (s *Store) UpsertProviders(tx *gorm.DB, names []core.Provider) error {
	seen := make(map[core.Provider]struct{}, len(names))
	rows := make([]StoreProvider, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		rows = append(rows, StoreProvider{
			ID:   uuid.Must(uuid.NewV7()).String(),
			Name: string(name),
		})
	}
	if len(rows) == 0 {
		return nil
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("upsert providers: %w", err)
	}
	return nil
}

func (s *Store) ProviderIDs(tx *gorm.DB) (map[core.Provider]string, error) {
	if tx == nil {
		tx = s.DB
	}
	var providers []StoreProvider
	if err := tx.Find(&providers).Error; err != nil {
		return nil, fmt.Errorf("load providers: %w", err)
	}
	ids := make(map[core.Provider]string, len(providers))
	for _, provider := range providers {
		ids[core.Provider(provider.Name)] = provider.ID
	}
	return ids, nil
}
