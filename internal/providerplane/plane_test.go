package providerplane

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"diffractllm/internal/core"
)

// Builds a credential that passes Validate(). Each test overrides what it cares
// about through the option funcs.
func cred(id string, provider core.Provider, opts ...func(*core.Credential)) *core.Credential {
	c := &core.Credential{
		ID:            id,
		Provider:      provider,
		Name:          id,
		APIKey:        "sk-test",
		Enabled:       true,
		Endpoint:      "https://example.test",
		AllowedModels: []string{"gpt-4o"},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func disabled(c *core.Credential) { c.Enabled = false }

func expired(c *core.Credential) {
	past := time.Now().Add(-time.Hour)
	c.ExpiryAt = &past
}

func expiresLater(c *core.Credential) {
	future := time.Now().Add(time.Hour)
	c.ExpiryAt = &future
}

func noEndpoint(c *core.Credential) { c.Endpoint = "" }

func allow(models ...string) func(*core.Credential) {
	return func(c *core.Credential) { c.AllowedModels = models }
}

func block(models ...string) func(*core.Credential) {
	return func(c *core.Credential) { c.BlockedModels = models }
}

// nil rather than an empty slice, so assert.Equal against a nil want works.
func ids(creds []*core.Credential) []string {
	if len(creds) == 0 {
		return nil
	}
	out := make([]string, 0, len(creds))
	for _, c := range creds {
		out = append(out, c.ID)
	}
	return out
}

const gpt4o = "gpt-4o"

func TestCandidatesFilters(t *testing.T) {
	key := core.CatalogKey{Provider: core.ProviderOpenAI, ModelName: gpt4o}

	tests := []struct {
		name  string
		creds []*core.Credential
		key   core.CatalogKey
		want  []string
	}{
		{
			name:  "valid and allowed is returned",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI)},
			key:   key,
			want:  []string{"a"},
		},
		{
			name:  "disabled is dropped",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, disabled)},
			key:   key,
		},
		{
			name:  "expired is dropped",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, expired)},
			key:   key,
		},
		{
			name:  "future expiry is kept",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, expiresLater)},
			key:   key,
			want:  []string{"a"},
		},
		{
			name:  "model not in allowed list is dropped",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, allow("gpt-4o-mini"))},
			key:   key,
		},
		{
			name:  "wildcard allow matches any model",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, allow("*"))},
			key:   key,
			want:  []string{"a"},
		},
		{
			name:  "blocked model beats wildcard allow",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, allow("*"), block(gpt4o))},
			key:   key,
		},
		{
			name:  "blocked model beats explicit allow",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, allow(gpt4o), block(gpt4o))},
			key:   key,
		},
		{
			name:  "wildcard block drops everything",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, allow("*"), block("*"))},
			key:   key,
		},
		{
			name:  "other provider is not returned",
			creds: []*core.Credential{cred("a", core.ProviderAnthropic)},
			key:   key,
		},
		{
			name: "only the matching credentials survive, in insertion order",
			creds: []*core.Credential{
				cred("keep-1", core.ProviderOpenAI),
				cred("drop-disabled", core.ProviderOpenAI, disabled),
				cred("keep-2", core.ProviderOpenAI, allow("*")),
				cred("drop-model", core.ProviderOpenAI, allow("claude")),
				cred("drop-provider", core.ProviderAnthropic),
			},
			key:  key,
			want: []string{"keep-1", "keep-2"},
		},
		{
			name:  "unknown provider returns nothing",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI)},
			key:   core.CatalogKey{Provider: core.ProviderCohere, ModelName: gpt4o},
		},
		{
			name:  "empty model name matches nothing",
			creds: []*core.Credential{cred("a", core.ProviderOpenAI, allow("*"))},
			key:   core.CatalogKey{Provider: core.ProviderOpenAI},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plane := NewProviderPlane(tc.creds)
			assert.Equal(t, tc.want, ids(plane.Candidates(tc.key)))
		})
	}
}

// Credentials is the raw bucket. It deliberately does not apply CheckValidity,
// which is why it cannot stand in for a "usable credentials" count.
func TestCredentialsDoesNotFilter(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{
		cred("valid", core.ProviderOpenAI),
		cred("disabled", core.ProviderOpenAI, disabled),
		cred("expired", core.ProviderOpenAI, expired),
		cred("other-model", core.ProviderOpenAI, allow("claude")),
	})

	assert.Equal(t,
		[]string{"valid", "disabled", "expired", "other-model"},
		ids(plane.Credentials(core.ProviderOpenAI)),
		"Credentials must return the raw bucket")

	assert.Len(t, plane.Candidates(core.CatalogKey{Provider: core.ProviderOpenAI, ModelName: gpt4o}), 1,
		"Candidates must filter where Credentials does not")
}

func TestCredentialsUnknownProvider(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})
	assert.Empty(t, plane.Credentials(core.ProviderCohere))
}

func TestEmptyPlane(t *testing.T) {
	plane := NewProviderPlane(nil)

	assert.Empty(t, plane.Candidates(core.CatalogKey{Provider: core.ProviderOpenAI, ModelName: gpt4o}))
	assert.Empty(t, plane.Credentials(core.ProviderOpenAI))
}

func TestUpsertCredentialAdds(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})

	require.NoError(t, plane.UpsertCredential(cred("b", core.ProviderOpenAI)))
	assert.Equal(t, []string{"a", "b"}, ids(plane.Credentials(core.ProviderOpenAI)))
}

