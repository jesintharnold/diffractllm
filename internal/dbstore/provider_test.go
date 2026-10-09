package dbstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"diffractllm/internal/core"
)

func proxyOf(t *testing.T, s *Store, provider core.Provider) *core.ProxyConfig {
	t.Helper()
	rows, err := s.ListProviders()
	require.NoError(t, err)
	for _, r := range rows {
		if r.Name == string(provider) {
			return r.Proxy
		}
	}
	t.Fatalf("provider %q not found", provider)
	return nil
}

// The console reads the proxy masked; saving that form back must not store the mask.
func TestUpdateProviderConfigKeepsMaskedProxySecrets(t *testing.T) {
	s := store(t)
	t.Cleanup(func() { _ = s.UpdateProviderConfig(core.ProviderOpenAI, core.NetworkConfig{}, nil) })
	proxy := func(user, pass string) *core.ProxyConfig {
		return &core.ProxyConfig{Type: core.ProxyHTTP, URL: "http://proxy.test:3128", Username: user, Password: pass}
	}
	require.NoError(t, s.UpdateProviderConfig(core.ProviderOpenAI, core.NetworkConfig{}, proxy("alice", "s3cret")))

	require.NoError(t, s.UpdateProviderConfig(core.ProviderOpenAI, core.NetworkConfig{}, proxy(SecretMask, SecretMask)))
	got := proxyOf(t, s, core.ProviderOpenAI)
	require.NotNil(t, got)
	assert.Equal(t, "alice", got.Username)
	assert.Equal(t, "s3cret", got.Password)

	require.NoError(t, s.UpdateProviderConfig(core.ProviderOpenAI, core.NetworkConfig{}, proxy("bob", "n3w")))
	got = proxyOf(t, s, core.ProviderOpenAI)
	assert.Equal(t, "bob", got.Username)
	assert.Equal(t, "n3w", got.Password)
}
