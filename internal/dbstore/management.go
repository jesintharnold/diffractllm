package dbstore

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *Store) CreateModel(m *core.Model) (*StoreModelRegistry, error) {
	var provider StoreProvider
	if err := s.DB.Where("name = ?", m.ModelProvider).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", m.ModelProvider, err)
	}

	modelData := StoreModelRegistry{
		ID:            uuid.Must(uuid.NewV7()).String(),
		ModelName:     m.ModelName,
		BaseModel:     m.BaseModel,
		ModelEndpoint: m.ModelEndpoint,
		IsActive:      true,
		ProviderID:    provider.ID,
	}

	if err := s.DB.Create(&modelData).Error; err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}
	if err := s.DB.Preload("Provider").Where("id = ?", modelData.ID).First(&modelData).Error; err != nil {
		return nil, fmt.Errorf("failed to reload model runtime id: %w", err)
	}
	return &modelData, nil
}

func (s *Store) CreateModelWithQuickSetup(m *core.Model) (*StoreModelRegistry, *StoreBackend, *StorePolicy, error) {
	var (
		modelData   StoreModelRegistry
		backendData StoreBackend
		policyData  StorePolicy
	)

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var provider StoreProvider
		if err := tx.Where("name = ?", m.ModelProvider).First(&provider).Error; err != nil {
			return fmt.Errorf("provider %q not found: %w", m.ModelProvider, err)
		}

		modelData = StoreModelRegistry{
			ID:            uuid.Must(uuid.NewV7()).String(),
			ModelName:     m.ModelName,
			BaseModel:     m.BaseModel,
			ModelEndpoint: m.ModelEndpoint,
			IsActive:      true,
			ProviderID:    provider.ID,
		}
		if err := tx.Create(&modelData).Error; err != nil {
			return fmt.Errorf("failed to create model: %w", err)
		}

		backendData = StoreBackend{
			ID:       uuid.Must(uuid.NewV7()).String(),
			Name:     m.ModelName,
			IsActive: true,
			LBType:   core.LB_ROUND_ROBIN,
		}
		if err := tx.Select("*").Create(&backendData).Error; err != nil {
			return fmt.Errorf("failed to create backend: %w", err)
		}

		if err := tx.Create(&StoreBackendModel{
			BackendID: backendData.ID,
			ModelID:   modelData.ID,
			Position:  0,
		}).Error; err != nil {
			return fmt.Errorf("failed to bind model to backend: %w", err)
		}

		policyData = StorePolicy{
			ID:        uuid.Must(uuid.NewV7()).String(),
			Name:      "auto-" + m.ModelName,
			Priority:  50,
			IsEnabled: true,
			BackendID: backendData.ID,
		}
		if err := tx.Create(&policyData).Error; err != nil {
			return fmt.Errorf("failed to create policy: %w", err)
		}

		if err := tx.Create(&StorePolicyCondition{
			PolicyID: policyData.ID,
			Type:     "body",
			Key:      "model",
			Operator: "equals",
			Value:    m.ModelName,
			Position: 0,
		}).Error; err != nil {
			return fmt.Errorf("failed to create policy condition: %w", err)
		}

		if err := tx.Where("id = ?", modelData.ID).First(&modelData).Error; err != nil {
			return fmt.Errorf("failed to reload model runtime id: %w", err)
		}
		if err := tx.Where("id = ?", backendData.ID).First(&backendData).Error; err != nil {
			return fmt.Errorf("failed to reload backend runtime id: %w", err)
		}
		if err := tx.Where("id = ?", policyData.ID).First(&policyData).Error; err != nil {
			return fmt.Errorf("failed to reload policy runtime id: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return &modelData, &backendData, &policyData, nil
}

func (s *Store) GetModel(id string) (*StoreModelRegistry, error) {
	var model StoreModelRegistry
	if err := s.DB.Preload("Provider").Where("id = ?", id).First(&model).Error; err != nil {
		return nil, fmt.Errorf("model %q not found %w", id, err)
	}
	return &model, nil
}

func (s *Store) GetModelByProviderAndName(provider core.Provider, name string) (*StoreModelRegistry, error) {
	var providerRow StoreProvider
	if err := s.DB.Where("name = ?", provider).First(&providerRow).Error; err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", provider, err)
	}
	var model StoreModelRegistry
	if err := s.DB.Preload("Provider").Where("provider_id = ? AND model_name = ?", providerRow.ID, name).First(&model).Error; err != nil {
		return nil, fmt.Errorf("model %s/%s not found: %w", provider, name, err)
	}
	return &model, nil
}

func (s *Store) ListModels(active bool) ([]StoreModelRegistry, error) {
	var models []StoreModelRegistry
	query := s.DB.Preload("Provider")
	if active {
		query = query.Where("is_active = ?", true)
	}
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	return models, nil
}

func (s *Store) UpdateModel(id string, m core.Model) (*StoreModelRegistry, error) {
	var existingModel StoreModelRegistry
	if err := s.DB.Preload("Provider").Where("id = ?", id).First(&existingModel).Error; err != nil {
		return nil, fmt.Errorf("model %q not found: %w", id, err)
	}

	existingProvider := existingModel.Provider
	if m.ModelProvider != "" {
		var provider StoreProvider
		if err := s.DB.Where("name = ?", m.ModelProvider).First(&provider).Error; err != nil {
			return nil, fmt.Errorf("provider %q not found: %w", m.ModelProvider, err)
		}
		if existingProvider.ID != provider.ID {
			existingProvider = provider
		}
	}

	updates := map[string]interface{}{}
	if m.ModelName != "" {
		updates["model_name"] = m.ModelName
	}
	if m.BaseModel != "" {
		updates["base_model"] = m.BaseModel
	}
	if m.ModelEndpoint != "" {
		updates["model_endpoint"] = m.ModelEndpoint
	}
	if m.ModelProvider != "" {
		updates["provider_id"] = existingProvider.ID
	}
	if len(updates) == 0 {
		return &existingModel, nil
	}
	if err := s.DB.Model(&StoreModelRegistry{}).Where("id = ?", existingModel.ID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update model: %w", err)
	}
	return s.GetModel(id)
}

func (s *Store) DeleteModel(id string) error {
	var model StoreModelRegistry
	if err := s.DB.Where("id = ?", id).First(&model).Error; err != nil {
		return fmt.Errorf("model %q not found: %w", id, err)
	}
	if err := s.DB.Delete(&model).Error; err != nil {
		return fmt.Errorf("cannot delete model %q (may be bound to a backend): %w", id, err)
	}
	return nil
}

func (s *Store) CreateBackend(cfg *core.BackendConfig) (*StoreBackend, error) {
	var backend StoreBackend

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		backendID := uuid.Must(uuid.NewV7()).String()
		backend = StoreBackend{
			ID:             backendID,
			Name:           cfg.Name,
			IsActive:       cfg.IsActive,
			LBType:         cfg.LBtype,
			RequestTimeout: cfg.Request.Timeout,
		}
		if err := tx.Model(&StoreBackend{}).Create(map[string]interface{}{
			"id":              backend.ID,
			"name":            backend.Name,
			"is_active":       backend.IsActive,
			"lb_type":         backend.LBType,
			"request_timeout": backend.RequestTimeout,
		}).Error; err != nil {
			return fmt.Errorf("failed to create backend: %w", err)
		}

		if storeHC, isDrift := DiffHealthCheck(&cfg.HealthCheck, backendID); isDrift {
			if err := tx.Create(storeHC).Error; err != nil {
				return fmt.Errorf("failed to create health check: %w", err)
			}
		}
		if storeTC, isDrift := DiffTransport(&cfg.TransportConfig, backendID); isDrift {
			if err := tx.Create(storeTC).Error; err != nil {
				return fmt.Errorf("failed to create transport config: %w", err)
			}
		}

		for idx, m := range cfg.Models {
			var providerRow StoreProvider
			if err := tx.Where("name = ?", string(m.ModelProvider)).First(&providerRow).Error; err != nil {
				return fmt.Errorf("provider %q not found: %w", m.ModelProvider, err)
			}
			var model StoreModelRegistry
			if err := tx.Where("provider_id = ? AND model_name = ?", providerRow.ID, m.ModelName).First(&model).Error; err != nil {
				return fmt.Errorf("model %s/%s not found: %w", m.ModelProvider, m.ModelName, err)
			}
			if err := tx.Create(&StoreBackendModel{
				BackendID: backendID,
				ModelID:   model.ID,
				Position:  idx,
			}).Error; err != nil {
				return fmt.Errorf("failed to bind model %s/%s: %w", m.ModelProvider, m.ModelName, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := s.DB.Where("id = ?", backend.ID).First(&backend).Error; err != nil {
		return nil, fmt.Errorf("failed to reload backend runtime id: %w", err)
	}
	return &backend, nil
}

func (s *Store) GetBackendByID(id string) (*StoreBackend, error) {
	var backend StoreBackend
	err := s.DB.Preload("Models").Preload("Models.Model").Preload("Models.Model.Provider").Preload("TransportConfig").Preload("HealthCheck").
		Where("id = ?", id).First(&backend).Error
	if err != nil {
		return nil, fmt.Errorf("backend %q not found: %w", id, err)
	}
	return &backend, nil
}

func (s *Store) ListBackends(activeOnly bool) ([]StoreBackend, error) {
	var backends []StoreBackend
	query := s.DB.Preload("Models").Preload("Models.Model").Preload("Models.Model.Provider").Preload("TransportConfig").Preload("HealthCheck")
	if activeOnly {
		query = query.Where("is_active = ?", true)
	}
	if err := query.Find(&backends).Error; err != nil {
		return nil, fmt.Errorf("failed to list backends: %w", err)
	}
	return backends, nil
}

func (s *Store) UpdateBackendByID(id string, cfg *core.BackendConfig) error {
	var backend StoreBackend
	if err := s.DB.Where("id = ?", id).First(&backend).Error; err != nil {
		return fmt.Errorf("backend %q not found: %w", id, err)
	}
	updates := map[string]any{}
	if cfg.LBtype != "" && cfg.LBtype != backend.LBType {
		updates["lb_type"] = cfg.LBtype
	}
	if cfg.Request.Timeout != 0 && cfg.Request.Timeout != backend.RequestTimeout {
		updates["request_timeout"] = cfg.Request.Timeout
	}
	if cfg.IsActive != backend.IsActive {
		updates["is_active"] = cfg.IsActive
	}
	if len(updates) == 0 {
		return nil
	}
	return s.DB.Model(&backend).Updates(updates).Error
}

func (s *Store) UpdateHealthCheck(name string, hc *core.HealthCheckConfig) error {
	var backend StoreBackend
	if err := s.DB.Preload("HealthCheck").Where("name = ?", name).First(&backend).Error; err != nil {
		return fmt.Errorf("backend %q not found: %w", name, err)
	}
	storeHC, isDrift := DiffHealthCheck(hc, backend.ID)
	if isDrift && backend.HealthCheck != nil {
		return s.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Delete(backend.HealthCheck).Error; err != nil {
				return err
			}
			return tx.Create(storeHC).Error
		})
	} else if isDrift && backend.HealthCheck == nil {
		return s.DB.Create(storeHC).Error
	} else if !isDrift && backend.HealthCheck != nil {
		return s.DB.Delete(backend.HealthCheck).Error
	}
	return nil
}

func (s *Store) UpdateTransportConfig(name string, tc *core.TransportSettings) error {
	var backend StoreBackend
	if err := s.DB.Preload("TransportConfig").Where("name = ?", name).First(&backend).Error; err != nil {
		return fmt.Errorf("backend %q not found: %w", name, err)
	}
	storeTC, isDrift := DiffTransport(tc, backend.ID)
	if isDrift && backend.TransportConfig != nil {
		return s.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Delete(backend.TransportConfig).Error; err != nil {
				return err
			}
			return tx.Create(storeTC).Error
		})
	} else if isDrift && backend.TransportConfig == nil {
		return s.DB.Create(storeTC).Error
	} else if !isDrift && backend.TransportConfig != nil {
		return s.DB.Delete(backend.TransportConfig).Error
	}
	return nil
}

