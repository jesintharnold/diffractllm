package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/observability/logging"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The OpenAI dialect lives under /openai: base_url <host>/openai/v1 and <host>/openai both work; bare /v1 stays free.
func TestOpenAIRoutesStayUnderTheOpenAIPrefix(t *testing.T) {
	ds := &DiffractLLMServer{
		logger: zap.NewNop(), config: &config.ServerConfig{MaxBodySize: 1024},
		governance: syncedGovernance(t), CtxPool: core.NewDiffractLLMContextPool(),
		HookEngine: core.NewHookEngine(logging.For("hooks")),
	}
	h, err := ds.routeHandlers()
	require.NoError(t, err)

	for path, want := range map[string]int{
		"/openai/v1/chat/completions": http.StatusUnauthorized, // reaches the handler; no key gives 401
		"/openai/chat/completions":    http.StatusUnauthorized,
		"/v1/chat/completions":        http.StatusNotFound, // reserved for a future native API
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`))))
		assert.Equal(t, want, rec.Code, path)
	}
}
