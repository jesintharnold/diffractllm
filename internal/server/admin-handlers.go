package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/dbstore"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const adminMaxBody = 1 << 20

func adminErr(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}

// --------- Budget handlers -------------

func (ds *DiffractLLMServer) getBudget(c *gin.Context) {
	row, err := ds.dbStore.GetBudget(c.Param("id"))
	if err != nil {
		adminErr(c, http.StatusNotFound, "budget not found")
		return
	}
	c.JSON(http.StatusOK, row.ToCore())
}

func (ds *DiffractLLMServer) createBudget(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)
	var payload core.Budget
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	row, err := ds.dbStore.CreateBudget(payload)
	if err != nil {
		ds.logger.Error("create budget", zap.Error(err))
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	ds.governance.BudgetCache.UpsertBudget(row.ToCore())
	c.JSON(http.StatusCreated, row.ToCore())
}

func (ds *DiffractLLMServer) updateBudget(c *gin.Context) {
	id := c.Param("id")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload core.Budget
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	row, err := ds.dbStore.UpdateBudget(id, payload)
	if err != nil {
		ds.logger.Error("update budget", zap.Error(err))
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	ds.governance.BudgetCache.UpsertBudget(row.ToCore())
	c.JSON(http.StatusOK, row.ToCore())
}

func (ds *DiffractLLMServer) deleteBudget(c *gin.Context) {
	id := c.Param("id")
	if err := ds.dbStore.DeleteBudget(id); err != nil {
		adminErr(c, http.StatusConflict, err.Error())
		return
	}
	ds.governance.BudgetCache.DeleteBudget(id)
	c.Status(http.StatusNoContent)
}

func (ds *DiffractLLMServer) listBudgets(c *gin.Context) {
	rows, err := ds.dbStore.ListBudgets()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "listing budgets")
		return
	}
	out := make([]*core.Budget, len(rows))
	for i := range rows {
		out[i] = rows[i].ToCore()
	}
	c.JSON(http.StatusOK, out)
}

// ----------- virtual key ----------------

func (ds *DiffractLLMServer) getVirtualKey(c *gin.Context) {
	row, err := ds.dbStore.GetVirtualKeyRedacted(c.Param("id"))
	if err != nil {
		adminErr(c, http.StatusNotFound, "virtual key not found")
		return
	}
	resp, err := row.ToResponse()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "rendering virtual key")
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (ds *DiffractLLMServer) createVirtualKey(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload core.VirtualKeyRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	result, apiKey, err := ds.dbStore.CreateVirtualKeyTx(&payload)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	vk, err := result.VirtualKey.ToCore()
	if err != nil {
		ds.logger.Error("virtual key to core", zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	ds.governance.KeyCache.UpsertVirtualKey(vk)
	resp, _ := result.VirtualKey.ToResponse()
	c.JSON(http.StatusCreated, gin.H{"key": apiKey, "virtual_key": resp})
}

func (ds *DiffractLLMServer) updateVirtualKeyRouting(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload dbstore.UpdateVirtualKeyRoutingRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	row, err := ds.dbStore.UpdateVirtualKeyRouting(c.Param("id"), payload)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	vk, err := row.ToCore()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	ds.governance.KeyCache.UpsertVirtualKey(vk)

	resp, _ := row.ToResponse()
	c.JSON(http.StatusOK, resp)
}

func (ds *DiffractLLMServer) revokeVirtualKey(c *gin.Context) {
	id := c.Param("id")
	if err := ds.dbStore.RevokeVirtualKey(id); err != nil {
		adminErr(c, http.StatusNotFound, "virtual key not found")
		return
	}
	ds.governance.KeyCache.DeleteVirtualKeyByID(id)
	c.Status(http.StatusNoContent)
}

func (ds *DiffractLLMServer) listVirtualKeys(c *gin.Context) {
	rows, err := ds.dbStore.ListVirtualKeysWithoutKeys()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "listing virtual keys")
		return
	}

	out := make([]core.VirtualKeyResponse, 0, len(rows))
	skipped := make([]string, 0)
	for i := range rows {
		resp, err := rows[i].ToResponse()
		if err != nil {
			ds.logger.Error("virtual key row unreadable",
				zap.String("virtual_key_id", rows[i].ID), zap.Error(err))
			skipped = append(skipped, rows[i].ID)
			continue
		}
		out = append(out, resp)
	}

	c.JSON(http.StatusOK, gin.H{
		"virtual_keys": out,
		"count":        len(out),
		"skipped":      len(skipped),
		"skipped_ids":  skipped,
	})
}