func TestUpsertCredentialReplacesByID(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{
		cred("a", core.ProviderOpenAI),
		cred("b", core.ProviderOpenAI),
	})

	updated := cred("a", core.ProviderOpenAI, allow("claude"))
	require.NoError(t, plane.UpsertCredential(updated))

	got := plane.Credentials(core.ProviderOpenAI)
	require.Equal(t, []string{"a", "b"}, ids(got), "replace must not reorder or duplicate")
	assert.Same(t, updated, got[0], "slot a must hold the replacement")
}

func TestUpsertCredentialRejectsInvalid(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})

	err := plane.UpsertCredential(cred("b", core.ProviderOpenAI, noEndpoint))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint")

	assert.Equal(t, []string{"a"}, ids(plane.Credentials(core.ProviderOpenAI)),
		"a rejected upsert must not publish")
}

// The plane is read lock-free through an atomic pointer, so a writer must
// publish a new snapshot rather than mutate the one readers already hold.
func TestUpsertDoesNotMutatePublishedSnapshot(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})

	held := plane.Credentials(core.ProviderOpenAI)
	require.Len(t, held, 1)

	require.NoError(t, plane.UpsertCredential(cred("b", core.ProviderOpenAI)))

	assert.Equal(t, []string{"a"}, ids(held), "the slice a reader already held must not change")
	assert.Len(t, plane.Credentials(core.ProviderOpenAI), 2)
}

// Holding the slice is not enough: a writer builds a fresh slice either way.
// The map itself has to be cloned, so read the old snapshot's map entry.
func TestUpsertClonesTheMap(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})
	old := plane.credSnapshot.Load()

	require.NoError(t, plane.UpsertCredential(cred("b", core.ProviderOpenAI)))

	assert.Equal(t, []string{"a"}, ids(old.providerCredentials[core.ProviderOpenAI]),
		"the previous snapshot's map entry must not change")
	assert.NotSame(t, old, plane.credSnapshot.Load(), "a new snapshot must be published")
}

func TestRemoveClonesTheMap(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{
		cred("a", core.ProviderOpenAI),
		cred("b", core.ProviderOpenAI),
	})
	old := plane.credSnapshot.Load()

	require.NoError(t, plane.RemoveCredential(core.ProviderOpenAI, "a"))

	assert.Equal(t, []string{"a", "b"}, ids(old.providerCredentials[core.ProviderOpenAI]),
		"the previous snapshot's map entry must not change")
}

func TestUpsertCredentialNewProvider(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})

	require.NoError(t, plane.UpsertCredential(cred("b", core.ProviderAnthropic)))

	assert.Equal(t, []string{"a"}, ids(plane.Credentials(core.ProviderOpenAI)))
	assert.Equal(t, []string{"b"}, ids(plane.Credentials(core.ProviderAnthropic)))
}

func TestRemoveCredential(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{
		cred("a", core.ProviderOpenAI),
		cred("b", core.ProviderOpenAI),
	})

	require.NoError(t, plane.RemoveCredential(core.ProviderOpenAI, "a"))
	assert.Equal(t, []string{"b"}, ids(plane.Credentials(core.ProviderOpenAI)))
}

func TestRemoveCredentialMissing(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})

	require.ErrorIs(t, plane.RemoveCredential(core.ProviderOpenAI, "nope"), ErrNoCredential)
	assert.Equal(t, []string{"a"}, ids(plane.Credentials(core.ProviderOpenAI)),
		"a failed remove must change nothing")
}

func TestRemoveCredentialUnknownProvider(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})

	assert.ErrorIs(t, plane.RemoveCredential(core.ProviderCohere, "a"), ErrNoCredential)
}

// Removing the last credential drops the bucket instead of leaving an empty
// slice behind.
func TestRemoveLastCredentialDropsBucket(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})

	require.NoError(t, plane.RemoveCredential(core.ProviderOpenAI, "a"))

	assert.NotContains(t, plane.credSnapshot.Load().providerCredentials, core.ProviderOpenAI,
		"the map key must be deleted once the bucket is empty")
}

func TestRemoveDoesNotMutatePublishedSnapshot(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{
		cred("a", core.ProviderOpenAI),
		cred("b", core.ProviderOpenAI),
	})

	held := plane.Credentials(core.ProviderOpenAI)
	require.NoError(t, plane.RemoveCredential(core.ProviderOpenAI, "a"))

	assert.Equal(t, []string{"a", "b"}, ids(held))
}

// Replace is a wholesale swap, not a merge: a provider absent from the argument
// loses every credential it had.
func TestReplaceIsWholesale(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{
		cred("a", core.ProviderOpenAI),
		cred("b", core.ProviderAnthropic),
	})

	plane.Replace([]*core.Credential{cred("c", core.ProviderOpenAI)})

	assert.Equal(t, []string{"c"}, ids(plane.Credentials(core.ProviderOpenAI)))
	assert.Empty(t, plane.Credentials(core.ProviderAnthropic), "Replace drops absent providers")
}

func TestReplaceWithNothingEmptiesThePlane(t *testing.T) {
	plane := NewProviderPlane([]*core.Credential{cred("a", core.ProviderOpenAI)})

	plane.Replace(nil)

	assert.Empty(t, plane.Credentials(core.ProviderOpenAI))
}

// Replace does not call Validate, unlike UpsertCredential. It is the boot and
// resync path, and the store is the thing that validated those rows.
func TestReplaceSkipsValidation(t *testing.T) {
	plane := NewProviderPlane(nil)

	plane.Replace([]*core.Credential{cred("a", core.ProviderOpenAI, noEndpoint)})

	assert.Equal(t, []string{"a"}, ids(plane.Credentials(core.ProviderOpenAI)))
}
