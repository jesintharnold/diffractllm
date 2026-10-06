package dbstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"diffractllm/internal/core"
)

func newKeyRow(t *testing.T, s *Store, budgetID string) (*StoreVirtualKey, string) {
	t.Helper()
	created, plaintext, err := s.CreateVirtualKeyTx(&core.VirtualKeyRequest{
		ClientID: "client-1", BudgetID: budgetID,
		Mode: tptr(core.VKDirect), LoadBalancer: tptr(core.LBRoundRobin),
		ProviderConfigs: []core.ProviderConfig{
			{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}},
		},
	})
	require.NoError(t, err)
	row, err := s.GetVirtualKey(created.VirtualKey.ID)
	require.NoError(t, err)
	return row, plaintext
}

// ---------- create ----------

// The plaintext is returned exactly once. It has to validate against the key
// format and resolve through the cache, because that is the only copy anyone
// will ever see.
func TestCreateVirtualKeyReturnsAUsablePlaintext(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)

	row, plaintext := newKeyRow(t, s, budget.ID)

	require.NotEmpty(t, plaintext)
	assert.True(t, core.ValidateKeySignature(plaintext), "the minted key must pass its own format check")
	assert.Equal(t, plaintext, row.APIKey, "a plain read of the row yields the same plaintext")
	assert.NotEmpty(t, row.KeyHash)
	assert.NotEmpty(t, row.DisplayPrefix)
	assert.True(t, row.IsActive)

	// The governance sync keys its cache on this value, so it must survive ToCore.
	vk, err := row.ToCore()
	require.NoError(t, err)
	assert.Equal(t, plaintext, vk.Key)
	assert.Equal(t, budget.ID, vk.BudgetID)
}

// The row on disk is encrypted, so a database dump leaks nothing usable.
func TestVirtualKeyIsEncryptedAtRest(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	row, plaintext := newKeyRow(t, s, budget.ID)

	onDisk := rawColumn(t, s, "virtual_keys", "api_key", row.ID)
	assert.NotEmpty(t, onDisk)
	assert.NotEqual(t, plaintext, onDisk)
	assert.NotContains(t, onDisk, "dk-")
}

func TestCreateVirtualKeyRejectsAnUnknownBudget(t *testing.T) {
	s := store(t)

	_, _, err := s.CreateVirtualKeyTx(&core.VirtualKeyRequest{
		ClientID: "client-1", BudgetID: "no-such-budget",
		Mode: tptr(core.VKDirect), LoadBalancer: tptr(core.LBRoundRobin),
		ProviderConfigs: []core.ProviderConfig{
			{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}},
		},
	})
	assert.Error(t, err)
}

func TestCreateVirtualKeyRejectsBadRouting(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)

	// Weighted mode requires the weights to sum to 1.0.
	_, _, err := s.CreateVirtualKeyTx(&core.VirtualKeyRequest{
		ClientID: "client-1", BudgetID: budget.ID,
		Mode: tptr(core.VKWeighted), LoadBalancer: tptr(core.LBRoundRobin),
		ProviderConfigs: []core.ProviderConfig{
			{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}, Weight: 0.3},
			{Provider: core.ProviderAzure, AllowedModels: []string{"*"}, Weight: 0.3},
		},
	})
	require.Error(t, err)

	rows, err := s.ListVirtualKeys()
	require.NoError(t, err)
	assert.Empty(t, rows, "a rejected create must leave no row behind")
}

// ---------- the list projection ----------

// The admin list must not decrypt. It omits the column entirely, which is why
// AfterFind has to tolerate an empty APIKey rather than trying to decrypt "".
func TestListVirtualKeysWithoutKeysNeverDecrypts(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	row, plaintext := newKeyRow(t, s, budget.ID)

	rows, err := s.ListVirtualKeysWithoutKeys()
	require.NoError(t, err)
	require.Len(t, rows, 1)

	assert.Empty(t, rows[0].APIKey, "the projection drops the column")
	assert.NotEqual(t, plaintext, rows[0].APIKey)
	assert.Equal(t, row.DisplayPrefix, rows[0].DisplayPrefix, "the prefix is what the console shows")
	assert.Equal(t, row.KeyHash, rows[0].KeyHash)
}

// Active keys sort first, so a console list leads with what is usable.
func TestListVirtualKeysWithoutKeysOrdersActiveFirst(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)

	revoked, _ := newKeyRow(t, s, budget.ID)
	require.NoError(t, s.RevokeVirtualKey(revoked.ID))
	active, _ := newKeyRow(t, s, budget.ID)

	rows, err := s.ListVirtualKeysWithoutKeys()
	require.NoError(t, err)
	require.Len(t, rows, 2)

	assert.Equal(t, active.ID, rows[0].ID)
	assert.True(t, rows[0].IsActive)
	assert.False(t, rows[1].IsActive)
}

func TestGetVirtualKeyRedacted(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	row, plaintext := newKeyRow(t, s, budget.ID)

	masked, err := s.GetVirtualKeyRedacted(row.ID)
	require.NoError(t, err)
	assert.Equal(t, SecretMask, masked.APIKey)
	assert.NotEqual(t, plaintext, masked.APIKey)
	assert.Equal(t, row.DisplayPrefix, masked.DisplayPrefix)
}

// ---------- rotate ----------