func (s *Store) AddBackendModels(backendName string, refs []core.ModelRef) error {
	var backend StoreBackend
	if err := s.DB.Preload("Models").Where("name = ?", backendName).First(&backend).Error; err != nil {
		return fmt.Errorf("backend %q not found: %w", backendName, err)
	}
	existing := make(map[string]bool, len(backend.Models))
	for _, bm := range backend.Models {
		existing[bm.ModelID] = true
	}
	nextPos := len(backend.Models)
	for _, ref := range refs {
		model, err := s.GetModelByProviderAndName(ref.Provider, ref.Name)
		if err != nil {
			return err
		}
		if existing[model.ID] {
			continue
		}
		if err := s.DB.Create(&StoreBackendModel{
			BackendID: backend.ID,
			ModelID:   model.ID,
			Position:  nextPos,
		}).Error; err != nil {
			return fmt.Errorf("failed to bind model %s/%s: %w", ref.Provider, ref.Name, err)
		}
		nextPos++
	}
	return nil
}

func (s *Store) RemoveBackendModels(backendName string, refs []core.ModelRef) error {
	var backend StoreBackend
	if err := s.DB.Where("name = ?", backendName).First(&backend).Error; err != nil {
		return fmt.Errorf("backend %q not found: %w", backendName, err)
	}
	for _, ref := range refs {
		model, err := s.GetModelByProviderAndName(ref.Provider, ref.Name)
		if err != nil {
			return err
		}
		if err := s.DB.Where("backend_id = ? AND model_id = ?", backend.ID, model.ID).
			Delete(&StoreBackendModel{}).Error; err != nil {
			return fmt.Errorf("failed to unbind model %s/%s: %w", ref.Provider, ref.Name, err)
		}
	}
	return nil
}

