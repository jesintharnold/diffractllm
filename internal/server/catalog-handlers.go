package server

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"diffractllm/internal/core"
	"diffractllm/internal/modelcatalog"

	"github.com/gin-gonic/gin"
)

func (ds *DiffractLLMServer) configuredProviders() ([]core.Provider, error) {
	rows, err := ds.dbStore.ListProvidersRedacted()
	if err != nil {
		return nil, err
	}
	out := make([]core.Provider, 0, len(rows))
	for i := range rows {
		provider := core.Provider(rows[i].Name)
		if ds.ProviderRegistry.Has(provider) && len(ds.CredentialPlane.Credentials(provider)) > 0 {
			out = append(out, provider)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

type catalogSummary struct {
	Providers    []core.Provider       `json:"providers"`
	PerProvider  map[core.Provider]int `json:"provider_models"` // same counting as Models
	Models       int                   `json:"models"`
	Priced       int                   `json:"priced"`
	Unpriced     int                   `json:"unpriced"`
	Syncing      bool                  `json:"syncing"`
	LastRunAt    time.Time             `json:"last_run_at,omitzero"` // start of the latest run, ok or not
	LastSyncAt   time.Time             `json:"last_sync_at,omitzero"`
	LastError    string                `json:"last_error,omitempty"`
	SyncInterval float64               `json:"sync_interval_seconds"`
}

func (ds *DiffractLLMServer) getCatalogSummary(c *gin.Context) {
	providers, err := ds.configuredProviders()
	if err != nil {
		adminErr(c, http.StatusInternalServerError, "listing providers")
		return
	}
	s := catalogSummary{Providers: providers, PerProvider: make(map[core.Provider]int, len(providers))}
	for _, provider := range providers {
		for _, m := range ds.ModelCatalog.Models(provider) {
			s.Models++
			s.PerProvider[provider]++
			if ds.ModelCatalog.BasePrice(m.CatalogKey()) != nil {
				s.Priced++
			}
		}
	}
	s.Unpriced = s.Models - s.Priced
	for _, job := range ds.ModelCatalog.Stats() {
		if job.Name == modelcatalog.JobCatalogSync {
			s.Syncing = job.Running
			s.LastRunAt = job.LastRunAt
			s.LastSyncAt = job.LastSuccessAt
			s.LastError = job.LastError
			s.SyncInterval = job.Interval.Seconds()
		}
	}
	if s.LastSyncAt.IsZero() { // no sync since this process started
		if at, err := ds.dbStore.LatestModelSync(); err == nil {
			s.LastSyncAt = at.UTC()
		}
	}
	c.JSON(http.StatusOK, s)
}

func (ds *DiffractLLMServer) getCatalogSettings(c *gin.Context) {
	c.JSON(http.StatusOK, ds.ModelCatalog.Settings())
}

func (ds *DiffractLLMServer) putCatalogSettings(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody)

	var next modelcatalog.Settings
	if err := c.ShouldBindJSON(&next); err != nil {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := ds.ModelCatalog.UpdateSettings(next)
	if errors.Is(err, modelcatalog.ErrInvalidSettings) {
		adminErr(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		adminErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, saved)
}
