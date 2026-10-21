package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"diffractllm/internal/core"
	"diffractllm/internal/providers"

	"github.com/stretchr/testify/assert"
)

func TestWriteJSONSendsRawBytesUnchanged(t *testing.T) {
	rec := httptest.NewRecorder()
	rctx := core.NewDiffractLLMContextPool().Acquire(context.Background(), nil, rec)
	raw := json.RawMessage("{\n  \"content\": \"hi\",\n  \"not_in_the_ir\": true\n}")

	writeJSON(rctx, http.StatusOK, raw)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, string(raw), rec.Body.String(), "not re-encoded, whitespace and all")
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, []byte(raw), rctx.ResponseBody, "payload capture sees what the client got")
}

func TestWriteJSONMarshalsEverythingElse(t *testing.T) {
	rec := httptest.NewRecorder()
	rctx := core.NewDiffractLLMContextPool().Acquire(context.Background(), nil, rec)

	writeJSON(rctx, http.StatusOK, map[string]int{"n": 1})

	assert.JSONEq(t, `{"n":1}`, rec.Body.String())
}

func TestWriteErrSendsRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	rctx := core.NewDiffractLLMContextPool().Acquire(context.Background(), nil, rec)

	writeErr(rctx, &providers.OpenAIDescriptors[0], core.NewProviderOverloaded("azure", 256))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("Retry-After"))
}