func (s *Store) DeleteBackendByID(id string) error {
	var backend StoreBackend
	if err := s.DB.Where("id = ?", id).First(&backend).Error; err != nil {
		return fmt.Errorf("backend %q not found: %w", id, err)
	}
	var count int64
	if err := s.DB.Model(&StorePolicy{}).Where("backend_id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check policy references: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("cannot delete backend: %d policy(ies) still reference it", count)
	}
	return s.DB.Delete(&backend).Error
}

func (s *Store) CreatePolicy(cfg *core.RoutePolicy) (*StorePolicy, error) {
	var policy StorePolicy
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var backend StoreBackend
		if err := tx.Where("name = ?", cfg.BackendName).First(&backend).Error; err != nil {
			return fmt.Errorf("backend %q not found: %w", cfg.BackendName, err)
		}

		policy = StorePolicy{
			ID:          uuid.Must(uuid.NewV7()).String(),
			Name:        cfg.Name,
			Priority:    cfg.Priority,
			IsEnabled:   cfg.IsEnabled,
			BackendID:   backend.ID,
			Description: cfg.Description,
		}
		if err := tx.Model(&StorePolicy{}).Create(map[string]interface{}{
			"id":          policy.ID,
			"name":        policy.Name,
			"priority":    policy.Priority,
			"is_enabled":  policy.IsEnabled,
			"backend_id":  policy.BackendID,
			"description": policy.Description,
		}).Error; err != nil {
			return fmt.Errorf("failed to create policy: %w", err)
		}

		for i, c := range cfg.Conditions {
			if err := tx.Create(&StorePolicyCondition{
				PolicyID: policy.ID,
				Type:     c.Type,
				Key:      c.Key,
				Operator: c.Operator,
				Value:    fmt.Sprintf("%v", c.Value),
				Position: i,
			}).Error; err != nil {
				return fmt.Errorf("failed to create condition: %w", err)
			}
		}

		if err := tx.Where("id = ?", policy.ID).First(&policy).Error; err != nil {
			return fmt.Errorf("failed to reload policy runtime id: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &policy, nil
}

func (s *Store) GetPolicyByID(id string) (*StorePolicy, error) {
	var policy StorePolicy
	err := s.DB.Preload("Backend").Preload("Conditions").
		Where("id = ?", id).First(&policy).Error
	if err != nil {
		return nil, fmt.Errorf("policy %q not found: %w", id, err)
	}
	return &policy, nil
}

func (s *Store) ListPolicies(enabledOnly bool) ([]StorePolicy, error) {
	var policies []StorePolicy
	query := s.DB.Preload("Backend").Preload("Conditions")
	if enabledOnly {
		query = query.Where("is_enabled = ?", true)
	}
	if err := query.Find(&policies).Error; err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}
	return policies, nil
}

func (s *Store) UpdatePolicyByID(id string, cfg *core.RoutePolicy) (*StorePolicy, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var policy StorePolicy
		if err := tx.Preload("Conditions").Where("id = ?", id).First(&policy).Error; err != nil {
			return fmt.Errorf("policy %q not found: %w", id, err)
		}
		updates := map[string]any{}
		if cfg.Priority != 0 && cfg.Priority != policy.Priority {
			updates["priority"] = cfg.Priority
		}
		if cfg.IsEnabled != policy.IsEnabled {
			updates["is_enabled"] = cfg.IsEnabled
		}
		if cfg.Description != "" && cfg.Description != policy.Description {
			updates["description"] = cfg.Description
		}
		if cfg.BackendName != "" {
			var backend StoreBackend
			if err := tx.Where("name = ?", cfg.BackendName).First(&backend).Error; err != nil {
				return fmt.Errorf("backend %q not found: %w", cfg.BackendName, err)
			}
			if backend.ID != policy.BackendID {
				updates["backend_id"] = backend.ID
			}
		}
		if len(updates) > 0 {
			if err := tx.Model(&policy).Updates(updates).Error; err != nil {
				return fmt.Errorf("failed to update policy: %w", err)
			}
		}
		if len(cfg.Conditions) > 0 {
			if err := tx.Where("policy_id = ?", policy.ID).Delete(&StorePolicyCondition{}).Error; err != nil {
				return fmt.Errorf("failed to clear conditions: %w", err)
			}
			for i, c := range cfg.Conditions {
				cond := StorePolicyCondition{
					PolicyID: policy.ID,
					Type:     c.Type,
					Key:      c.Key,
					Operator: c.Operator,
					Value:    fmt.Sprintf("%v", c.Value),
					Position: i,
				}
				if err := tx.Create(&cond).Error; err != nil {
					return fmt.Errorf("failed to create condition: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetPolicyByID(id)
}

func (s *Store) DeletePolicyByID(id string) error {
	var policy StorePolicy
	if err := s.DB.Where("id = ?", id).First(&policy).Error; err != nil {
		return fmt.Errorf("policy %q not found: %w", id, err)
	}
	return s.DB.Delete(&policy).Error
}