func (ds *DiffractLLMServer) rotateVirtualKey(c *gin.Context) {
	id := c.Param("id")
	row, apiKey, err := ds.dbStore.RotateVirtualKey(id)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	vk, err := row.ToCore()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	ds.governance.KeyCache.DeleteVirtualKeyByID(id)
	ds.governance.KeyCache.UpsertVirtualKey(vk)
	resp, _ := row.ToResponse()
	c.JSON(http.StatusOK, gin.H{"key": apiKey, "virtual_key": resp})
}

// ----------- credentials ----------------

func (ds *DiffractLLMServer) createCredential(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload core.Credential
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	payload.Provider = core.Provider(c.Param("providername"))

	row, err := ds.dbStore.CreateCredential(&payload)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := ds.CredentialPlane.UpsertCredential(row.ToCore()); err != nil {
		ds.logger.Error("credential stored but not published", zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}

	safe, err := ds.dbStore.GetCredentialRedacted(row.ID)
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "created, could not render")
		return
	}
	c.JSON(http.StatusCreated, safe.ToCore())
}

func (ds *DiffractLLMServer) updateCredential(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload core.Credential
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	id := c.Param("id")
	row, err := ds.dbStore.UpdateCredential(id, &payload)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := ds.CredentialPlane.UpsertCredential(row.ToCore()); err != nil {
		ds.logger.Error("credential stored but not published", zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}

	safe, err := ds.dbStore.GetCredentialRedacted(id)
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "updated, could not render")
		return
	}
	c.JSON(http.StatusOK, safe.ToCore())
}

func (ds *DiffractLLMServer) deleteCredential(c *gin.Context) {
	id := c.Param("id")

	row, err := ds.dbStore.GetCredential(id)
	if err != nil {
		adminErr(c, http.StatusNotFound, "credential not found")
		return
	}
	provider := row.ToCore().Provider
	if provider != core.Provider(c.Param("providername")) {
		adminErr(c, http.StatusNotFound, "credential not found")
		return
	}

	if err := ds.dbStore.DeleteCredential(id); err != nil {
		adminErr(c, http.StatusNotFound, err.Error())
		return
	}
	if err := ds.CredentialPlane.RemoveCredential(provider, id); err != nil {
		ds.logger.Error("credential deleted but still live", zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "deleted, still live")
		return
	}
	c.Status(http.StatusNoContent)
}

func (ds *DiffractLLMServer) listCredentials(c *gin.Context) {
	provider := core.Provider(c.Param("providername"))
	rows, err := ds.dbStore.ListCredentialsByProviderRedacted(provider)
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "listing credentials")
		return
	}
	out := make([]*core.Credential, len(rows))
	for i := range rows {
		out[i] = rows[i].ToCore()
	}
	c.JSON(http.StatusOK, out)
}

// ----------- custom pricing ----------------

func (ds *DiffractLLMServer) createCustomPricing(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload core.CustomPricingRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	row, err := ds.dbStore.CreateCustomPricing(payload)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := ds.ModelCatalog.ReloadCustomPricing(); err != nil {
		ds.logger.Error("custom pricing reload", zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	c.JSON(http.StatusCreated, row.ToCore())
}

func (ds *DiffractLLMServer) updateCustomPricing(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload core.Pricing
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	row, err := ds.dbStore.UpdateCustomPricing(c.Param("id"), payload)
	if err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := ds.ModelCatalog.ReloadCustomPricing(); err != nil {
		ds.logger.Error("custom pricing reload", zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	c.JSON(http.StatusOK, row.ToCore())
}

func (ds *DiffractLLMServer) deleteCustomPricing(c *gin.Context) {
	if err := ds.dbStore.DeleteCustomPricing(c.Param("id")); err != nil {
		adminErr(c, http.StatusNotFound, err.Error())
		return
	}

	if err := ds.ModelCatalog.ReloadCustomPricing(); err != nil {
		ds.logger.Error("custom pricing reload", zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "deleted, still live")
		return
	}
	c.Status(http.StatusNoContent)
}

func (ds *DiffractLLMServer) listCustomPricing(c *gin.Context) {
	rows, err := ds.dbStore.ListCustomPricingFiltered(dbstore.CustomPricingFilters{
		ScopeType:    c.Query("scope_type"),
		VirtualKeyID: c.Query("virtual_key_id"),
		Provider:     c.Query("provider"),
		ModelName:    c.Query("model_name"),
	})
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "listing custom pricing")
		return
	}

	out := make([]*core.CustomPricing, len(rows))
	for i := range rows {
		out[i] = rows[i].ToCore()
	}
	c.JSON(http.StatusOK, out)
}

// ---------- simple extra utils -------

func (ds *DiffractLLMServer) handleStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ready":      ds.Status(),
		"governance": ds.governance.Stats(),
		"catalog":    ds.ModelCatalog.Stats(),
	})
}

