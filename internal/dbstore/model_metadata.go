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
	ID                   string        `gorm:"primaryKey;type:text" json:"id"`
	Source               string        `gorm:"not null;type:text;uniqueIndex:uq_model_metadata,priority:1;default:manual" json:"source"`
	ProviderID           string        `gorm:"not null;type:text;uniqueIndex:uq_model_metadata,priority:2;index:ix_model_metadata_lookup,priority:1" json:"provider_id"`
	Provider             StoreProvider `gorm:"foreignKey:ProviderID;references:ID"                                                                   json:"provider"`
	ModelName            string        `gorm:"not null;type:text;uniqueIndex:uq_model_metadata,priority:3;index:ix_model_metadata_lookup,priority:2" json:"model_name"`
	ModelType            string        `gorm:"not null;type:text;uniqueIndex:uq_model_metadata,priority:4;index:ix_model_metadata_lookup,priority:3" json:"model_type"`
	BaseModel            string        `gorm:"type:text;index"    json:"base_model"`
	Capabilities         []string      `gorm:"serializer:json;type:text" json:"capabilities"`
	ContextWindow        int32         `json:"context_window"`
	MaxInputTokens       int32         `json:"max_input_tokens"`
	MaxOutputTokens      int32         `json:"max_output_tokens"`
	LongContextThreshold int32         `json:"long_context_threshold"`
	SourceRawKey         string        `gorm:"type:text" json:"source_raw_key"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
}

func (StoreModelMetadata) TableName() string { return "model_metadata" }

func (s *StoreModelMetadata) ToCore() core.ModelMetadata {
	return core.ModelMetadata{
		ID:         s.ID,
		Provider:   core.Provider(s.Provider.Name),
		ModelType:  core.ParseModelType(s.ModelType),
		ModelName:  s.ModelName,
		BaseModel:  s.BaseModel,
		Capability: core.ParseCapabilityStrings(s.Capabilities),
		Limits: core.ModelLimits{
			ContextWindow:        s.ContextWindow,
			MaxInputTokens:       s.MaxInputTokens,
			MaxOutputTokens:      s.MaxOutputTokens,
			LongContextThreshold: s.LongContextThreshold,
		},
		SourceRawKey: s.SourceRawKey,
	}
}

func newStoreModelMetadata(md *core.ModelMetadata, providerID, source string, now time.Time) StoreModelMetadata {
	return StoreModelMetadata{
		ID:                   uuid.Must(uuid.NewV7()).String(),
		Source:               source,
		ProviderID:           providerID,
		ModelName:            md.ModelName,
		ModelType:            md.ModelType.String(),
		BaseModel:            md.BaseModel,
		Capabilities:         md.Capability.String(),
		ContextWindow:        md.Limits.ContextWindow,
		MaxInputTokens:       md.Limits.MaxInputTokens,
		MaxOutputTokens:      md.Limits.MaxOutputTokens,
		LongContextThreshold: md.Limits.LongContextThreshold,
		SourceRawKey:         md.SourceRawKey,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func (s *Store) CreateModelMetadata(md core.ModelMetadata, source string) (*StoreModelMetadata, error) {
	var payload StoreModelMetadata
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		provider, err := s.resolveProvider(tx, md.Provider)
		if err != nil {
			return err
		}
		payload = newStoreModelMetadata(&md, provider.ID, source, time.Now())
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

func (s *Store) UpdateModelMetadata(id string, md core.ModelMetadata) (*StoreModelMetadata, error) {
	payload := map[string]any{
		"model_type":             md.ModelType.String(),
		"base_model":             md.BaseModel,
		"capabilities":           md.Capability.String(),
		"context_window":         md.Limits.ContextWindow,
		"max_input_tokens":       md.Limits.MaxInputTokens,
		"max_output_tokens":      md.Limits.MaxOutputTokens,
		"long_context_threshold": md.Limits.LongContextThreshold,
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

func (s *Store) BulkSyncModelMetadata(source string, models []core.ModelMetadata) error {
	if source == "" {
		return fmt.Errorf("source is required for a metadata sync")
	}
	if len(models) == 0 {
		return nil
	}

	providerIDs, err := s.ProviderIDs(nil)
	if err != nil {
		return fmt.Errorf("metadata sync: %w", err)
	}

	now := time.Now()
	rows := make([]StoreModelMetadata, 0, len(models))
	seen := make(map[core.CatalogKey]struct{}, len(models))
	skipped, duplicates := 0, 0

	for i := range models {
		md := &models[i]
		providerID, ok := providerIDs[md.Provider]
		if !ok {
			skipped++
			continue
		}

		catalogKey := md.CatalogKey()
		if _, exists := seen[catalogKey]; exists {
			duplicates++
			continue
		}
		seen[catalogKey] = struct{}{}
		rows = append(rows, newStoreModelMetadata(md, providerID, source, now))
	}

	if skipped > 0 {
		s.logger.Warn("metadata sync skipped models with unknown providers",
			zap.Int("skipped", skipped), zap.String("source", source))
	}
	if duplicates > 0 {
		s.logger.Warn("metadata sync dropped duplicate model keys",
			zap.Int("duplicates", duplicates), zap.String("source", source))
	}
	if len(rows) == 0 {
		return nil
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "source"}, {Name: "provider_id"},
				{Name: "model_name"}, {Name: "model_type"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"base_model", "capabilities", "context_window",
				"max_input_tokens", "max_output_tokens", "long_context_threshold",
				"source_raw_key", "updated_at",
			}),
		}).CreateInBatches(rows, 500).Error
		if err != nil {
			return fmt.Errorf("model metadata upsert (%s): %w", source, err)
		}

		return tx.Where("source = ? AND updated_at < ?", source, now).
			Delete(&StoreModelMetadata{}).Error
	})
}
