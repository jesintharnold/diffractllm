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

func (s *DBSource) Load() ([]*core.Deployment, error) {
	if s.store == nil {
		return nil, fmt.Errorf("db source store is not initialized")
	}

	if err := s.store.Migrate(); err != nil {
		return nil, fmt.Errorf("db source migrate: %w", err)
	}

	mockEnabled := config.GlobalConfig().ServerConfig.MockEnabled
	if err := s.store.Seed(mockEnabled); err != nil {
		return nil, fmt.Errorf("db source seed: %w", err)
	}

	return s.HydrateRoutingTableConfig()
}

func (s *DBSource) HydrateRoutingTableConfig() (*core.RoutingTableConfig, error) {
	if s.store == nil {
		return nil, fmt.Errorf("db source store is not initialized")
	}

	models, err := s.store.ListModels(true)
	if err != nil {
		return nil, fmt.Errorf("db source list models: %w", err)
	}

	backends, err := s.store.ListBackends(true)
	if err != nil {
		return nil, fmt.Errorf("db source list backends: %w", err)
	}

	policies, err := s.store.ListPolicies(true)
	if err != nil {
		return nil, fmt.Errorf("db source list policies: %w", err)
	}

	config := &core.RoutingTableConfig{
		ModelRegistry: make([]*core.Model, 0, len(models)),
		Backends:      make([]core.BackendConfig, 0, len(backends)),
		Policies:      make([]core.RoutePolicy, 0, len(policies)),
	}

	for i := range models {
		config.ModelRegistry = append(config.ModelRegistry, models[i].ToCore())
	}

	for i := range backends {
		config.Backends = append(config.Backends, *backends[i].ToCore())
	}

	for i := range policies {
		config.Policies = append(config.Policies, *policies[i].ToCore())
	}

	return config, nil
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
