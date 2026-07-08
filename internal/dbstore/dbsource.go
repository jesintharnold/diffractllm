package dbstore

import (
	config "diffractllm/configs"
	"diffractllm/internal/core"
	"fmt"

	"go.uber.org/zap"
)

type DBSource struct {
	store  *Store
	logger *zap.Logger
	path   string
}

func NewDBSource(logger *zap.Logger) (*DBSource, error) {
	dbpath := config.GlobalConfig().ServerConfig.DBPath
	AesPassKey := config.GlobalConfig().ServerConfig.AesPasskey
	store, err := NewStore(dbpath, AesPassKey, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create new db store : %w", err)
	}
	return &DBSource{
		store:  store,
		logger: logger,
		path:   dbpath,
	}, nil
}

func (s *DBSource) Init() error {
	if s.store == nil {
		return fmt.Errorf("db source store is not initialized")
	}

	if err := s.store.Migrate(); err != nil {
		return fmt.Errorf("db source migrate: %w", err)
	}

	mockEnabled := config.GlobalConfig().ServerConfig.MockEnabled
	if err := s.store.Seed(mockEnabled); err != nil {
		return fmt.Errorf("db source seed: %w", err)
	}

	return nil
}

func (s *DBSource) Load() (*core.ModelPlaneSnapshot, error) {
	if s.store == nil {
		return nil, fmt.Errorf("db source store is not initialized")
	}

	registries, err := s.store.ListModelAPIRegistries()
	if err != nil {
		return nil, fmt.Errorf("db source list api registries: %w", err)
	}
	// catalog, err := s.store.ListCatalogModels(true)
	// if err != nil {
	// 	return nil, fmt.Errorf("db source list catalog models: %w", err)
	// }
	pools, err := s.store.ListModelPools()
	if err != nil {
		return nil, fmt.Errorf("db source list model pools: %w", err)
	}

	snap := &core.ModelPlaneSnapshot{
		APIRegistries: make([]core.ModelAPIRegistry, 0, len(registries)),
		// Catalog:       make([]core.ModelCatalog, 0, len(catalog)),
		Pools: make([]core.ModelPool, 0, len(pools)),
	}

	for i := range registries {
		k := &registries[i]
		snap.APIRegistries = append(snap.APIRegistries, core.ModelAPIRegistry{
			ID:                 k.ID,
			Provider:           core.Provider(k.Provider.Name),
			BaseURL:            k.BaseURL,
			APIkey:             deref(k.APIKey),
			EnableCustomHeader: k.EnableCustomHeader,
			CustomHeader:       k.CustomHeader,
			ExpiryAt:           k.ExpiryAt,
			AllowedModels:      k.AllowedModels,
		})
	}

	// for i := range catalog {
	// 	c := &catalog[i]
	// 	snap.Catalog = append(snap.Catalog, core.ModelCatalog{
	// 		ID:         c.ID,
	// 		ModelName:  c.ModelName,
	// 		Kind:       c.Kind,
	// 		IsActive:   c.IsActive,
	// 		CreatedAt:  c.CreatedAt,
	// 		UpdatedAt:  c.UpdatedAt,
	// 		ProviderID: c.ProviderID,
	// 		Provider:   core.Provider(c.Provider.Name),
	// 	})
	// }

	for i := range pools {
		p := &pools[i]
		snap.Pools = append(snap.Pools, core.ModelPool{
			ID:           p.ID,
			Name:         p.Name,
			LBType:       p.LBType,
			AllowedModel: p.AllowedModels,
			IsActive:     p.IsActive,
			CreatedAt:    p.CreatedAt,
			UpdatedAt:    p.UpdatedAt,
		})
	}

	return snap, nil
}

func (s *DBSource) GetStore() *Store { return s.store }
func (s *DBSource) Name() string     { return "sqlite" }
func (s *DBSource) Path() string     { return s.path }
func (s *DBSource) Close() error {
	if s.store == nil {
		return nil
	}

	if err := s.store.Close(); err != nil {
		return fmt.Errorf("db source close: %w", err)
	}

	s.store = nil
	return nil
}
