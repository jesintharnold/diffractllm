package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/dbstore"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func budgetServer(t *testing.T) (http.Handler, *DiffractLLMServer) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	ds := &DiffractLLMServer{
		logger: zap.NewNop(), config: &config.ServerConfig{},
		dbStore: testStore(t), governance: syncedGovernance(t),
	}
	h, err := ds.routeHandlers()
	require.NoError(t, err)
	return h, ds
}

func postJSON(t *testing.T, h http.Handler, url string, body any) (int, map[string]any) {
	t.Helper()
	code, raw := send(t, h, http.MethodPost, url, body)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out), string(raw))
	return code, out
}

func newBudgetOverHTTP(t *testing.T, h http.Handler, extra map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{
		"name": fmt.Sprintf("handler-budget-%d", time.Now().UnixNano()), "budget_limit": 5_000_000_000, "budget_duration": "1D",
	}
	maps.Copy(body, extra)
	code, out := postJSON(t, h, "/v1/admin/budgets", body)
	require.Equal(t, http.StatusCreated, code, out)
	id := out["id"].(string)
	t.Cleanup(func() { send(t, h, http.MethodDelete, "/v1/admin/budgets/"+id, nil) })
	return out
}

func TestCreateBudgetOverHTTP(t *testing.T) {
	h, _ := budgetServer(t)
	b := newBudgetOverHTTP(t, h, map[string]any{"enforce": false})

	assert.Equal(t, "1D", b["budget_duration"])
	assert.Equal(t, false, b["enforce"])
	assert.Equal(t, "unbound", b["status"])
}

func TestCreateBudgetRejectsBadInput(t *testing.T) {
	h, _ := budgetServer(t)
	existing := newBudgetOverHTTP(t, h, nil)

	tests := map[string]struct {
		body map[string]any
		code int
		want string
	}{
		"duplicate name":     {map[string]any{"name": existing["name"], "budget_limit": 1_000, "budget_duration": "1D"}, http.StatusConflict, "already exists"},
		"a zero window":      {map[string]any{"name": "w0", "budget_limit": 1_000, "budget_duration": "0h"}, http.StatusBadRequest, "between 1 hour and 365 days"},
		"ninety 'minutes'":   {map[string]any{"name": "w90", "budget_limit": 1_000, "budget_duration": "90m"}, http.StatusBadRequest, "between 1 hour and 365 days"},
		"an unparseable one": {map[string]any{"name": "wx", "budget_limit": 1_000, "budget_duration": "whenever"}, http.StatusBadRequest, "budget_duration"},
		"no limit":           {map[string]any{"name": "nolimit", "budget_duration": "1D"}, http.StatusBadRequest, "BudgetLimit"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			code, out := postJSON(t, h, "/v1/admin/budgets", tc.body)
			assert.Equal(t, tc.code, code, out)
			assert.Contains(t, fmt.Sprint(out["error"]), tc.want)
			assert.NotContains(t, fmt.Sprint(out["error"]), "invalid budget:", "the sentinel prefix is not for clients")
		})
	}
}

func TestUpdateBudgetIsPartial(t *testing.T) {
	h, _ := budgetServer(t)
	b := newBudgetOverHTTP(t, h, nil)
	url := "/v1/admin/budgets/" + b["id"].(string)

	code, out := put(t, h, url, map[string]any{"enforce": false})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["enforce"])
	assert.EqualValues(t, 5_000_000_000, out["budget_limit"], "omitted fields stay as they were")
	assert.Equal(t, "1D", out["budget_duration"])

	code, out = put(t, h, url, map[string]any{"name": b["name"], "budget_limit": 9_000_000_000, "budget_duration": "1W"})
	require.Equal(t, http.StatusOK, code, out)
	assert.EqualValues(t, 9_000_000_000, out["budget_limit"])
	assert.Equal(t, "1W", out["budget_duration"])

	for name, payload := range map[string]map[string]any{
		"a rename":      {"name": "another-name"},
		"a negative":    {"budget_limit": -5},
		"a zero window": {"budget_duration": "0h"},
		"90 months":     {"budget_duration": "90m"},
	} {
		code, out = put(t, h, url, payload)
		assert.Equal(t, http.StatusBadRequest, code, name)
		assert.NotEmpty(t, out["error"], name)
	}
}

func TestBudgetMissingIsNotFound(t *testing.T) {
	h, _ := budgetServer(t)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		code, _ := send(t, h, method, "/v1/admin/budgets/no-such-budget", map[string]any{"budget_limit": 5})
		assert.Equal(t, http.StatusNotFound, code, method)
	}
}

func TestBudgetReadsShowLiveSpend(t *testing.T) {
	h, ds := budgetServer(t)
	b := newBudgetOverHTTP(t, h, nil)
	id := b["id"].(string)

	live, ok := ds.governance.BudgetCache.LookupBudget(id)
	require.True(t, ok, "creating a budget loads it into the cache")
	live.RecordUsage(2_500_000_000)

	code, got := get(t, h, "/v1/admin/budgets/"+id)
	require.Equal(t, http.StatusOK, code, got)
	assert.EqualValues(t, 2_500_000_000, got["total_spend"])
	assert.EqualValues(t, 1, got["request_count"])

	row, err := ds.dbStore.GetBudget(id)
	require.NoError(t, err)
	assert.Zero(t, row.TotalCost, "the database has not caught up, which is why the cache is read")

	code, raw := send(t, h, http.MethodGet, "/v1/admin/budgets", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var list []map[string]any
	require.NoError(t, json.Unmarshal(raw, &list))
	var found map[string]any
	for _, item := range list {
		if item["id"] == id {
			found = item
		}
	}
	require.NotNil(t, found, "the list includes the budget")
	assert.EqualValues(t, 2_500_000_000, found["total_spend"], "the list is live too")
}

func TestDeleteBoundBudgetIsAConflict(t *testing.T) {
	h, ds := budgetServer(t)
	b := newBudgetOverHTTP(t, h, nil)
	id := b["id"].(string)
	bind := func(status string) {
		require.NoError(t, ds.dbStore.DB.Model(&dbstore.StoreBudget{}).Where("id = ?", id).Update("status", status).Error)
	}

	bind("bound")
	code, raw := send(t, h, http.MethodDelete, "/v1/admin/budgets/"+id, nil)
	assert.Equal(t, http.StatusConflict, code)
	assert.Contains(t, string(raw), "bound to an active key")

	bind("released")
	code, _ = send(t, h, http.MethodDelete, "/v1/admin/budgets/"+id, nil)
	assert.Equal(t, http.StatusNoContent, code, "a released budget can be deleted")
}
