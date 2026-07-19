package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Storepricing struct {
	InputCostPerToken              *float64 `json:"input_cost_per_token,omitempty"`
	OutputCostPerToken             *float64 `json:"output_cost_per_token,omitempty"`
	CacheReadInputTokenCost        *float64 `json:"cache_read_input_token_cost,omitempty"`
	CacheCreationInputTokenCost    *float64 `json:"cache_creation_input_token_cost,omitempty"`
	CacheCreationInputTokenCost1Hr *float64 `json:"cache_creation_input_token_cost_1hr,omitempty"`

	InputCostPerTokenPriority       *float64 `json:"input_cost_per_token_priority,omitempty"`
	OutputCostPerTokenPriority      *float64 `json:"output_cost_per_token_priority,omitempty"`
	CacheReadInputTokenCostPriority *float64 `json:"cache_read_input_token_cost_priority,omitempty"`

	InputCostPerTokenFlex       *float64 `json:"input_cost_per_token_flex,omitempty"`
	OutputCostPerTokenFlex      *float64 `json:"output_cost_per_token_flex,omitempty"`
	CacheReadInputTokenCostFlex *float64 `json:"cache_read_input_token_cost_flex,omitempty"`

	InputCostPerTokenBatch       *float64 `json:"input_cost_per_token_batch,omitempty"`
	OutputCostPerTokenBatch      *float64 `json:"output_cost_per_token_batch,omitempty"`
	CacheReadInputTokenCostBatch *float64 `json:"cache_read_input_token_cost_batch,omitempty"`

	LongContextThreshold                 *int     `json:"long_context_threshold,omitempty"`
	InputCostPerTokenAboveTier           *float64 `json:"input_cost_per_token_above_tier,omitempty"`
	OutputCostPerTokenAboveTier          *float64 `json:"output_cost_per_token_above_tier,omitempty"`
	CacheReadInputTokenCostAboveTier     *float64 `json:"cache_read_input_token_cost_above_tier,omitempty"`
	CacheCreationInputTokenCostAboveTier *float64 `json:"cache_creation_input_token_cost_above_tier,omitempty"`

	InputCostPerCharacter   *float64 `json:"input_cost_per_character,omitempty"`
	InputCostPerAudioSecond *float64 `json:"input_cost_per_audio_second,omitempty"`
	InputCostPerAudioToken  *float64 `json:"input_cost_per_audio_token,omitempty"`
	OutputCostPerAudioToken *float64 `json:"output_cost_per_audio_token,omitempty"`
}

// ------------------ Pricing -------------------

type StoreBaseModelPricing struct {
	ID        string `gorm:"primaryKey;type:text"                                           json:"id"`
	ModelName string `gorm:"not null;type:text;uniqueIndex:idx_model_pricing"               json:"model_name"`

	ProviderID string        `gorm:"not null;type:text;uniqueIndex:idx_model_pricing" json:"provider_id"`
	Provider   StoreProvider `gorm:"foreignKey:ProviderID;references:ID"              json:"provider"`

	ModelType string        `gorm:"not null;type:text;uniqueIndex:idx_model_pricing"               json:"model_type"`
	Rates     *Storepricing `gorm:"serializer:json;type:jsonb"                                     json:"rates"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (StoreBaseModelPricing) TableName() string { return "model_pricing" }

func (s *StoreBaseModelPricing) ToCore() *core.BasePricing {
	out := core.BasePricing{
		ID:        s.ID,
		ModelName: s.ModelName,
		ModelType: s.ModelType,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
	if s.Provider.Name != "" {
		out.Provider = core.Provider(s.Provider.Name)
	}
	if s.Rates != nil {
		out.Pricing = core.Pricing(*s.Rates)
	}
	return &out
}

func (s *Store) CreateBasePricing(modelprice core.BasePricing) (*StoreBaseModelPricing, error) {
	var provider StoreProvider
	if err := s.DB.Where("name = ?", modelprice.Provider).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", modelprice.Provider, err)
	}

	rates := Storepricing(modelprice.Pricing)
	payload := StoreBaseModelPricing{
		ID:         uuid.Must(uuid.NewV7()).String(),
		ModelName:  modelprice.ModelName,
		ModelType:  modelprice.ModelType,
		ProviderID: provider.ID,
		Rates:      &rates,
	}

	if err := s.DB.Create(&payload).Error; err != nil {
		return nil, fmt.Errorf("create model pricing for %s, provider %s: %w", modelprice.ModelName, modelprice.Provider, err)
	}

	var created StoreBaseModelPricing
	if err := s.DB.Preload("Provider").Where("id = ?", payload.ID).First(&created).Error; err != nil {
		return nil, fmt.Errorf("reload model pricing: %w", err)
	}

	return &created, nil
}

func (s *Store) UpdateBasePricing(id string, modelprice core.Pricing) (*StoreBaseModelPricing, error) {
	rates := Storepricing(modelprice)
	var result StoreBaseModelPricing
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&StoreBaseModelPricing{}).Where("id = ?", id).Update("rates", &rates)
		if res.Error != nil {
			return fmt.Errorf("update model pricing %q: %w", id, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("model pricing %q not found", id)
		}

		if err := tx.Preload("Provider").Where("id = ?", id).First(&result).Error; err != nil {
			return fmt.Errorf("reload model pricing %q: %w", id, err)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Store) ListBasePricing() ([]StoreBaseModelPricing, error) {
	var result []StoreBaseModelPricing
	if err := s.DB.Preload("Provider").Find(&result).Error; err != nil {
		return nil, fmt.Errorf("failed to list base pricing for models: %w", err)
	}
	return result, nil
}

// --------------- Custom pricing ----------------

type StoreCustomModelPricing struct {
	ID        string `gorm:"primaryKey;type:text"                                           json:"id"`
	Name      string `gorm:"not null;type:text"                                             json:"name"`
	ModelName string `gorm:"not null;type:text;uniqueIndex:idx_override_scope"              json:"model_name"`
	ModelType string `gorm:"not null;type:text"                                             json:"model_type"`

	ScopeType         core.ScopeType `gorm:"not null;type:text;uniqueIndex:idx_override_scope"              json:"scope_type"`
	ScopeVirtualkeyID *string        `gorm:"type:text;uniqueIndex:idx_override_scope"                       json:"scope_virtual_key_id"`
	ScopeProviderID   *string        `gorm:"type:text"                                                      json:"scope_provider_id,omitempty"`
	ScopeProvider     *StoreProvider `gorm:"foreignKey:ScopeProviderID;references:ID"                       json:"scope_provider,omitempty"`

	Rates     *Storepricing `gorm:"serializer:json;type:jsonb"                                     json:"rates"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (StoreCustomModelPricing) TableName() string { return "model_pricing_override" }

func (o *StoreCustomModelPricing) ToCore() *core.CustomPricing {
	out := core.CustomPricing{
		ID:                o.ID,
		Name:              o.Name,
		ModelName:         o.ModelName,
		ModelType:         o.ModelType,
		ScopeType:         core.ScopeType(o.ScopeType),
		ScopeVirtualkeyID: o.ScopeVirtualkeyID,
	}

	if o.ScopeProvider != nil {
		p := core.Provider(o.ScopeProvider.Name)
		out.ScopeProvider = &p
	}

	if o.Rates != nil {
		out.Pricing = core.Pricing(*o.Rates)
	}

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

	rates := Storepricing(b.Pricing)
	payload.Rates = &rates

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
	rates := Storepricing(pricing)

	var result StoreCustomModelPricing
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&StoreCustomModelPricing{}).Where("id = ?", pricingID).Update("rates", &rates)
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
