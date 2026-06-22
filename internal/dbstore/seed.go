package dbstore

import (
	"fmt"

	"github.com/google/uuid"
)

func (s *Store) seedProviders() error {
	providers := []string{"openai", "anthropic", "azure-openai"}
	for _, provider := range providers {
		p := &StoreProvider{
			ID:   uuid.Must(uuid.NewV7()).String(),
			Name: provider,
		}
		if err := s.DB.Where("name = ?", provider).FirstOrCreate(p).Error; err != nil {
			return fmt.Errorf("failed to seed provider %q: %w", provider, err)
		}
	}
	return nil
}


