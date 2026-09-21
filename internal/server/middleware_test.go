package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// Mirrors production: the gateway logger attaches a stacktrace at Error.
func accessLogRouter(t *testing.T) (*gin.Engine, *observer.ObservedLogs) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core, zap.AddStacktrace(zapcore.ErrorLevel))

	r := gin.New()
	r.Use(RequestIDMiddleware(), AccessLogMiddleware(logger))
	r.GET("/ready", func(c *gin.Context) { c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready"}) })
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/boom", func(c *gin.Context) { c.JSON(http.StatusInternalServerError, gin.H{}) })
	r.GET("/nope", func(c *gin.Context) { c.JSON(http.StatusNotFound, gin.H{}) })
	r.GET("/fine", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })
	return r, logs
}

func callPath(r *gin.Engine, path string) {
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
}

// A 503 from /ready means "not ready yet", which is a normal state at boot and
// while credentials are missing. Logging it at Error made every probe an incident.
func TestProbesNeverLogAboveDebug(t *testing.T) {
	r, logs := accessLogRouter(t)

	callPath(r, "/ready")
	callPath(r, "/health")

	entries := logs.All()
	require.Len(t, entries, 2)
	for _, e := range entries {
		assert.Equal(t, zapcore.DebugLevel, e.Level, "probe logged above debug")
	}
}

func TestStatusDrivesLevelForRealRoutes(t *testing.T) {
	for _, tc := range []struct {
		path string
		want zapcore.Level
	}{
		{"/fine", zapcore.InfoLevel},
		{"/nope", zapcore.WarnLevel},
		{"/boom", zapcore.ErrorLevel},
	} {
		r, logs := accessLogRouter(t)
		callPath(r, tc.path)

		entries := logs.All()
		require.Len(t, entries, 1, tc.path)
		assert.Equal(t, tc.want, entries[0].Level, tc.path)
	}
}

// An access line is data about a request, not an exception. Without this the
// middleware's own frames are dumped on every 5xx.
func TestAccessLogCarriesNoStacktrace(t *testing.T) {
	r, logs := accessLogRouter(t)

	callPath(r, "/boom")
	callPath(r, "/ready")

	for _, e := range logs.All() {
		assert.Empty(t, e.Stack, "access log emitted a stacktrace")
	}
}

func TestAccessLogCarriesRequestID(t *testing.T) {
	r, logs := accessLogRouter(t)
	callPath(r, "/fine")

	entries := logs.All()
	require.Len(t, entries, 1)

	id, ok := entries[0].ContextMap()[RequestIDKey]
	require.True(t, ok, "request_id missing from the access line")
	assert.NotEmpty(t, id)
}
