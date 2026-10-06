package dbstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"diffractllm/internal/core"
)

const plainAPIKey = "sk-live-not-a-real-key-000111222"

func newCredential(t *testing.T, s *Store, provider core.Provider, opts ...func(*core.Credential)) *StoreCredential {
	t.Helper()
	cred := &core.Credential{
		Provider: provider, Name: "cred-" + string(provider), APIKey: plainAPIKey,
		Enabled: true, Endpoint: "https://api.example.test",
		AllowedModels: []string{"gpt-4o"},
	}
	for _, opt := range opts {
		opt(cred)
	}
	row, err := s.CreateCredential(cred)
	require.NoError(t, err)
	return row
}

// ---------- encryption at rest ----------

// The only way to prove encryption is to read the column with plain SQL, so no
// AfterFind hook runs and nothing is decrypted on the way out.
func TestCredentialAPIKeyIsEncryptedAtRest(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderOpenAI)

	onDisk := rawColumn(t, s, "credentials", "api_key", row.ID)
	assert.NotEmpty(t, onDisk)
	assert.NotEqual(t, plainAPIKey, onDisk, "the plaintext key must not be on disk")
	assert.NotContains(t, onDisk, "sk-live", "not even a recognisable prefix")

	got, err := s.GetCredential(row.ID)
	require.NoError(t, err)
	require.NotNil(t, got.APIKey)
	assert.Equal(t, plainAPIKey, *got.APIKey, "a plain read decrypts it")
}

// Azure's client secret goes through the same secrets() list, so it must be
// encrypted too - it is a credential in every sense.
func TestAzureClientSecretIsEncryptedAtRest(t *testing.T) {
	s := store(t)
	const secret = "azure-client-secret-abc123"

	row := newCredential(t, s, core.ProviderAzure, func(c *core.Credential) {
		c.Settings = core.CredentialSettings{Azure: &core.AzureSettings{
			AuthMode: core.AzureAuthServicePrincipal,
			TenantID: "tenant-1", ClientID: "client-1", ClientSecret: secret,
		}}
	})

	onDisk := rawColumn(t, s, "credentials", "azure_client_secret", row.ID)
	assert.NotEqual(t, secret, onDisk)
	assert.NotContains(t, onDisk, "azure-client-secret")

	got, err := s.GetCredential(row.ID)
	require.NoError(t, err)
	require.NotNil(t, got.AzureClientSecret)
	assert.Equal(t, secret, *got.AzureClientSecret)
}

// Two credentials with the same plaintext must not produce the same ciphertext,
// or the column leaks which keys are shared.
func TestCredentialCiphertextIsNotDeterministic(t *testing.T) {
	s := store(t)

	first := newCredential(t, s, core.ProviderOpenAI)
	second := newCredential(t, s, core.ProviderOpenAI, func(c *core.Credential) {
		c.Name = "second"
	})

	a := rawColumn(t, s, "credentials", "api_key", first.ID)
	b := rawColumn(t, s, "credentials", "api_key", second.ID)
	assert.NotEqual(t, a, b, "the nonce must make identical plaintext encrypt differently")
}

// ---------- redacted reads ----------

// The redacted read is what admin GETs use. It must never decrypt, and it must
// never return an empty string, because empty is indistinguishable from unset
// and a GET then PUT round trip would blank the real secret.
func TestRedactedReadsNeverDecrypt(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderAzure, func(c *core.Credential) {
		c.Settings = core.CredentialSettings{Azure: &core.AzureSettings{
			AuthMode: core.AzureAuthServicePrincipal,
			TenantID: "tenant-1", ClientID: "client-1", ClientSecret: "azure-secret",
		}}
	})

	masked, err := s.GetCredentialRedacted(row.ID)
	require.NoError(t, err)

	require.NotNil(t, masked.APIKey)
	assert.Equal(t, SecretMask, *masked.APIKey)
	require.NotNil(t, masked.AzureClientSecret)
	assert.Equal(t, SecretMask, *masked.AzureClientSecret)
	assert.NotEmpty(t, SecretMask, "the mask is never the empty string")

	// Everything else still comes through.
	assert.Equal(t, row.Name, masked.Name)
	assert.Equal(t, row.Endpoint, masked.Endpoint)
	assert.True(t, masked.Enabled)
}