// Rotation mints a new plaintext and invalidates the old one. The hash moves
// with it, so a stale copy of the key cannot be matched back to the row.
func TestRotateVirtualKey(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	before, oldPlaintext := newKeyRow(t, s, budget.ID)

	rotated, newPlaintext, err := s.RotateVirtualKey(before.ID)
	require.NoError(t, err)

	assert.Equal(t, before.ID, rotated.ID, "rotation keeps the identity")
	assert.NotEqual(t, oldPlaintext, newPlaintext)
	assert.True(t, core.ValidateKeySignature(newPlaintext))
	assert.NotEqual(t, before.KeyHash, rotated.KeyHash, "the old key can no longer be matched")
	assert.NotEqual(t, before.DisplayPrefix, rotated.DisplayPrefix)

	// The store returns the decrypted new key, not the ciphertext - publishing
	// the ciphertext into the cache would file the key under the wrong value.
	assert.Equal(t, newPlaintext, rotated.APIKey)

	fresh, err := s.GetVirtualKey(before.ID)
	require.NoError(t, err)
	assert.Equal(t, newPlaintext, fresh.APIKey)
	assert.True(t, fresh.IsActive)
}

func TestRotateVirtualKeyRefusesARevokedKey(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	row, _ := newKeyRow(t, s, budget.ID)
	require.NoError(t, s.RevokeVirtualKey(row.ID))

	_, _, err := s.RotateVirtualKey(row.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revoked")
}

func TestRotateVirtualKeyNotFound(t *testing.T) {
	_, _, err := store(t).RotateVirtualKey("no-such-key")
	assert.Error(t, err)
}

// ---------- revoke ----------

func TestRevokeVirtualKey(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	row, _ := newKeyRow(t, s, budget.ID)

	require.NoError(t, s.RevokeVirtualKey(row.ID))

	got, err := s.GetVirtualKey(row.ID)
	require.NoError(t, err)
	assert.False(t, got.IsActive)

	active, err := s.ListActiveVirtualKeys()
	require.NoError(t, err)
	assert.Empty(t, active, "a revoked key is out of the active set")

	budgetRow, err := s.GetBudget(budget.ID)
	require.NoError(t, err)
	assert.Equal(t, "released", budgetRow.Status, "the last key is gone, so the budget is free")
}

// A budget carries at most one active key: CreateVirtualKeyTx refuses a budget
// already bound. That is what makes the unconditional release on revoke correct
// - there can never be a second key still billing against it.
func TestBudgetHoldsOneActiveKeyAtATime(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)

	first, _ := newKeyRow(t, s, budget.ID)

	bound, err := s.GetBudget(budget.ID)
	require.NoError(t, err)
	assert.Equal(t, "bound", bound.Status, "creating a key binds the budget")

	_, _, err = s.CreateVirtualKeyTx(&core.VirtualKeyRequest{
		ClientID: "client-2", BudgetID: budget.ID,
		Mode: tptr(core.VKDirect), LoadBalancer: tptr(core.LBRoundRobin),
		ProviderConfigs: []core.ProviderConfig{
			{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already bound")

	// Revoking frees it, and only then can another key take it.
	require.NoError(t, s.RevokeVirtualKey(first.ID))
	released, err := s.GetBudget(budget.ID)
	require.NoError(t, err)
	assert.Equal(t, "released", released.Status)

	_, second := newKeyRow(t, s, budget.ID)
	assert.NotEmpty(t, second, "a released budget is reusable")
}

func TestDeleteVirtualKeyReleasesTheBudget(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	row, _ := newKeyRow(t, s, budget.ID)

	require.NoError(t, s.DeleteVirtualKey(row.ID))

	got, err := s.GetBudget(budget.ID)
	require.NoError(t, err)
	assert.Equal(t, "released", got.Status)

	_, err = s.GetVirtualKey(row.ID)
	assert.Error(t, err)
}

func TestRevokeVirtualKeyNotFound(t *testing.T) {
	assert.Error(t, store(t).RevokeVirtualKey("no-such-key"))
}

// ---------- routing updates ----------

func TestUpdateVirtualKeyRouting(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	row, plaintext := newKeyRow(t, s, budget.ID)

	updated, err := s.UpdateVirtualKeyRouting(row.ID, UpdateVirtualKeyRoutingRequest{
		Mode: tptr(core.VKWeighted),
		ProviderConfigs: []core.ProviderConfig{
			{Provider: core.ProviderOpenAI, AllowedModels: []string{"gpt-4o"}, Weight: 0.5},
			{Provider: core.ProviderAzure, AllowedModels: []string{"*"}, Weight: 0.5},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, core.VKWeightedName, updated.Mode)
	assert.Len(t, updated.ProviderConfigs, 2)
	assert.Equal(t, plaintext, updated.APIKey,
		"a routing change must not disturb the key itself")
}

// A routing change that cannot compile is refused, so an unusable key never
// reaches the cache.
func TestUpdateVirtualKeyRoutingRejectsBadWeights(t *testing.T) {
	s := store(t)
	budget := newBudgetRow(t, s, 1_000_000)
	row, _ := newKeyRow(t, s, budget.ID)

	_, err := s.UpdateVirtualKeyRouting(row.ID, UpdateVirtualKeyRoutingRequest{
		Mode: tptr(core.VKWeighted),
		ProviderConfigs: []core.ProviderConfig{
			{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}, Weight: 0.2},
		},
	})
	require.Error(t, err)

	unchanged, err := s.GetVirtualKey(row.ID)
	require.NoError(t, err)
	assert.Equal(t, core.VKDirectName, unchanged.Mode, "the rejected update changed nothing")
}
