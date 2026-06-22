package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StoreVirtualKey struct {
	ID                string              `gorm:"primaryKey;type:text"`
	KeyHash           string              `gorm:"uniqueIndex;not null;type:text"`
	APIKey            string              `gorm:"not null;type:text"`
	DisplayPrefix     string              `gorm:"not null;type:text"`
	ClientID          string              `gorm:"not null;type:text"`
	BudgetID          string              `gorm:"not null;type:text"`
	Mode              string              `gorm:"not null;type:text;default:'allowed_model'"`
	AllowedModels     []core.AllowedModel `gorm:"type:json;serializer:json" json:"allowed_models"`
	AllowedModelPools []string            `gorm:"type:json;serializer:json" json:"allowed_pools"`
	IsActive          bool                `gorm:"not null;default:true"`
	CreatedAt         time.Time
	UpdatedAt         time.Time  `json:"created_at"`
	ExpiresAt         *time.Time `json:"expires_at"`
}

func (StoreVirtualKey) TableName() string { return "virtual_keys" }

func (s *Store) resolveModels(models []string) ([]core.AllowedModel, error) {
	if len(models) == 0 {
		return nil, nil
	}
	out := make([]core.AllowedModel, 0, len(models))
	for _, entry := range models {
		if entry == "" {
			continue
		}

		parts := strings.SplitN(entry, "/", 2)
		if len(parts) == 1 {
			out = append(out, core.AllowedModel{
				Provider: "",
				Model:    parts[0],
			})
		} else {
			if parts[0] == "" || parts[1] == "" {
				return nil, fmt.Errorf("invalid model format %q: cannot have empty provider or model", entry)
			}

			out = append(out, core.AllowedModel{
				Provider: parts[0],
				Model:    parts[1],
			})
		}
	}
	return out, nil
}

func (k *StoreVirtualKey) ToModelKeySet() map[core.ModelKey]struct{} {
	if len(k.AllowedModels) == 0 {
		return nil
	}
	s := make(map[core.ModelKey]struct{}, len(k.AllowedModels))
	for _, m := range k.AllowedModels {
		s[core.ModelKey{Provider: core.Provider(m.Provider), ModelName: m.Model}] = struct{}{}
	}
	return s
}

func (k *StoreVirtualKey) ToPoolNameSet() map[string]struct{} {
	if len(k.AllowedModelPools) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(k.AllowedModelPools))
	for _, p := range k.AllowedModelPools {
		s[p] = struct{}{}
	}
	return s
}

func (k *StoreVirtualKey) ToCore() core.VirtualKeyResponse {
	resp := core.VirtualKeyResponse{
		ID:            k.ID,
		DisplayPrefix: k.DisplayPrefix,
		ClientID:      k.ClientID,
		BudgetID:      k.BudgetID,
		IsActive:      k.IsActive,
		CreatedAt:     k.CreatedAt,
		UpdatedAt:     k.UpdatedAt,
		Mode:          core.ParseVKMode(k.Mode),
		AllowedModels: k.AllowedModels,
		AllowedPools:  k.AllowedModelPools,
	}

	if k.ExpiresAt != nil {
		resp.ExpiresAt = *k.ExpiresAt
	}

	return resp
}

func (s *StoreVirtualKey) BeforeCreate(tx *gorm.DB) error {
	encKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	var err error
	if s.APIKey, err = encryptKey(s.APIKey, encKey); err != nil {
		return fmt.Errorf("error while encrypting APIKey: %w", err)
	}
	return nil
}

func (s *StoreVirtualKey) AfterFind(tx *gorm.DB) error {
	decKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	var err error
	if s.APIKey, err = decryptKey(s.APIKey, decKey); err != nil {
		return fmt.Errorf("error while decrypting APIKey: %w", err)
	}
	return nil
}

func (s *Store) ListActiveVirtualKeys() ([]StoreVirtualKey, error) {
	var keys []StoreVirtualKey
	if err := s.DB.Preload("AllowedModels").Preload("AllowedModels.Provider").Preload("Pools").Where("is_active = ?", true).Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("failed to list active virtual keys: %w", err)
	}
	return keys, nil
}

