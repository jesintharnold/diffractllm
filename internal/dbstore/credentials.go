package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StoreCredential struct {
	ID            string            `gorm:"primaryKey;type:text" json:"id"`
	ProviderID    string            `gorm:"not null;type:text;index" json:"provider_id"`
	Provider      StoreProvider     `gorm:"foreignKey:ProviderID;references:ID" json:"provider"`
	Name          string            `gorm:"type:text" json:"name"`
	APIKey        *string           `gorm:"type:text" json:"-"` // encrypted
	Enabled       bool              `gorm:"not null;default:true" json:"enabled"`
	ExpiryAt      *time.Time        `json:"expires_at,omitempty"`
	AllowedModels []string          `gorm:"serializer:json;type:text" json:"allowed_models"`
	BlockedModels []string          `gorm:"serializer:json;type:text" json:"blocked_models"`
	Endpoint      string            `gorm:"type:text" json:"endpoint"`
	Aliases       map[string]string `gorm:"serializer:json;type:text" json:"aliases"`

	AzureAPIVersion   *string  `gorm:"type:text" json:"azure_api_version,omitempty"`
	AzureAuthMode     *string  `gorm:"type:text" json:"azure_auth_mode,omitempty"`
	AzureTenantID     *string  `gorm:"type:text" json:"azure_tenant_id,omitempty"`
	AzureClientID     *string  `gorm:"type:text" json:"azure_client_id,omitempty"`
	AzureClientSecret *string  `gorm:"type:text" json:"-"` // encrypted
	AzureScopes       []string `gorm:"serializer:json;type:text" json:"azure_scopes,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (StoreCredential) TableName() string { return "credentials" }

func (s *StoreCredential) secrets() []**string {
	return []**string{
		&s.APIKey,
		&s.AzureClientSecret,
	}
}

func (s *StoreCredential) BeforeSave(tx *gorm.DB) error {
	encKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	for _, field := range s.secrets() {
		if *field == nil {
			continue
		}
		encrypted, err := encryptKey(*field, encKey)
		if err != nil {
			return fmt.Errorf("error while encrypting credential secret: %w", err)
		}
		*field = encrypted
	}
	return nil
}

func (s *StoreCredential) AfterFind(tx *gorm.DB) error {
	decKey := tx.Statement.Context.Value(aesKeyPass{}).([]byte)
	for _, field := range s.secrets() {
		if *field == nil {
			continue
		}
		decrypted, err := decryptKey(*field, decKey)
		if err != nil {
			return fmt.Errorf("error while decrypting credential secret: %w", err)
		}
		*field = decrypted
	}
	return nil
}

func (s *StoreCredential) ToCore() *core.Credential {
	cred := &core.Credential{
		ID:            s.ID,
		Provider:      core.Provider(s.Provider.Name),
		Name:          s.Name,
		APIKey:        deref(s.APIKey),
		Enabled:       s.Enabled,
		ExpiryAt:      s.ExpiryAt,
		AllowedModels: s.AllowedModels,
		BlockedModels: s.BlockedModels,
		Endpoint:      s.Endpoint,
		Aliases:       s.Aliases,
	}
	if cred.Provider == core.ProviderAzure && s.AzureAuthMode != nil {
		cred.Settings.Azure = &core.AzureSettings{
			AuthMode:     core.AzureAuthMode(deref(s.AzureAuthMode)),
			APIVersion:   deref(s.AzureAPIVersion),
			TenantID:     deref(s.AzureTenantID),
			ClientID:     deref(s.AzureClientID),
			ClientSecret: deref(s.AzureClientSecret),
			Scopes:       s.AzureScopes,
		}
	}
	return cred
}

func newStoreCredential(cred *core.Credential, providerID string, now time.Time) StoreCredential {
	row := StoreCredential{
		ID:            uuid.Must(uuid.NewV7()).String(),
		ProviderID:    providerID,
		Name:          cred.Name,
		APIKey:        optKey(cred.APIKey),
		Enabled:       cred.Enabled,
		ExpiryAt:      cred.ExpiryAt,
		AllowedModels: cred.AllowedModels,
		BlockedModels: cred.BlockedModels,
		Endpoint:      cred.Endpoint,
		Aliases:       cred.Aliases,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	applyAzureSettings(&row, cred)
	return row
}

func applyAzureSettings(row *StoreCredential, cred *core.Credential) {
	azure := cred.Settings.Azure
	if azure == nil {
		return
	}
	row.AzureAuthMode = optKey(string(azure.AuthMode))
	row.AzureAPIVersion = optKey(azure.APIVersion)
	row.AzureTenantID = optKey(azure.TenantID)
	row.AzureClientID = optKey(azure.ClientID)
	row.AzureClientSecret = optKey(azure.ClientSecret)
	row.AzureScopes = azure.Scopes
}

func (s *Store) CreateCredential(cred *core.Credential) (*StoreCredential, error) {
	if err := cred.Validate(); err != nil {
		return nil, fmt.Errorf("invalid credential: %w", err)
	}

	var payload StoreCredential
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		provider, err := s.resolveProvider(tx, cred.Provider)
		if err != nil {
			return err
		}
		payload = newStoreCredential(cred, provider.ID, time.Now())
		return tx.Create(&payload).Error
	})
	if err != nil {
		return nil, fmt.Errorf("create credential for %q: %w", cred.Provider, err)
	}
	return s.GetCredential(payload.ID)
}

func (s *Store) GetCredential(id string) (*StoreCredential, error) {
	var row StoreCredential
	if err := s.DB.Preload("Provider").Where("id = ?", id).First(&row).Error; err != nil {
		return nil, fmt.Errorf("credential %q not found: %w", id, err)
	}
	return &row, nil
}

func (s *Store) ListCredentials() ([]StoreCredential, error) {
	var rows []StoreCredential
	if err := s.DB.Preload("Provider").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list credentials: %w", err)
	}
	return rows, nil
}

func (s *Store) ListCredentialsByProvider(provider core.Provider) ([]StoreCredential, error) {
	var rows []StoreCredential
	if err := s.DB.Preload("Provider").
		Joins("JOIN providers ON providers.id = credentials.provider_id").
		Where("providers.name = ?", string(provider)).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list credentials for %q: %w", provider, err)
	}
	return rows, nil
}

func (s *Store) UpdateCredential(id string, cred *core.Credential) (*StoreCredential, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var existing StoreCredential
		if err := tx.Preload("Provider").Where("id = ?", id).First(&existing).Error; err != nil {
			return fmt.Errorf("credential %q not found: %w", id, err)
		}
		candidate := *cred
		candidate.Provider = core.Provider(existing.Provider.Name)
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("invalid credential: %w", err)
		}

		next := newStoreCredential(&candidate, existing.ProviderID, time.Now())
		next.ID = existing.ID
		next.CreatedAt = existing.CreatedAt

		if err := tx.Model(&next).Select(
			"name", "api_key", "enabled", "expiry_at",
			"allowed_models", "blocked_models", "endpoint", "aliases",
			"azure_api_version", "azure_auth_mode", "azure_tenant_id",
			"azure_client_id", "azure_client_secret", "azure_scopes",
			"updated_at",
		).Updates(&next).Error; err != nil {
			return fmt.Errorf("update credential %q: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetCredential(id)
}

func (s *Store) DeleteCredential(id string) error {
	res := s.DB.Where("id = ?", id).Delete(&StoreCredential{})
	if res.Error != nil {
		return fmt.Errorf("delete credential %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("credential %q not found", id)
	}
	return nil
}
