package dbstore

import (
	"diffractllm/internal/core"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StoreProvider struct {
	ID           string             `gorm:"primaryKey;type:text"                           json:"id"`
	Name         string             `gorm:"not null;type:text;uniqueIndex:uq_provider_name" json:"name"`
	IsConfigured bool               `gorm:"not null;default:false"                         json:"is_configured"`
	Network      core.NetworkConfig `gorm:"serializer:json;type:text" json:"network_config"`
	Proxy        *core.ProxyConfig  `gorm:"serializer:json;type:text" json:"proxy_config,omitempty"`
}

func (StoreProvider) TableName() string { return "providers" }

func (s *StoreProvider) ToUpstream() *core.Upstream {
	return &core.Upstream{
		Provider: core.Provider(s.Name),
		Network:  s.Network,
		Proxy:    s.Proxy,
	}
}

func (s *Store) ListProviders() ([]StoreProvider, error) {
	var rows []StoreProvider
	if err := s.DB.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	return rows, nil
}

func (s *Store) UpdateProviderConfig(provider core.Provider, network core.NetworkConfig, proxy *core.ProxyConfig) error {
	res := s.DB.Model(&StoreProvider{}).
		Where("name = ?", string(provider)).
		Select("network", "proxy", "is_configured").
		Updates(&StoreProvider{
			Network:      network,
			Proxy:        proxy,
			IsConfigured: true,
		})
	if res.Error != nil {
		return fmt.Errorf("update provider %q config: %w", provider, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("provider %q not found", provider)
	}
	return nil
}

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
