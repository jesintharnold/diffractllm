package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StoreModelPool struct {
	ID            string              `gorm:"primaryKey;type:text" json:"id"`
	Name          string              `gorm:"not null;type:text;uniqueIndex:idx_pool_name" json:"name"`
	LBType        core.LBkind         `gorm:"not null;default:0" json:"lb_type"`
	AllowedModels []core.AllowedModel `gorm:"type:json;serializer:json" json:"allowed_models"`
	IsActive      bool                `gorm:"not null;default:true" json:"is_active"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

func (StoreModelPool) TableName() string { return "model_pool" }

func (s *Store) DeleteModelPool(id string) error {
	res := s.DB.Where("id = ?", id).Delete(&StoreModelPool{})
	if res.Error != nil {
		return fmt.Errorf("delete model pool %q failed: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("model pool %q not found", id)
	}
	return nil
}

func (s *Store) GetModelPool(id string) (*StoreModelPool, error) {
	var pool StoreModelPool
	if err := s.DB.Where("id = ?", id).First(&pool).Error; err != nil {
		return nil, fmt.Errorf("model pool %q not found: %w", id, err)
	}
	return &pool, nil
}

func (s *Store) ListModelPools() ([]StoreModelPool, error) {
	var pools []StoreModelPool
	if err := s.DB.Find(&pools).Error; err != nil {
		return nil, fmt.Errorf("failed to list model pools: %w", err)
	}
	return pools, nil
}

func (s *Store) UpdateModelPool(id string, pool core.ModelPool) (*StoreModelPool, error) {
	var modelpool StoreModelPool
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&StoreModelPool{}).Where("id = ?", id).
			Select("Name", "LBType", "AllowedModels", "IsActive", "UpdatedAt").
			Updates(StoreModelPool{
				Name:          pool.Name,
				LBType:        pool.LBType,
				AllowedModels: pool.AllowedModel,
				IsActive:      pool.IsActive,
				UpdatedAt:     time.Now(),
			})

		if res.Error != nil {
			return fmt.Errorf("update model pool failed %q: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("model pool %q not found", id)
		}

		if err := tx.Where("id = ?", id).First(&modelpool).Error; err != nil {
			return fmt.Errorf("failed to get the updated model pools with id %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &modelpool, nil
}
func (s *Store) CreateModelPool(pool core.ModelPool) (*StoreModelPool, error) {

	now := time.Now()
	payload := StoreModelPool{
		ID:            uuid.Must(uuid.NewV7()).String(),
		Name:          pool.Name,
		LBType:        pool.LBType,
		AllowedModels: pool.AllowedModel,
		IsActive:      pool.IsActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.DB.Select("ID", "Name", "LBType", "AllowedModels", "IsActive", "CreatedAt", "UpdatedAt").
		Create(&payload).Error; err != nil {
		return nil, fmt.Errorf("create model pools with name %s failed: %w", payload.Name, err)
	}

	var modelpool StoreModelPool
	if err := s.DB.Where("id = ?", payload.ID).First(&modelpool).Error; err != nil {
		return nil, fmt.Errorf("failed to get the model pools with id %w", err)
	}

	return &modelpool, nil
}
