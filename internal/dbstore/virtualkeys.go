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
	ID              string                `gorm:"primaryKey;type:text"`
	KeyHash         string                `gorm:"uniqueIndex;not null;type:text"`
	APIKey          string                `gorm:"not null;type:text"`
	DisplayPrefix   string                `gorm:"not null;type:text"`
	ClientID        string                `gorm:"not null;type:text"`
	BudgetID        string                `gorm:"not null;type:text"`
	Mode            string                `gorm:"not null;type:text;default:'direct'"`
	ProviderConfigs []core.ProviderConfig `gorm:"type:json;serializer:json" json:"provider_configs"`
	CustomPoolName  string                `gorm:"not null;type:text;default:''" json:"custom_pool_name"`
	LoadBalancer    string                `gorm:"not null;type:text;default:'round_robin'" json:"load_balancer"`
	IsActive        bool                  `gorm:"not null;default:true"`
	CreatedAt       time.Time
	UpdatedAt       time.Time  `json:"created_at"`
	ExpiresAt       *time.Time `json:"expires_at"`
}

func (StoreVirtualKey) TableName() string { return "virtual_keys" }

func (key *StoreVirtualKey) ToCore() (*core.VirtualKey, error) {
	mode, err := core.ParseVKMode(key.Mode)
	if err != nil {
		return nil, err
	}
	loadBalancer, err := core.ParseLBKind(key.LoadBalancer)
	if err != nil {
		return nil, err
	}
	providerConfigs, err := core.CompileProviderConfigs(mode, key.ProviderConfigs)
	if err != nil {
		return nil, err
	}

	return &core.VirtualKey{
		ID:              key.ID,
		Key:             key.APIKey,
		ClientID:        key.ClientID,
		BudgetID:        key.BudgetID,
		IsActive:        key.IsActive,
		ExpiresAt:       key.ExpiresAt,
		Mode:            mode,
		CustomPoolName:  key.CustomPoolName,
		LoadBalancer:    loadBalancer,
		ProviderConfigs: providerConfigs,
	}, nil
}

func (key *StoreVirtualKey) ToResponse() (core.VirtualKeyResponse, error) {
	mode, err := core.ParseVKMode(key.Mode)
	if err != nil {
		return core.VirtualKeyResponse{}, err
	}
	loadBalancer, err := core.ParseLBKind(key.LoadBalancer)
	if err != nil {
		return core.VirtualKeyResponse{}, err
	}
	response := core.VirtualKeyResponse{
		ID:              key.ID,
		DisplayPrefix:   key.DisplayPrefix,
		ClientID:        key.ClientID,
		BudgetID:        key.BudgetID,
		Mode:            mode,
		CustomPoolName:  key.CustomPoolName,
		LoadBalancer:    loadBalancer,
		ProviderConfigs: key.ProviderConfigs,
		IsActive:        key.IsActive,
		CreatedAt:       key.CreatedAt,
		UpdatedAt:       key.UpdatedAt,
	}
	if key.ExpiresAt != nil {
		response.ExpiresAt = *key.ExpiresAt
	}
	return response, nil
}

func (key *StoreVirtualKey) BeforeSave(tx *gorm.DB) error {
	encKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	apiKey, err := encryptKey(&key.APIKey, encKey)
	if err != nil {
		return fmt.Errorf("error while encrypting APIKey: %w", err)
	}
	key.APIKey = *apiKey
	return nil
}

func (key *StoreVirtualKey) AfterFind(tx *gorm.DB) error {
	decKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	apiKey, err := decryptKey(&key.APIKey, decKey)
	if err != nil {
		return fmt.Errorf("error while decrypting APIKey: %w", err)
	}
	key.APIKey = *apiKey
	return nil
}

func (s *Store) ListActiveVirtualKeys() ([]StoreVirtualKey, error) {
	var keys []StoreVirtualKey
	if err := s.DB.Where("is_active = ?", true).Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("failed to list active virtual keys: %w", err)
	}
	return keys, nil
}

func (s *Store) GetVirtualKey(id string) (*StoreVirtualKey, error) {
	var key StoreVirtualKey
	if err := s.DB.Where("id = ?", id).First(&key).Error; err != nil {
		return nil, fmt.Errorf("virtual key %q not found: %w", id, err)
	}
	return &key, nil
}

