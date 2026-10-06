package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	metricsengine "diffractllm/internal/metrics-engine"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	defaultMetricsPageSize = 50
	defaultMetricsTopLimit = 10
)

func (ds *DiffractLLMServer) SetMetrics(m *metricsengine.MetricsEngine) { ds.metrics = m }

func (ds *DiffractLLMServer) requireMetrics(c *gin.Context) {
	if ds.metrics == nil {
		adminErr(c, http.StatusServiceUnavailable, "metrics engine is disabled")
		return
	}
	c.Next()
}

func (ds *DiffractLLMServer) metricsErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, metricsengine.ErrInvalidQuery):
		adminErr(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, metricsengine.ErrNotFound):
		adminErr(c, http.StatusNotFound, "request not found")
	case errors.Is(err, context.DeadlineExceeded):
		adminErr(c, http.StatusGatewayTimeout, "metrics query timed out")
	default:
		ds.logger.Error("metrics query", zap.String("path", c.FullPath()), zap.Error(err))
		adminErr(c, http.StatusInternalServerError, "metrics query failed")
	}
}

func timeRange(c *gin.Context) (from, to time.Time, ok bool) {
	var err error
	if from, err = time.Parse(time.RFC3339, c.Query("from")); err != nil {
		adminErr(c, http.StatusBadRequest, "from must be an RFC3339 time")
		return
	}
	if to, err = time.Parse(time.RFC3339, c.Query("to")); err != nil {
		adminErr(c, http.StatusBadRequest, "to must be an RFC3339 time")
		return
	}
	return from, to, true
}

func (ds *DiffractLLMServer) getMetricsStats(c *gin.Context) {
	from, to, ok := timeRange(c)
	if !ok {
		return
	}
	tiles, err := ds.metrics.GetOverviewTiles(c.Request.Context(), from, to)
	if err != nil {
		ds.metricsErr(c, err)
		return
	}
	c.JSON(http.StatusOK, tiles)
}

func (ds *DiffractLLMServer) getMetricsRequestSummary(c *gin.Context) {
	from, to, ok := timeRange(c)
	if !ok {
		return
	}
	summary, err := ds.metrics.GetRequestSummaryByTime(c.Request.Context(), from, to)
	if err != nil {
		ds.metricsErr(c, err)
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (ds *DiffractLLMServer) listMetricsRequests(c *gin.Context) {
	from, to, ok := timeRange(c)
	if !ok {
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		adminErr(c, http.StatusBadRequest, "offset must be a number")
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultMetricsPageSize)))
	if err != nil {
		adminErr(c, http.StatusBadRequest, "limit must be a number")
		return
	}
	page, err := ds.metrics.GetRequestLogsByTime(c.Request.Context(), from, to, offset, limit)
	if err != nil {
		ds.metricsErr(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (ds *DiffractLLMServer) getMetricsRequest(c *gin.Context) {
	detail, err := ds.metrics.GetRequestDetailByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		ds.metricsErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (ds *DiffractLLMServer) getMetricsRequestPayload(c *gin.Context) {
	payload, err := ds.metrics.GetPayloadByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		ds.metricsErr(c, err)
		return
	}
	c.JSON(http.StatusOK, payload)
}

func (ds *DiffractLLMServer) getMetricsTopVirtualKeys(c *gin.Context) {
	from, to, ok := timeRange(c)
	if !ok {
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultMetricsTopLimit)))
	if err != nil {
		adminErr(c, http.StatusBadRequest, "limit must be a number")
		return
	}
	keys, err := ds.metrics.GetTopVirtualKeys(c.Request.Context(), from, to, limit)
	if err != nil {
		ds.metricsErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": keys})
}

func (ds *DiffractLLMServer) getMetricsBudgetSpend(c *gin.Context) {
	from, to, ok := timeRange(c)
	if !ok {
		return
	}
	spend, err := ds.metrics.GetBudgetSpendByTime(c.Request.Context(), c.Param("id"), from, to)
	if err != nil {
		ds.metricsErr(c, err)
		return
	}
	c.JSON(http.StatusOK, spend)
}
