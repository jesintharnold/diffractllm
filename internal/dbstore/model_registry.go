package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StoreModelCatalog struct {
	ID         string        `gorm:"primaryKey;type:text"               json:"id"`
	ModelName  string        `gorm:"not null;type:text;uniqueIndex:idx_model_provider" json:"model_name"`
	IsActive   bool          `gorm:"not null;default:true" json:"is_active"`
	Kind       string        `gorm:"not null;type:text"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	ProviderID string        `gorm:"not null;type:text;uniqueIndex:idx_model_provider" json:"provider_id"`
	Provider   StoreProvider `gorm:"foreignKey:ProviderID;references:ID" json:"provider"`
}

func (StoreModelCatalog) TableName() string { return "model_registry" }

// -------- Model Registry Store methods ---------

func (s *Store) CreateCatalogModel(modelName string, provider core.Provider, kind string) (*StoreModelCatalog, error) {
	var payload StoreModelCatalog
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		provider, err := s.resolveProvider(tx, provider)
		if err != nil {
			return err
		}
		payload = StoreModelCatalog{
			ID:         uuid.Must(uuid.NewV7()).String(),
			ModelName:  modelName,
			Kind:       kind,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			ProviderID: provider.ID,
		}
		if err := tx.Create(&payload).Error; err != nil {
			return fmt.Errorf("failed to create catalog model %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &payload, nil
}

func (s *Store) GetCatalogModelByID(id string) (*StoreModelCatalog, error) {
	var row StoreModelCatalog
	if err := s.DB.Preload("Provider").Where("id = ?", id).First(&row).Error; err != nil {
		return nil, fmt.Errorf("catalog model %q not found: %w", id, err)
	}
	return &row, nil
}

func (s *Store) GetCatalogModelByNameAndProviderID(modelName string, providerID string) (*StoreModelCatalog, error) {
	var row StoreModelCatalog
	if err := s.DB.Preload("Provider").
		Where("model_name = ? AND provider_id = ?", modelName, providerID).
		First(&row).Error; err != nil {
		return nil, fmt.Errorf("catalog model %q on provider %q not found: %w", modelName, providerID, err)
	}
	return &row, nil
}

func (s *Store) ListCatalogModels(activeOnly bool) ([]StoreModelCatalog, error) {
	var rows []StoreModelCatalog
	if err := s.DB.Preload("Provider").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list catalog models: %w", err)
	}
	return rows, nil
}

func (s *Store) UpdateCatalogModel(id string, kind *string, modelName *string) (*StoreModelCatalog, error) {
	updatecols := []string{"UpdatedAt"}
	payload := StoreModelCatalog{UpdatedAt: time.Now()}

	if kind != nil {
		payload.Kind = *kind
		updatecols = append(updatecols, "Kind")
	}

	if modelName != nil {
		payload.ModelName = *modelName
		updatecols = append(updatecols, "model_name")
	}

	res := s.DB.Model(&StoreModelCatalog{}).Where("id = ?", id).Select(updatecols).Updates(payload)
	if res.Error != nil {
		return nil, fmt.Errorf("update catalog model %q failed: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("catalog model %q not found", id)
	}
	return s.GetCatalogModelByID(id)
}

func (s *Store) DeleteCatalogModel(id string) error {
	res := s.DB.Where("id = ?", id).Delete(&StoreModelCatalog{})
	if res.Error != nil {
		return fmt.Errorf("delete catalog model %q failed: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("catalog model %q not found", id)
	}
	return nil
}

func (s *Store) BulkSyncModelCatalog(models []core.ModelCatalogRequest) error {
	var allProviders []StoreProvider
	if err := s.DB.Find(&allProviders).Error; err != nil {
		return fmt.Errorf("failed to load providers for sync: %w", err)
	}
	providerMap := make(map[string]string)
	for _, p := range allProviders {
		providerMap[p.Name] = p.ID
	}
	var rowsToInsert []StoreModelCatalog
	for _, mc := range models {
		providerID, ok := providerMap[mc.Provider]
		if !ok {
			continue
		}
		rowsToInsert = append(rowsToInsert, StoreModelCatalog{
			ID:         uuid.Must(uuid.NewV7()).String(),
			ModelName:  mc.ModelName,
			Kind:       mc.Kind,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			ProviderID: providerID,
		})
	}
	if len(rowsToInsert) == 0 {
		return nil
	}
	err := s.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "model_name"}, {Name: "provider_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"kind", "updated_at"}),
	}).CreateInBatches(rowsToInsert, 500).Error
	if err != nil {
		return fmt.Errorf("failed bulk sync: %w", err)
	}
	return nil
}
