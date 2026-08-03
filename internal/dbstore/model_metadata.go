package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StoreModelMetadata struct {
	ID                   string        `gorm:"primaryKey;type:text"                              json:"id"`
	ModelName            string        `gorm:"not null;type:text;uniqueIndex:idx_model_metadata" json:"model_name"`
	ProviderID           string        `gorm:"not null;type:text;uniqueIndex:idx_model_metadata" json:"provider_id"`
	Provider             StoreProvider `gorm:"foreignKey:ProviderID;references:ID"               json:"provider"`
	BaseModel            string        `gorm:"type:text;index"                                   json:"base_model"`
	ModelType            string        `gorm:"not null;type:text"                                json:"model_type"`
	Capabilities         []string      `gorm:"serializer:json;type:jsonb"                        json:"capabilities"`
	ContextWindow        int32         `json:"context_window"`
	MaxInputTokens       int32         `json:"max_input_tokens"`
	MaxOutputTokens      int32         `json:"max_output_tokens"`
	LongContextThreshold int32         `json:"long_context_threshold"`
	Source               string        `gorm:"not null;type:text;index;default:manual"           json:"source"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
}

func (StoreModelMetadata) TableName() string { return "model_metadata" }

func (s *StoreModelMetadata) ToCore() core.ModelMetaData {
	return core.ModelMetaData{
		Provider:             core.Provider(s.Provider.Name),
		ModelName:            s.ModelName,
		BaseModel:            s.BaseModel,
		ContextWindow:        s.ContextWindow,
		MaxInputTokens:       s.MaxInputTokens,
		MaxOutputTokens:      s.MaxOutputTokens,
		LongContextThreshold: s.LongContextThreshold,
		Capability:           core.ParseCapabilityStrings(s.Capabilities),
		ModelType:            core.ParseModelType(s.ModelType),
	}
}

func (s *Store) CreateModelMetadata(md core.ModelMetaData, source string) (*StoreModelMetadata, error) {
	var payload StoreModelMetadata
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		provider, err := s.resolveProvider(tx, md.Provider)
		if err != nil {
			return err
		}
		payload = StoreModelMetadata{
			ID:                   uuid.Must(uuid.NewV7()).String(),
			ModelName:            md.ModelName,
			ProviderID:           provider.ID,
			BaseModel:            md.BaseModel,
			ModelType:            md.ModelType.String(),
			Capabilities:         md.Capability.String(),
			ContextWindow:        md.ContextWindow,
			MaxInputTokens:       md.MaxInputTokens,
			MaxOutputTokens:      md.MaxOutputTokens,
			LongContextThreshold: md.LongContextThreshold,
			Source:               source,
		}
		if err := tx.Create(&payload).Error; err != nil {
			return fmt.Errorf("create model metadata for %s/%s: %w", md.Provider, md.ModelName, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetModelMetadata(payload.ID)
}

func (s *Store) GetModelMetadata(id string) (*StoreModelMetadata, error) {
	var row StoreModelMetadata
	if err := s.DB.Preload("Provider").Where("id = ?", id).First(&row).Error; err != nil {
		return nil, fmt.Errorf("model metadata %q not found: %w", id, err)
	}
	return &row, nil
}

func (s *Store) ListModelMetadata() ([]StoreModelMetadata, error) {
	var rows []StoreModelMetadata
	if err := s.DB.Preload("Provider").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list model metadata: %w", err)
	}
	return rows, nil
}

func (s *Store) ModelNamesByProvider(provider core.Provider) ([]string, error) {
	var row StoreProvider
	if err := s.DB.Select("id").Where("name = ?", string(provider)).First(&row).Error; err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", provider, err)
	}

	var names []string
	if err := s.DB.Model(&StoreModelMetadata{}).
		Where("provider_id = ?", row.ID).
		Pluck("model_name", &names).Error; err != nil {
		return nil, fmt.Errorf("failed to list model names for provider %q: %w", provider, err)
	}
	return names, nil
}

func (s *Store) UpdateModelMetadata(id string, md core.ModelMetaData) (*StoreModelMetadata, error) {
	payload := map[string]any{
		"base_model":             md.BaseModel,
		"model_type":             md.ModelType.String(),
		"capabilities":           md.Capability.String(),
		"context_window":         md.ContextWindow,
		"max_input_tokens":       md.MaxInputTokens,
		"max_output_tokens":      md.MaxOutputTokens,
		"long_context_threshold": md.LongContextThreshold,
		"updated_at":             time.Now(),
	}
	res := s.DB.Model(&StoreModelMetadata{}).Where("id = ?", id).Updates(payload)
	if res.Error != nil {
		return nil, fmt.Errorf("update model metadata %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("model metadata %q not found", id)
	}
	return s.GetModelMetadata(id)
}

func (s *Store) DeleteModelMetadata(id string) error {
	res := s.DB.Where("id = ?", id).Delete(&StoreModelMetadata{})
	if res.Error != nil {
		return fmt.Errorf("delete model metadata %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("model metadata %q not found", id)
	}
	return nil
}

func (s *Store) BulkSyncModelMetadata(source string, models []core.ModelMetaData) error {
	if source == "" {
		return fmt.Errorf("source is required for a metadata sync")
	}
	if len(models) == 0 {
		return nil
	}

	var providers []StoreProvider
	if err := s.DB.Find(&providers).Error; err != nil {
		return fmt.Errorf("failed to load providers for metadata sync: %w", err)
	}
	providerIDs := make(map[core.Provider]string, len(providers))
	for _, p := range providers {
		providerIDs[core.Provider(p.Name)] = p.ID
	}

	now := time.Now()
	rows := make([]StoreModelMetadata, 0, len(models))
	skipped := 0
	for i := range models {
		md := &models[i]
		providerID, ok := providerIDs[md.Provider]
		if !ok {
			skipped++
			continue
		}
		rows = append(rows, StoreModelMetadata{
			ID:                   uuid.Must(uuid.NewV7()).String(),
			ModelName:            md.ModelName,
			ProviderID:           providerID,
			BaseModel:            md.BaseModel,
			ModelType:            md.ModelType.String(),
			Capabilities:         md.Capability.String(),
			ContextWindow:        md.ContextWindow,
			MaxInputTokens:       md.MaxInputTokens,
			MaxOutputTokens:      md.MaxOutputTokens,
			LongContextThreshold: md.LongContextThreshold,
			Source:               source,
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}
	if skipped > 0 {
		s.logger.Warn("metadata sync skipped models with unknown providers",
			zap.Int("skipped", skipped), zap.String("source", source))
	}
	if len(rows) == 0 {
		return nil
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "model_name"}, {Name: "provider_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"base_model", "model_type", "capabilities", "context_window",
				"max_input_tokens", "max_output_tokens", "long_context_threshold",
				"source", "updated_at",
			}),
		}).CreateInBatches(rows, 500).Error
		if err != nil {
			return fmt.Errorf("model metadata upsert (%s): %w", source, err)
		}

		return tx.Where("source = ? AND updated_at < ?", source, now).
			Delete(&StoreModelMetadata{}).Error
	})
}
