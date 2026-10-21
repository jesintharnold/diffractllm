package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteSSEFrames(t *testing.T) {
	rec := httptest.NewRecorder()
	rc := NewDiffractLLMContextPool().Acquire(context.Background(), nil, rec)

	require.NoError(t, rc.WriteSSE("", []byte(`{"a":1}`)))
	require.NoError(t, rc.WriteSSE("error", []byte(`{"b":2}`)))

	want := "data: {\"a\":1}\n\nevent: error\ndata: {\"b\":2}\n\n"
	assert.Equal(t, want, rec.Body.String())
	assert.Equal(t, len(want), rc.ResponseBytes)
	assert.True(t, rec.Flushed)
}

// discardWriter stands in for net/http's buffered writer without allocating.
type discardWriter struct{ h http.Header }

func (d discardWriter) Header() http.Header             { return d.h }
func (discardWriter) Write(p []byte) (int, error)       { return len(p), nil }
func (discardWriter) WriteString(s string) (int, error) { return len(s), nil }
func (discardWriter) WriteHeader(int)                   {}
func (discardWriter) Flush()                            {}

func TestWriteSSEDoesNotAllocate(t *testing.T) {
	rc := NewDiffractLLMContextPool().Acquire(context.Background(), nil, discardWriter{h: http.Header{}})
	body := []byte(`{"id":"c1","object":"chat.completion.chunk","choices":[{"delta":{"content":"hi"}}]}`)

	allocs := testing.AllocsPerRun(100, func() { _ = rc.WriteSSE("", body) })
	assert.Zero(t, allocs, "a frame is written straight into the response writer")
}