func TestRedactedListNeverDecrypts(t *testing.T) {
	s := store(t)
	newCredential(t, s, core.ProviderOpenAI)

	rows, err := s.ListCredentialsByProviderRedacted(core.ProviderOpenAI)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NotNil(t, rows[0].APIKey)
	assert.Equal(t, SecretMask, *rows[0].APIKey)
}

// A plain read and a redacted read of the same row must not interfere: the mask
// is a property of the read, not something written back.
func TestRedactedReadDoesNotPoisonThePlainRead(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderOpenAI)

	masked, err := s.GetCredentialRedacted(row.ID)
	require.NoError(t, err)
	assert.Equal(t, SecretMask, *masked.APIKey)

	plain, err := s.GetCredential(row.ID)
	require.NoError(t, err)
	assert.Equal(t, plainAPIKey, *plain.APIKey, "the row on disk is untouched")

	assert.NotEqual(t, SecretMask, rawColumn(t, s, "credentials", "api_key", row.ID),
		"the mask must never be persisted")
}

// ---------- update round trip ----------

// An update re-encrypts, so the secret must still decrypt afterwards and the
// ciphertext must have changed.
func TestUpdateCredentialReEncrypts(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderOpenAI)
	before := rawColumn(t, s, "credentials", "api_key", row.ID)

	const rotated = "sk-live-rotated-333444555"
	_, err := s.UpdateCredential(row.ID, UpdateCredentialRequest{
		Name: tptr("renamed"), APIKey: tptr(rotated),
		AllowedModels: &[]string{"gpt-4o", "gpt-4o-mini"},
	})
	require.NoError(t, err)

	after := rawColumn(t, s, "credentials", "api_key", row.ID)
	assert.NotEqual(t, before, after)
	assert.NotEqual(t, rotated, after, "still encrypted after the update")

	got, err := s.GetCredential(row.ID)
	require.NoError(t, err)
	assert.Equal(t, rotated, *got.APIKey)
	assert.Equal(t, "renamed", got.Name)
	assert.Len(t, got.AllowedModels, 2)
}

// ---------- ToCore and validation ----------

// ToCore is what the provider plane consumes, so the decrypted key has to land
// on core.Credential rather than staying in the row.
func TestCredentialToCore(t *testing.T) {
	s := store(t)
	expiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	row := newCredential(t, s, core.ProviderOpenAI, func(c *core.Credential) {
		c.ExpiryAt = &expiry
		c.BlockedModels = []string{"o1"}
	})

	got, err := s.GetCredential(row.ID)
	require.NoError(t, err)

	cred := got.ToCore()
	assert.Equal(t, core.ProviderOpenAI, cred.Provider)
	assert.Equal(t, plainAPIKey, cred.APIKey, "the plane needs the real key")
	assert.True(t, cred.Enabled)
	assert.Equal(t, []string{"gpt-4o"}, cred.AllowedModels)
	assert.Equal(t, []string{"o1"}, cred.BlockedModels)
	require.NotNil(t, cred.ExpiryAt)
	assert.Equal(t, expiry, cred.ExpiryAt.UTC())

	assert.True(t, cred.CheckValidity())
	assert.True(t, cred.CheckModel("gpt-4o"))
	assert.False(t, cred.CheckModel("o1"), "the blocklist survives the round trip")
}

func TestCreateCredentialRejectsInvalid(t *testing.T) {
	s := store(t)

	tests := []struct {
		name string
		give *core.Credential
	}{
		{
			name: "no endpoint",
			give: &core.Credential{
				Provider: core.ProviderOpenAI, Name: "x", APIKey: plainAPIKey,
				AllowedModels: []string{"gpt-4o"},
			},
		},
		{
			name: "no allowed models",
			give: &core.Credential{
				Provider: core.ProviderOpenAI, Name: "x", APIKey: plainAPIKey,
				Endpoint: "https://api.example.test",
			},
		},
		{
			name: "unknown provider",
			give: &core.Credential{
				Provider: "not-a-provider", Name: "x", APIKey: plainAPIKey,
				Endpoint: "https://api.example.test", AllowedModels: []string{"gpt-4o"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.CreateCredential(tc.give)
			assert.Error(t, err)
		})
	}
}

func TestDeleteCredential(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderOpenAI)

	require.NoError(t, s.DeleteCredential(row.ID))

	_, err := s.GetCredential(row.ID)
	assert.Error(t, err)
}

