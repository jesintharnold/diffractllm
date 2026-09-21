package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
)

func (s *Store) seedProviders() error {
	if err := s.UpsertProviders(s.DB, core.SupportedProviders()); err != nil {
		return fmt.Errorf("seed providers: %w", err)
	}
	return nil
}