func (ds *DiffractLLMServer) handleModelCatalog(c *gin.Context) {
	if err := ds.ModelCatalog.SyncNow(); err != nil {
		adminErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "catalog sync triggered"})
}

// ---------- providers ----------

var providerWriteMu sync.Mutex

type updateProviderRequest struct {
	Network       core.NetworkConfig `json:"network_config"`
	Proxy         *core.ProxyConfig  `json:"proxy_config,omitempty"`
	ConfirmUnsafe bool               `json:"confirm_unsafe,omitempty"`
}

func (ds *DiffractLLMServer) annotate(row *dbstore.StoreProvider) {
	provider := core.Provider(row.Name)
	row.AdapterEnabled = ds.ProviderRegistry.Has(provider)
	row.ModelCount = ds.ModelCatalog.ModelCount(provider)
}

func (ds *DiffractLLMServer) listProviders(c *gin.Context) {
	rows, err := ds.dbStore.ListProvidersRedacted()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "listing providers")
		return
	}
	for i := range rows {
		ds.annotate(&rows[i])
	}
	c.JSON(http.StatusOK, rows)
}

func (ds *DiffractLLMServer) getProvider(c *gin.Context) {
	row, err := ds.dbStore.GetProviderRedacted(core.Provider(c.Param("providername")))
	if err != nil {
		adminErr(c, http.StatusNotFound, "provider not found")
		return
	}
	ds.annotate(row)
	c.JSON(http.StatusOK, row)
}

func (ds *DiffractLLMServer) getProviderSettings(c *gin.Context) {
	row, err := ds.dbStore.GetProviderRedacted(core.Provider(c.Param("providername")))
	if err != nil {
		adminErr(c, http.StatusNotFound, "provider not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"network_config": row.Network,
		"proxy_config":   row.Proxy,
	})
}

func (ds *DiffractLLMServer) updateProvider(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var payload updateProviderRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	provider := core.Provider(c.Param("providername"))
	if !ds.ProviderRegistry.Has(provider) {
		adminErr(c, http.StatusBadRequest, "provider has no registered adapter in this build")
		return
	}

	if payload.Network.IsZero() && payload.Proxy == nil {
		adminErr(c, http.StatusBadRequest, "empty settings; PUT replaces the whole config")
		return
	}

	if payload.Network.AllowPrivateNetwork || payload.Network.InsecureSkipVerify {
		if !payload.ConfirmUnsafe {
			adminErr(c, http.StatusBadRequest,
				"allow_private_network and insecure_skip_verify require confirm_unsafe")
			return
		}
	}

	if err := validateProviderSettings(payload); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	providerWriteMu.Lock()
	defer providerWriteMu.Unlock()

	if err := ds.dbStore.UpdateProviderConfig(provider, payload.Network, payload.Proxy); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := ds.dbStore.ListProviders()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "stored, not yet live")
		return
	}
	upstreams := make(map[core.Provider]*core.Upstream, len(rows))
	for i := range rows {
		upstreams[core.Provider(rows[i].Name)] = rows[i].ToUpstream()
	}
	if slices.Contains(ds.Transport.Replace(upstreams), provider) {
		adminErr(c, http.StatusInternalServerError, "stored, still running the previous config")
		return
	}

	row, err := ds.dbStore.GetProviderRedacted(provider)
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "updated, could not render")
		return
	}
	ds.annotate(row)
	c.JSON(http.StatusOK, row)
}

