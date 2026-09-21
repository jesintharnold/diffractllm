package dbstore

import (
	config "diffractllm/configs"
	"diffractllm/internal/core"
	"fmt"

	"go.uber.org/zap"
)

type DBSource struct {
	store       *Store
	logger      *zap.Logger
	path        string
	mockEnabled bool
}

func NewDBSource(logger *zap.Logger) (*DBSource, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("db source config: %w", err)
	}

	dbpath := cfg.ServerConfig.DBPath
	store, err := NewStore(dbpath, cfg.ServerConfig.AesPasskey, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create new db store : %w", err)
	}
	return &DBSource{
		store:       store,
		logger:      logger,
		path:        dbpath,
		mockEnabled: cfg.ServerConfig.MockEnabled,
	}, nil
}

func (s *DBSource) Init() error {
	if s.store == nil {
		return fmt.Errorf("db source store is not initialized")
	}

	if err := s.store.Migrate(); err != nil {
		return fmt.Errorf("db source migrate: %w", err)
	}
	if err := s.store.Seed(s.mockEnabled); err != nil {
		return fmt.Errorf("db source seed: %w", err)
	}
	return nil
}

func (s *DBSource) Load() ([]*core.Upstream, []*core.Credential, error) {
	if s.store == nil {
		return nil, nil, fmt.Errorf("db source store is not initialized")
	}

	providers, err := s.store.ListProviders()
	if err != nil {
		return nil, nil, fmt.Errorf("db source list providers: %w", err)
	}
	credentials, err := s.store.ListCredentials()
	if err != nil {
		return nil, nil, fmt.Errorf("db source list credentials: %w", err)
	}

	providerconfigs := make([]*core.Upstream, 0, len(providers))
	for i := range providers {
		providerconfigs = append(providerconfigs, providers[i].ToUpstream())
	}

	creds := make([]*core.Credential, 0, len(credentials))
	for i := range credentials {
		creds = append(creds, credentials[i].ToCore())
	}

	return providerconfigs, creds, nil
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
