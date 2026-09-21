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
	ID          string        `gorm:"primaryKey;type:text" json:"id"`
	RawKey      string        `gorm:"not null;type:text;uniqueIndex:idx_uq_model_pricing_raw_key" json:"raw_key"`
	ProviderID  string        `gorm:"not null;type:text" json:"provider_id"`
	Provider    StoreProvider `gorm:"foreignKey:ProviderID;references:ID"                          json:"provider"`
	ModelName   string        `gorm:"not null;type:text" json:"model_name"`
	SelectorKey string        `gorm:"not null;type:text;default:'{}'" json:"selector_key"`
	ModelType   string        `gorm:"not null;type:text" json:"model_type"`
	Pricing     core.Pricing  `gorm:"serializer:json;type:text" json:"pricing"`

	HeadlineInputCostPerToken  *float64 `gorm:"column:input_cost_per_token"   json:"input_cost_per_token,omitempty"`
	HeadlineOutputCostPerToken *float64 `gorm:"column:output_cost_per_token" json:"output_cost_per_token,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (StoreModelPricing) TableName() string { return "model_pricing" }

func (s *StoreModelPricing) ToCore() core.PricingVariant {
	return core.PricingVariant{
		ID:        s.ID,
		RawKey:    s.RawKey,
		Provider:  core.Provider(s.Provider.Name),
		ModelName: s.ModelName,
		ModelType: core.ParseModelType(s.ModelType),
		Selectors: core.SelectorSet{Key: s.SelectorKey},
		Pricing:   s.Pricing,
	}
}

func newStoreModelPricing(variant *core.PricingVariant, providerID string, now time.Time) StoreModelPricing {
	return StoreModelPricing{
		ID:                         uuid.Must(uuid.NewV7()).String(),
		RawKey:                     variant.RawKey,
		ProviderID:                 providerID,
		ModelName:                  variant.ModelName,
		SelectorKey:                variant.Selectors.CanonicalKey(),
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

func (s *Store) CreateModelPricing(variant core.PricingVariant) (*StoreModelPricing, error) {
	var payload StoreModelPricing
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		provider, err := s.resolveProvider(tx, variant.Provider)
		if err != nil {
			return err
		}
		payload = newStoreModelPricing(&variant, provider.ID, time.Now())
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

func (s *Store) BulkSyncModelPricing(variants []core.PricingVariant) error {
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
		providerID, ok := providerIDs[variant.Provider]
		if !ok {
			skipped++
			continue
		}

		if _, exists := seen[variant.RawKey]; exists {
			duplicates++
			continue
		}
		seen[variant.RawKey] = struct{}{}
		rows = append(rows, newStoreModelPricing(variant, providerID, now))
	}

	if skipped > 0 {
		s.logger.Warn("pricing sync skipped variants with unknown providers",
			zap.Int("skipped", skipped))
	}
	if duplicates > 0 {
		s.logger.Warn("pricing sync dropped duplicate raw keys",
			zap.Int("duplicates", duplicates))
	}
	if len(rows) == 0 {
		return nil
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "raw_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"provider_id", "model_name", "selector_key",
				"model_type", "pricing", "input_cost_per_token",
				"output_cost_per_token", "updated_at",
			}),
		}).CreateInBatches(rows, 500).Error
		if err != nil {
			return fmt.Errorf("model pricing upsert: %w", err)
		}
		return nil
	})
}

type StoreCustomModelPricing struct {
	ID                string         `gorm:"primaryKey;type:text"                              json:"id"`
	Name              string         `gorm:"not null;type:text"                                json:"name"`
	ModelName         string         `gorm:"not null;type:text;uniqueIndex:idx_uq_cp_scope,priority:1" json:"model_name"`
	ModelType         string         `gorm:"not null;type:text;uniqueIndex:idx_uq_cp_scope,priority:2" json:"model_type"`
	ScopeType         core.ScopeType `gorm:"not null;type:text;uniqueIndex:idx_uq_cp_scope,priority:3" json:"scope_type"`
	ScopeVirtualkeyID *string        `gorm:"type:text"                                                json:"scope_virtual_key_id"`
	ScopeProviderID   *string        `gorm:"type:text"                                                json:"scope_provider_id,omitempty"`
	ScopeProvider     *StoreProvider `gorm:"foreignKey:ScopeProviderID;references:ID"                  json:"scope_provider,omitempty"`
	ScopeRef          string         `gorm:"not null;type:text;default:'';uniqueIndex:idx_uq_cp_scope,priority:4" json:"-"`
	Pricing           core.Pricing   `gorm:"serializer:json;type:text" json:"pricing"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