// ---------- the key must survive an unrelated update ----------

// api_key sits in the update's Select list, so an omitted one would be written
// as empty and erase the secret. Omitted means keep. Wiping it leaves the
// credential loadable but unable to authenticate, which surfaces far from the cause.
func TestUpdateCredentialKeepsTheKeyWhenOmitted(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderOpenAI)

	_, err := s.UpdateCredential(row.ID, UpdateCredentialRequest{Name: tptr("renamed")})
	require.NoError(t, err)

	got, err := s.GetCredential(row.ID)
	require.NoError(t, err)
	require.NotNil(t, got.APIKey, "omitting api_key erased the secret")
	assert.Equal(t, plainAPIKey, *got.APIKey)
	assert.Equal(t, "renamed", got.Name)
}

// A console renders the masked value; sending it back must not store asterisks.
func TestUpdateCredentialRefusesTheMaskedValue(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderOpenAI)

	_, err := s.UpdateCredential(row.ID, UpdateCredentialRequest{APIKey: tptr(SecretMask)})
	require.NoError(t, err)

	got, err := s.GetCredential(row.ID)
	require.NoError(t, err)
	require.NotNil(t, got.APIKey)
	assert.Equal(t, plainAPIKey, *got.APIKey, "the mask was stored as the key")
}

// enabled is a bool, so before the request became pointers an update that did not
// mention it wrote false and silently took the credential out of rotation.
func TestUpdateCredentialKeepsEveryOmittedField(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderAzure, func(c *core.Credential) {
		c.Enabled = true
		c.BlockedModels = []string{"dall-e-3"}
		c.Aliases = map[string]core.Alias{
			"gpt-4o": {ModelID: "gpt4o-deploy", RouteStyle: core.AzureRouteDeployment, APIVersion: "2025-04-01-preview"},
		}
		c.Settings.Azure = &core.AzureSettings{AuthMode: core.AzureAuthKeyMode}
	})

	// Rename only. Everything else must survive.
	_, err := s.UpdateCredential(row.ID, UpdateCredentialRequest{Name: tptr("renamed")})
	require.NoError(t, err)

	got, err := s.GetCredential(row.ID)
	require.NoError(t, err)

	assert.Equal(t, "renamed", got.Name)
	assert.True(t, got.Enabled, "omitting enabled disabled the credential")
	assert.Equal(t, []string{"dall-e-3"}, got.BlockedModels, "blocked models were erased")
	assert.Equal(t, "https://api.example.test", got.Endpoint, "endpoint was erased")
	assert.Len(t, got.Aliases, 1, "aliases were erased")
	assert.Equal(t, "gpt4o-deploy", got.Aliases["gpt-4o"].ModelID)
	require.NotNil(t, got.AzureAuthMode, "azure settings were erased")
	assert.Equal(t, string(core.AzureAuthKeyMode), *got.AzureAuthMode)
	require.NotNil(t, got.APIKey)
	assert.Equal(t, plainAPIKey, *got.APIKey)
}

// Sending a field explicitly still changes it.
func TestUpdateCredentialAppliesWhatIsSent(t *testing.T) {
	s := store(t)
	row := newCredential(t, s, core.ProviderOpenAI)

	_, err := s.UpdateCredential(row.ID, UpdateCredentialRequest{
		Enabled:       tptr(false),
		BlockedModels: &[]string{"o3-pro"},
	})
	require.NoError(t, err)

	got, err := s.GetCredential(row.ID)
	require.NoError(t, err)
	assert.False(t, got.Enabled)
	assert.Equal(t, []string{"o3-pro"}, got.BlockedModels)
}