func validateProviderSettings(payload updateProviderRequest) error {
	if payload.Network.RequestTimeout != nil && *payload.Network.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be positive")
	}
	if payload.Network.RetryBackoff != nil && *payload.Network.RetryBackoff <= 0 {
		return fmt.Errorf("retry_backoff must be positive")
	}
	if payload.Network.MaxConnsPerHost != nil && *payload.Network.MaxConnsPerHost <= 0 {
		return fmt.Errorf("max_conns_per_host must be positive")
	}
	if payload.Proxy == nil {
		return nil
	}
	switch payload.Proxy.Type {
	case core.ProxyHTTP, core.ProxySOCKS5:
		if _, err := url.Parse(payload.Proxy.URL); err != nil || payload.Proxy.URL == "" {
			return fmt.Errorf("proxy url is required and must parse for type %q", payload.Proxy.Type)
		}
	case core.ProxyEnvironment:
		if payload.Proxy.URL != "" {
			return fmt.Errorf("proxy url must be empty for type environment")
		}
	default:
		return fmt.Errorf("unknown proxy type %q", payload.Proxy.Type)
	}
	return nil
}

// ---------- models ----------

type modelOption struct {
	ID        string         `json:"id"`
	ModelName string         `json:"model_name"`
	ModelType core.ModelType `json:"model_type"`
}

type catalogEntry struct {
	core.ModelMetadata
	Pricing *core.Pricing `json:"pricing,omitempty"`
}

type catalogPage struct {
	Models []catalogEntry `json:"models"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

func (ds *DiffractLLMServer) runnableProviders() map[core.Provider]struct{} {
	out := make(map[core.Provider]struct{})
	for _, provider := range ds.ProviderRegistry.Providers() {
		out[provider] = struct{}{}
	}
	return out
}

func pageParams(c *gin.Context, def, max int) (limit, offset int) {
	limit = def
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && n > 0 {
		limit = n
	}
	if limit > max {
		limit = max
	}
	if n, err := strconv.Atoi(c.Query("offset")); err == nil && n > 0 {
		offset = n
	}
	return limit, offset
}

func (ds *DiffractLLMServer) listModels(c *gin.Context) {
	entries := ds.ModelCatalog.Models(core.Provider(c.Query("provider")))
	runnable := ds.runnableProviders()

	out := make([]modelOption, 0, len(entries))
	for i := range entries {
		if _, ok := runnable[entries[i].Provider]; !ok {
			continue
		}
		out = append(out, modelOption{
			ID:        entries[i].ID,
			ModelName: entries[i].ModelName,
			ModelType: entries[i].ModelType,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ModelName < out[j].ModelName })
	c.JSON(http.StatusOK, out)
}

func (ds *DiffractLLMServer) listModelCatalog(c *gin.Context) {
	provider := core.Provider(c.Query("provider"))
	if provider == "" {
		adminErr(c, http.StatusBadRequest, "provider is required")
		return
	}
	if !ds.ProviderRegistry.Has(provider) {
		adminErr(c, http.StatusBadRequest, "provider has no registered adapter in this build")
		return
	}

	limit, offset := pageParams(c, 100, 500)

	entries := ds.ModelCatalog.Models(provider)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ModelName < entries[j].ModelName })

	total := len(entries)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	window := entries[offset:end]

	models := make([]catalogEntry, 0, len(window))
	for i := range window {
		models = append(models, catalogEntry{
			ModelMetadata: window[i],
			Pricing:       ds.ModelCatalog.BasePrice(window[i].CatalogKey()),
		})
	}

	c.JSON(http.StatusOK, catalogPage{Models: models, Total: total, Limit: limit, Offset: offset})
}

func (ds *DiffractLLMServer) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (ds *DiffractLLMServer) handleReady(c *gin.Context) {
	checks := gin.H{
		"listening":   ds.Status(),
		"catalog":     ds.ModelCatalog.Ready(),
		"credentials": ds.CredentialPlane.Len() > 0,
		"adapters":    ds.ProviderRegistry.Len() > 0,
	}

	for _, ok := range checks {
		if ok != true {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "checks": checks})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "checks": checks})
}