func (s *Store) ListVirtualKeys() ([]StoreVirtualKey, error) {
	var keys []StoreVirtualKey
	if err := s.DB.Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("failed to list virtual keys: %w", err)
	}
	return keys, nil
}

func (s *Store) RevokeVirtualKey(id string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var key StoreVirtualKey
		if err := tx.Where("id = ?", id).First(&key).Error; err != nil {
			return fmt.Errorf("virtual key %q not found: %w", id, err)
		}
		if err := tx.Model(&key).Update("is_active", false).Error; err != nil {
			return fmt.Errorf("failed to revoke key: %w", err)
		}
		return tx.Model(&StoreBudget{}).Where("id = ?", key.BudgetID).Update("status", "released").Error
	})
}

type UpdateVirtualKeyRoutingRequest struct {
	Mode            *core.VKMode
	ProviderConfigs []core.ProviderConfig
	CustomPoolName  *string
	LoadBalancer    *core.LBKind
}

func (s *Store) UpdateVirtualKeyRouting(keyID string, request UpdateVirtualKeyRoutingRequest) (*StoreVirtualKey, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var key StoreVirtualKey
		if err := tx.Where("id = ?", keyID).First(&key).Error; err != nil {
			return err
		}
		if request.Mode != nil {
			key.Mode = request.Mode.String()
		}
		if request.ProviderConfigs != nil {
			key.ProviderConfigs = request.ProviderConfigs
		}
		if request.CustomPoolName != nil {
			key.CustomPoolName = strings.TrimSpace(*request.CustomPoolName)
		}
		if request.LoadBalancer != nil {
			key.LoadBalancer = request.LoadBalancer.String()
		}
		mode, err := core.ParseVKMode(key.Mode)
		if err != nil {
			return err
		}
		if _, err := core.CompileProviderConfigs(mode, key.ProviderConfigs); err != nil {
			return err
		}
		if _, err := core.ParseLBKind(key.LoadBalancer); err != nil {
			return err
		}
		if strings.Contains(key.CustomPoolName, "/") {
			return fmt.Errorf("custom pool name cannot contain '/'")
		}
		return tx.Save(&key).Error
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

func (s *Store) CreateVirtualKeyTx(payload *core.VirtualKeyRequest, apiKey, hash, prefix string) (*CreateVirtualKeyResult, error) {
	if payload.Mode == nil {
		return nil, fmt.Errorf("mode is required")
	}
	if payload.LoadBalancer == nil {
		return nil, fmt.Errorf("load balancer is required")
	}
	if _, err := core.CompileProviderConfigs(*payload.Mode, payload.ProviderConfigs); err != nil {
		return nil, err
	}
	customPoolName := strings.TrimSpace(payload.CustomPoolName)
	if strings.Contains(customPoolName, "/") {
		return nil, fmt.Errorf("custom pool name cannot contain '/'")
	}

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

		virtualKey := StoreVirtualKey{
			ID:              uuid.Must(uuid.NewV7()).String(),
			KeyHash:         hash,
			APIKey:          apiKey,
			DisplayPrefix:   prefix,
			ClientID:        payload.ClientID,
			BudgetID:        budget.ID,
			Mode:            payload.Mode.String(),
			ProviderConfigs: payload.ProviderConfigs,
			CustomPoolName:  customPoolName,
			LoadBalancer:    payload.LoadBalancer.String(),
			IsActive:        true,
			ExpiresAt:       payload.ExpiresAt,
		}
		if err := tx.Create(&virtualKey).Error; err != nil {
			return fmt.Errorf("insert virtual key: %w", err)
		}
		result.VirtualKey = &virtualKey
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Store) DeleteVirtualKey(id string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var key StoreVirtualKey
		if err := tx.Where("id = ?", id).First(&key).Error; err != nil {
			return fmt.Errorf("virtual key %q not found: %w", id, err)
		}
		if err := tx.Delete(&key).Error; err != nil {
			return fmt.Errorf("failed to delete key: %w", err)
		}
		return tx.Model(&StoreBudget{}).Where("id = ?", key.BudgetID).Update("status", "released").Error
	})
}
