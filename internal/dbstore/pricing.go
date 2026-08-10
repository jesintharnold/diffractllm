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

type StoreModelPricing struct {
	ID          string           `gorm:"primaryKey;type:text" json:"id"`
	Source      string           `gorm:"not null;type:text;uniqueIndex:uq_model_pricing_source_row,priority:1;default:manual" json:"source"`
	RawKey      string           `gorm:"not null;type:text;uniqueIndex:uq_model_pricing_source_row,priority:2"                json:"raw_key"`
	ProviderID  string           `gorm:"not null;type:text;index:ix_model_pricing_lookup,priority:1" json:"provider_id"`
	Provider    StoreProvider    `gorm:"foreignKey:ProviderID;references:ID"                          json:"provider"`
	ModelName   string           `gorm:"not null;type:text;index:ix_model_pricing_lookup,priority:2" json:"model_name"`
	SelectorKey string           `gorm:"not null;type:text;default:'{}';index:ix_model_pricing_lookup,priority:3" json:"selector_key"`
	Selectors   core.SelectorSet `gorm:"serializer:json;type:text"                                                json:"selectors"`
	ModelType   string           `gorm:"not null;type:text" json:"model_type"`
	Pricing     core.Pricing     `gorm:"serializer:json;type:text" json:"pricing"`

	HeadlineInputCostPerToken  *float64 `gorm:"column:input_cost_per_token;index:ix_model_pricing_input_cost"   json:"input_cost_per_token,omitempty"`
	HeadlineOutputCostPerToken *float64 `gorm:"column:output_cost_per_token;index:ix_model_pricing_output_cost" json:"output_cost_per_token,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (StoreModelPricing) TableName() string { return "model_pricing" }

func (s *StoreModelPricing) ToCore() core.PricingVariant {
	selectors := s.Selectors
	selectors.Key = s.SelectorKey

	return core.PricingVariant{
		ID:     s.ID,
		Source: s.Source,
		RawKey: s.RawKey,
		Key: core.PriceKey{
			ModelKey: core.ModelKey{
				Provider:  core.Provider(s.Provider.Name),
				ModelName: s.ModelName,
			},
			SelectorKey: s.SelectorKey,
		},
		ModelType: core.ParseModelType(s.ModelType),
		Selectors: selectors,
		Pricing:   s.Pricing,
	}
}

func newStoreModelPricing(variant *core.PricingVariant, providerID, source string, now time.Time) StoreModelPricing {
	return StoreModelPricing{
		ID:                         uuid.Must(uuid.NewV7()).String(),
		Source:                     source,
		RawKey:                     variant.RawKey,
		ProviderID:                 providerID,
		ModelName:                  variant.Key.ModelName,
		SelectorKey:                variant.Selectors.CanonicalKey(),
		Selectors:                  variant.Selectors,
		ModelType:                  variant.ModelType.String(),
		Pricing:                    variant.Pricing,
		HeadlineInputCostPerToken:  variant.Pricing.InputCostPerToken,
		HeadlineOutputCostPerToken: variant.Pricing.OutputCostPerToken,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
}

func (s *Store) ListModelPricing() ([]StoreModelPricing, error) {
	var rows []StoreModelPricing
	if err := s.DB.Preload("Provider").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list model pricing: %w", err)
	}
	return rows, nil
}

func (s *Store) GetModelPricing(id string) (*StoreModelPricing, error) {
	var row StoreModelPricing
	if err := s.DB.Preload("Provider").Where("id = ?", id).First(&row).Error; err != nil {
		return nil, fmt.Errorf("model pricing %q not found: %w", id, err)
	}
	return &row, nil
}

func (s *Store) CreateModelPricing(variant core.PricingVariant, source string) (*StoreModelPricing, error) {
	var payload StoreModelPricing
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		provider, err := s.resolveProvider(tx, variant.Key.Provider)
		if err != nil {
			return err
		}
		payload = newStoreModelPricing(&variant, provider.ID, source, time.Now())
		if err := tx.Create(&payload).Error; err != nil {
			return fmt.Errorf("create model pricing for %s: %w", variant.RawKey, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetModelPricing(payload.ID)
}

func (s *Store) UpdateModelPricingRates(id string, pricing core.Pricing) (*StoreModelPricing, error) {
	res := s.DB.Model(&StoreModelPricing{}).Where("id = ?", id).Updates(map[string]any{
		"pricing":               pricing,
		"input_cost_per_token":  pricing.InputCostPerToken,
		"output_cost_per_token": pricing.OutputCostPerToken,
		"updated_at":            time.Now(),
	})
	if res.Error != nil {
		return nil, fmt.Errorf("update model pricing %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("model pricing %q not found", id)
	}
	return s.GetModelPricing(id)
}

func (s *Store) DeleteModelPricing(id string) error {
	res := s.DB.Where("id = ?", id).Delete(&StoreModelPricing{})
	if res.Error != nil {
		return fmt.Errorf("delete model pricing %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("model pricing %q not found", id)
	}
	return nil
}

func (s *Store) BulkSyncModelPricing(source string, variants []core.PricingVariant) error {
	if source == "" {
		return fmt.Errorf("source is required for a pricing sync")
	}
	if len(variants) == 0 {
		return nil
	}

	providerIDs, err := s.ProviderIDs(nil)
	if err != nil {
		return fmt.Errorf("pricing sync: %w", err)
	}

	now := time.Now()
	rows := make([]StoreModelPricing, 0, len(variants))
	seen := make(map[string]struct{}, len(variants))
	skipped, duplicates := 0, 0

	for i := range variants {
		variant := &variants[i]
		providerID, ok := providerIDs[variant.Key.Provider]
		if !ok {
			skipped++
			continue
		}

		if _, exists := seen[variant.RawKey]; exists {
			duplicates++
			continue
		}
		seen[variant.RawKey] = struct{}{}
		rows = append(rows, newStoreModelPricing(variant, providerID, source, now))
	}

	if skipped > 0 {
		s.logger.Warn("pricing sync skipped variants with unknown providers",
			zap.Int("skipped", skipped), zap.String("source", source))
	}
	if duplicates > 0 {
		s.logger.Warn("pricing sync dropped duplicate raw keys",
			zap.Int("duplicates", duplicates), zap.String("source", source))
	}
	if len(rows) == 0 {
		return nil
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "source"}, {Name: "raw_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"provider_id", "model_name", "selector_key", "selectors",
				"model_type", "pricing", "input_cost_per_token",
				"output_cost_per_token", "updated_at",
			}),
		}).CreateInBatches(rows, 500).Error
		if err != nil {
			return fmt.Errorf("model pricing upsert (%s): %w", source, err)
		}

		return tx.Where("source = ? AND updated_at < ?", source, now).
			Delete(&StoreModelPricing{}).Error
	})
}

type StoreCustomModelPricing struct {
	ID        string `gorm:"primaryKey;type:text"                              json:"id"`
	Name      string `gorm:"not null;type:text"                                json:"name"`
	ModelName string `gorm:"not null;type:text;uniqueIndex:idx_override_scope" json:"model_name"`
	ModelType string `gorm:"not null;type:text"                                json:"model_type"`

	ScopeType         core.ScopeType `gorm:"not null;type:text;uniqueIndex:idx_override_scope" json:"scope_type"`
	ScopeVirtualkeyID *string        `gorm:"type:text;uniqueIndex:idx_override_scope"          json:"scope_virtual_key_id"`
	ScopeProviderID   *string        `gorm:"type:text"                                         json:"scope_provider_id,omitempty"`
	ScopeProvider     *StoreProvider `gorm:"foreignKey:ScopeProviderID;references:ID"          json:"scope_provider,omitempty"`

	Pricing   core.Pricing `gorm:"serializer:json;type:text" json:"pricing"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func (StoreCustomModelPricing) TableName() string { return "model_pricing_override" }

func (o *StoreCustomModelPricing) ToCore() *core.CustomPricing {
	out := core.CustomPricing{
		ID:                o.ID,
		Name:              o.Name,
		ModelName:         o.ModelName,
		ModelType:         core.ParseModelType(o.ModelType),
		ScopeType:         core.ScopeType(o.ScopeType),
		ScopeVirtualkeyID: o.ScopeVirtualkeyID,
	}

	if o.ScopeProvider != nil {
		p := core.Provider(o.ScopeProvider.Name)
		out.ScopeProvider = &p
	}

	out.Pricing = o.Pricing
	return &out
}

func (s *Store) CreateCustomPricing(b core.CustomPricingRequest) (*StoreCustomModelPricing, error) {
	payload := StoreCustomModelPricing{
		ID:        uuid.Must(uuid.NewV7()).String(),
		Name:      b.Name,
		ModelName: b.ModelName,
		ModelType: b.ModelType,
		ScopeType: b.ScopeType,
	}

	switch b.ScopeType {
	case core.ScopeGlobal:

	case core.ScopeProvider:
		if b.ScopeProvider == nil {
			return nil, fmt.Errorf("scope_provider required when scope_type=provider")
		}

		var provider StoreProvider
		if err := s.DB.Where("name = ?", string(*b.ScopeProvider)).First(&provider).Error; err != nil {
			return nil, fmt.Errorf("provider %q not found: %w", *b.ScopeProvider, err)
		}
		payload.ScopeProviderID = &provider.ID

	case core.ScopeVirtualKey:
		if b.ScopeVirtualkeyID == nil || *b.ScopeVirtualkeyID == "" {
			return nil, fmt.Errorf("scope_virtual_key_id required when scope_type=virtualkey")
		}
		payload.ScopeVirtualkeyID = b.ScopeVirtualkeyID

	default:
		return nil, fmt.Errorf("invalid scope_type %q", b.ScopeType)
	}

	payload.Pricing = b.Pricing

	if err := s.DB.Create(&payload).Error; err != nil {
		return nil, fmt.Errorf("create override pricing: %w", err)
	}

	var created StoreCustomModelPricing
	if err := s.DB.Preload("ScopeProvider").Where("id = ?", payload.ID).First(&created).Error; err != nil {
		return nil, fmt.Errorf("reload override pricing: %w", err)
	}

	return &created, nil
}

func (s *Store) GetCustomPricing(pricingID string) (*StoreCustomModelPricing, error) {
	var result StoreCustomModelPricing
	if err := s.DB.Preload("ScopeProvider").Where("id = ?", pricingID).First(&result).Error; err != nil {
		return nil, fmt.Errorf("get override pricing %q: %w", pricingID, err)
	}
	return &result, nil
}

func (s *Store) ListCustomPricing() ([]StoreCustomModelPricing, error) {
	var result []StoreCustomModelPricing
	if err := s.DB.Preload("ScopeProvider").Find(&result).Error; err != nil {
		return nil, fmt.Errorf("failed to list override pricing for models: %w", err)
	}
	return result, nil
}

func (s *Store) UpdateCustomPricing(pricingID string, pricing core.Pricing) (*StoreCustomModelPricing, error) {
	var result StoreCustomModelPricing
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&StoreCustomModelPricing{}).Where("id = ?", pricingID).Update("pricing", pricing)
		if res.Error != nil {
			return fmt.Errorf("update override pricing %q: %w", pricingID, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("override pricing %q not found", pricingID)
		}
		if err := tx.Preload("ScopeProvider").Where("id = ?", pricingID).First(&result).Error; err != nil {
			return fmt.Errorf("reload override pricing %q: %w", pricingID, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
