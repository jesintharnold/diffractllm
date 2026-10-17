package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// keyServer is the real route tree over a store holding one virtual key and its budget.
func keyServer(t *testing.T) (http.Handler, *dbstore.Store, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	store := testStore(t)
	budget, err := store.CreateBudget(core.Budget{
		Name: fmt.Sprintf("vk-handler-%d", time.Now().UnixNano()), BudgetLimit: 1_000_000, BudgetDuration: "1D",
	})
	require.NoError(t, err)
	mode, lb := core.VKDirect, core.LBRoundRobin
	created, _, err := store.CreateVirtualKeyTx(&core.VirtualKeyRequest{
		ClientID: "handler-test", BudgetID: budget.ID, Mode: &mode, LoadBalancer: &lb,
		ProviderConfigs: []core.ProviderConfig{{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}}},
	})
	require.NoError(t, err)
	keyID := created.VirtualKey.ID
	t.Cleanup(func() {
		_ = store.DeleteVirtualKey(keyID)
		_ = store.DeleteBudget(budget.ID)
	})

	ds := &DiffractLLMServer{
		logger: zap.NewNop(), config: &config.ServerConfig{},
		dbStore: store, governance: syncedGovernance(t),
	}
	h, err := ds.routeHandlers()
	require.NoError(t, err)
	return h, store, keyID, budget.ID
}

func TestVirtualKeyExpiryCanBeSetAndCleared(t *testing.T) {
	h, _, id, _ := keyServer(t)
	url := "/v1/admin/virtual-keys/" + id + "/expiry"

	at := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	code, body := put(t, h, url, map[string]any{"expires_at": at})
	require.Equal(t, http.StatusOK, code, body)
	assert.NotEmpty(t, body["expires_at"])

	code, body = put(t, h, url, map[string]any{"clear_expiry": true})
	require.Equal(t, http.StatusOK, code, body)
	_, present := body["expires_at"]
	assert.False(t, present, "a key with no expiry omits the field")
}

func TestVirtualKeyExpiryRejectsBadRequests(t *testing.T) {
	h, _, id, _ := keyServer(t)
	url := "/v1/admin/virtual-keys/" + id + "/expiry"
	future := time.Now().Add(time.Hour).UTC()

	for name, payload := range map[string]map[string]any{
		"neither":      {},
		"both":         {"expires_at": future, "clear_expiry": true},
		"in the past":  {"expires_at": time.Now().Add(-time.Hour).UTC()},
		"not a date":   {"expires_at": "tomorrow"},
		"unset, false": {"clear_expiry": false},
	} {
		code, body := put(t, h, url, payload)
		assert.Equal(t, http.StatusBadRequest, code, name)
		assert.NotEmpty(t, body["error"], name)
	}

	code, _ := put(t, h, "/v1/admin/virtual-keys/no-such-key/expiry", map[string]any{"clear_expiry": true})
	assert.Equal(t, http.StatusNotFound, code)
}

// Delete removes the key and frees its budget; it is not a disable.
func TestDeleteVirtualKeyRemovesItAndReleasesTheBudget(t *testing.T) {
	h, store, id, budgetID := keyServer(t)

	code, raw := send(t, h, http.MethodDelete, "/v1/admin/virtual-keys/"+id, nil)
	require.Equal(t, http.StatusNoContent, code, string(raw))

	code, _ = send(t, h, http.MethodGet, "/v1/admin/virtual-keys/"+id, nil)
	assert.Equal(t, http.StatusNotFound, code)

	budget, err := store.GetBudget(budgetID)
	require.NoError(t, err)
	assert.Equal(t, "released", budget.Status)

	code, _ = send(t, h, http.MethodDelete, "/v1/admin/virtual-keys/"+id, nil)
	assert.Equal(t, http.StatusNotFound, code, "deleting twice is a not-found")
}
