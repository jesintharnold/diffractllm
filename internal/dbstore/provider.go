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

	AdapterEnabled bool `gorm:"-" json:"adapter_enabled"`
	ModelCount     int  `gorm:"-" json:"model_count"`
}

func (StoreProvider) TableName() string { return "providers" }

func (s *StoreProvider) secrets() []*string {
	if s.Proxy == nil {
		return nil
	}
	return []*string{&s.Proxy.Username, &s.Proxy.Password}
}

func (s *StoreProvider) BeforeSave(tx *gorm.DB) error {
	encKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	for _, field := range s.secrets() {
		if *field == "" {
			continue
		}
		encrypted, err := encryptKey(field, encKey)
		if err != nil {
			return fmt.Errorf("error while encrypting proxy secret: %w", err)
		}
		*field = *encrypted
	}
	return nil
}

func (s *StoreProvider) AfterFind(tx *gorm.DB) error {
	if mask, _ := tx.Statement.Context.Value(maskSecrets{}).(bool); mask {
		for _, field := range s.secrets() {
			if *field == "" {
				continue
			}
			*field = SecretMask
		}
		return nil
	}

	decKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	for _, field := range s.secrets() {
		if *field == "" {
			continue
		}
		decrypted, err := decryptKey(field, decKey)
		if err != nil {
			return fmt.Errorf("error while decrypting proxy secret: %w", err)
		}
		*field = *decrypted
	}
	return nil
}

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

func (s *Store) ListProvidersRedacted() ([]StoreProvider, error) {
	var rows []StoreProvider
	if err := s.redacted().Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	return rows, nil
}

func (s *Store) GetProvider(name core.Provider) (*StoreProvider, error) {
	var row StoreProvider
	if err := s.DB.Where("name = ?", string(name)).First(&row).Error; err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", name, err)
	}
	return &row, nil
}

func (s *Store) GetProviderRedacted(name core.Provider) (*StoreProvider, error) {
	var row StoreProvider
	if err := s.redacted().Where("name = ?", string(name)).First(&row).Error; err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", name, err)
	}
	return &row, nil
}

func (s *Store) UpdateProviderConfig(provider core.Provider, network core.NetworkConfig, proxy *core.ProxyConfig) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		row, err := s.resolveProvider(tx, provider)
		if err != nil {
			return err
		}
		row.Network = network
		row.Proxy = proxy
		row.IsConfigured = true

		if err := tx.Model(&row).
			Select("network", "proxy", "is_configured").
			Updates(&row).Error; err != nil {
			return fmt.Errorf("update provider %q config: %w", provider, err)
		}
		return nil
	})
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