func (s *Store) GetVirtualKey(VID string) (*StoreVirtualKey, error) {
	var key StoreVirtualKey
	if err := s.DB.Preload("AllowedModels").Preload("AllowedModels.Provider").Preload("Pools").Where("id = ?", VID).First(&key).Error; err != nil {
		return nil, fmt.Errorf("virtual key %q not found: %w", VID, err)
	}
	return &key, nil
}

func (s *Store) ListVirtualKeys() ([]StoreVirtualKey, error) {
	var keys []StoreVirtualKey
	if err := s.DB.Preload("AllowedModels").Preload("AllowedModels.Provider").Preload("Pools").Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("failed to list virtual keys: %w", err)
	}
	return keys, nil
}

func (s *Store) RevokeVirtualKey(VID string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var key StoreVirtualKey
		if err := tx.Where("id = ?", VID).First(&key).Error; err != nil {
			return fmt.Errorf("virtual key %q not found: %w", VID, err)
		}
		if err := tx.Model(&key).Update("is_active", false).Error; err != nil {
			return fmt.Errorf("failed to revoke key: %w", err)
		}
		return tx.Model(&StoreBudget{}).Where("id = ?", key.BudgetID).Update("status", "released").Error
	})
}

func (s *Store) UpdateVirtualKeyModels(keyID string, mode *core.VKMode, models []string) (*StoreVirtualKey, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var key StoreVirtualKey
		if err := tx.Where("id = ?", keyID).First(&key).Error; err != nil {
			return err
		}

		effectiveMode := key.Mode
		if mode != nil {
			effectiveMode = mode.String()
			key.Mode = effectiveMode
		}

		if models != nil {
			if effectiveMode == core.VKModelPool.String() {
				key.AllowedModelPools = models
				key.AllowedModels = nil
			} else {
				allowed_models, err := s.resolveModels(models)
				if err != nil {
					return err
				}
				key.AllowedModels = allowed_models
				key.AllowedModelPools = nil
			}
		}

		if err := tx.Save(&key).Error; err != nil {
			return fmt.Errorf("failed to update key routing: %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return s.GetVirtualKey(keyID)
}

type CreateVirtualKeyResult struct {
	VirtualKey *StoreVirtualKey
	Budget     *StoreBudget
}

func (s *Store) CreateVirtualKeyTx(payload *core.VirtualKey, apikey, hash, prefix string) (*CreateVirtualKeyResult, error) {
	var result CreateVirtualKeyResult
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var budget StoreBudget
		if err := tx.Where("id = ?", payload.BudgetID).First(&budget).Error; err != nil {
			return fmt.Errorf("budget %q not found: %w", payload.BudgetID, err)
		}

		if budget.Status == "bound" {
			return fmt.Errorf("budget %q is already bound to an active key", payload.BudgetID)
		}

		if err := tx.Model(&budget).Update("status", "bound").Error; err != nil {
			return fmt.Errorf("failed to bind budget: %w", err)
		}
		result.Budget = &budget

		vkey := StoreVirtualKey{
			ID:            uuid.Must(uuid.NewV7()).String(),
			KeyHash:       hash,
			APIKey:        apikey,
			DisplayPrefix: prefix,
			ClientID:      payload.ClientID,
			BudgetID:      budget.ID,
			Mode:          payload.Mode.String(),
			IsActive:      true,
			ExpiresAt:     payload.ExpiresAt,
		}

		if payload.Mode == core.VKAllowedModel {
			vkey.AllowedModels = payload.AllowedModels
		} else {
			vkey.AllowedModelPools = payload.AllowedPools
		}

		if err := tx.Create(&vkey).Error; err != nil {
			return fmt.Errorf("insert virtual key: %w", err)
		}

		result.VirtualKey = &vkey
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Store) DeleteVirtualKey(VID string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var key StoreVirtualKey
		if err := tx.Where("id = ?", VID).First(&key).Error; err != nil {
			return fmt.Errorf("virtual key %q not found: %w", VID, err)
		}

		if err := tx.Delete(&key).Error; err != nil {
			return fmt.Errorf("failed to delete virtual key: %w", err)
		}

		return tx.Model(&StoreBudget{}).Where("id = ?", key.BudgetID).Update("status", "released").Error
	})
}