func (StoreCustomModelPricing) TableName() string { return "model_pricing_override" }

func (o *StoreCustomModelPricing) BeforeSave(tx *gorm.DB) error {
	switch o.ScopeType {
	case core.ScopeProvider:
		if o.ScopeProviderID == nil {
			return fmt.Errorf("scope_provider_id required when scope_type=provider")
		}
		o.ScopeRef = *o.ScopeProviderID
	case core.ScopeVirtualKey:
		if o.ScopeVirtualkeyID == nil {
			return fmt.Errorf("scope_virtual_key_id required when scope_type=virtualkey")
		}
		o.ScopeRef = *o.ScopeVirtualkeyID
	case core.ScopeGlobal:
		o.ScopeRef = ""
	default:
		return fmt.Errorf("invalid scope_type %q", o.ScopeType)
	}
	return nil
}

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

func (s *Store) hasBasePricing(modelName, modelType string, provider *core.Provider) (bool, error) {
	query := s.DB.Model(&StoreModelPricing{}).Where("model_name = ? AND model_type = ?", modelName, modelType)
	if provider != nil {
		query = query.Joins("JOIN providers ON providers.id = model_pricing.provider_id").Where("providers.name = ?", string(*provider))
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, fmt.Errorf("checking base pricing for %q: %w", modelName, err)
	}
	return count > 0, nil
}

func (s *Store) CreateCustomPricing(b core.CustomPricingRequest) (*StoreCustomModelPricing, error) {
	if core.ParseModelType(b.ModelType) == core.ModelTypeUnknown {
		return nil, fmt.Errorf("invalid model_type %q", b.ModelType)
	}

	var scopedProvider *core.Provider
	if b.ScopeType == core.ScopeProvider {
		scopedProvider = b.ScopeProvider
	}
	hasBase, err := s.hasBasePricing(b.ModelName, b.ModelType, scopedProvider)
	if err != nil {
		return nil, err
	}
	if !hasBase {
		return nil, fmt.Errorf("no base price for %q (%s): custom pricing overrides a price, it cannot create one", b.ModelName, b.ModelType)
	}

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
		var row StoreCustomModelPricing
		if err := tx.Where("id = ?", pricingID).First(&row).Error; err != nil {
			return fmt.Errorf("override pricing %q not found: %w", pricingID, err)
		}
		row.Pricing = pricing
		if err := tx.Save(&row).Error; err != nil {
			return fmt.Errorf("update override pricing %q: %w", pricingID, err)
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

func (s *Store) DeleteCustomPricing(pricingID string) error {
	res := s.DB.Where("id = ?", pricingID).Delete(&StoreCustomModelPricing{})
	if res.Error != nil {
		return fmt.Errorf("delete override pricing %q: %w", pricingID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("override pricing %q not found", pricingID)
	}
	return nil
}

type CustomPricingFilters struct {
	ScopeType    string
	VirtualKeyID string
	Provider     string
	ModelName    string
}

func (s *Store) ListCustomPricingFiltered(filters CustomPricingFilters) ([]StoreCustomModelPricing, error) {
	query := s.DB.Preload("ScopeProvider").Model(&StoreCustomModelPricing{})

	if filters.ScopeType != "" {
		query = query.Where("scope_type = ?", filters.ScopeType)
	}
	if filters.VirtualKeyID != "" {
		query = query.Where("scope_virtualkey_id = ?", filters.VirtualKeyID)
	}
	if filters.ModelName != "" {
		query = query.Where("model_name = ?", filters.ModelName)
	}
	if filters.Provider != "" {
		query = query.Joins("JOIN providers ON providers.id = model_pricing_override.scope_provider_id").
			Where("providers.name = ?", filters.Provider)
	}

	var result []StoreCustomModelPricing
	if err := query.Find(&result).Error; err != nil {
		return nil, fmt.Errorf("failed to list override pricing: %w", err)
	}
	return result, nil
}
