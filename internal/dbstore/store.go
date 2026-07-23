package dbstore

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Store struct {
	DB     *gorm.DB
	logger *zap.Logger
}

var (
	globalStore *Store
	globalErr   error
	storeOnce   sync.Once
)

type aesKeyPass struct{}

func NewStore(dbPath string, aespasskey string, logger *zap.Logger) (*Store, error) {
	storeOnce.Do(func() {
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			file, err := os.Create(dbPath)
			if err != nil {
				globalErr = fmt.Errorf("failed to create sqlite db file at %q: %w", dbPath, err)
				return
			}
			file.Close()
		}

		gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		if err != nil {
			globalErr = fmt.Errorf("failed to open sqlite db at %q: %w", dbPath, err)
			return
		}

		sqlDB, err := gormDB.DB()
		if err != nil {
			globalErr = fmt.Errorf("failed to get underlying sql.DB: %w", err)
			return
		}

		pragmas := []string{
			"PRAGMA journal_mode = WAL",
			"PRAGMA foreign_keys = ON",
			"PRAGMA busy_timeout = 5000",
			"PRAGMA synchronous = NORMAL",
			"PRAGMA cache_size = -64000",
			"PRAGMA temp_store = MEMORY",
		}
		for _, p := range pragmas {
			if _, err := sqlDB.Exec(p); err != nil {
				globalErr = fmt.Errorf("failed to set pragma %q: %w", p, err)
				return
			}
		}

		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		sqlDB.SetConnMaxLifetime(time.Hour)

		logger.Info("sqlite store opened", zap.String("path", dbPath))

		ctx := context.WithValue(context.Background(), aesKeyPass{}, []byte(aespasskey))
		globalStore = &Store{
			DB:     gormDB.WithContext(ctx),
			logger: logger,
		}
	})

	return globalStore, globalErr
}

func (s *Store) Migrate() error {

	tables := []any{
		&StoreBudget{},
		&StoreModelAPIRegistry{},
		&StoreModelCatalog{},
		&StoreBaseModelPricing{},
		&StoreCustomModelPricing{},
		&StoreProvider{},
		&StoreUsageRecord{},
		&StoreVirtualKey{},
	}

	return s.DB.AutoMigrate(tables...)
}

func (s *Store) Seed(mockEnabled bool) error {
	if err := s.seedProviders(); err != nil {
		return err
	}
	// if mockEnabled {
	// 	if err := s.seedMockData(); err != nil {
	// 		return err
	// 	}
	// }
	return nil
}

func (s *Store) Close() error {
	sqlDB, err := s.DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return sqlDB.Close()
}
