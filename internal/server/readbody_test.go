package server

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bodyServer(maxKB int) *DiffractLLMServer {
	return &DiffractLLMServer{config: &config.ServerConfig{MaxBodySize: maxKB}}
}

func readBodyOf(t *testing.T, ds *DiffractLLMServer, r *http.Request) (*bytes.Buffer, *core.DiffractLLMError) {
	t.Helper()
	rctx := core.NewDiffractLLMContextPool().Acquire(r.Context(), r, httptest.NewRecorder())
	return ds.readBody(rctx)
}

func TestReadBodyDoesNotTrustALargeDeclaredLength(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"gpt-4o"}`))
	r.ContentLength = 8 << 20 // declares 8 MB, sends 18 bytes

	buf, derr := readBodyOf(t, bodyServer(32<<10), r)
	require.Nil(t, derr)
	assert.Equal(t, `{"model":"gpt-4o"}`, buf.String())
	assert.LessOrEqual(t, buf.Cap(), maxPooledBody, "preallocation is capped, not sized by the client")
}

func TestReadBodyReadsAChunkedBody(t *testing.T) {
	payload := strings.Repeat("x", 100<<10)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	r.ContentLength = -1

	buf, derr := readBodyOf(t, bodyServer(32<<10), r)
	require.Nil(t, derr)
	assert.Equal(t, len(payload), buf.Len())
}

func TestReadBodyRejectsAnOversizedBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 2<<10)))
	r.ContentLength = -1

	_, derr := readBodyOf(t, bodyServer(1), r)
	require.NotNil(t, derr)
	assert.Equal(t, http.StatusRequestEntityTooLarge, derr.StatusCode)
}

// slowServer serves h with a short ReadTimeout, standing in for the gateway's readTimeout.
func slowServer(t *testing.T, timeout time.Duration, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(h)
	server.Config.ReadTimeout = timeout
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestReadBodyCutsOffASlowClient(t *testing.T) {
	got := make(chan *core.DiffractLLMError, 1)
	ds := bodyServer(32 << 10)
	server := slowServer(t, 100*time.Millisecond, func(w http.ResponseWriter, r *http.Request) {
		rctx := core.NewDiffractLLMContextPool().Acquire(r.Context(), r, w)
		_, derr := ds.readBody(rctx)
		got <- derr
	})

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\n{\"model\"") // then stalls

	select {
	case derr := <-got:
		require.NotNil(t, derr, "a stalled body must fail, not hang")
	case <-time.After(2 * time.Second):
		t.Fatal("readBody did not give up on a stalled client")
	}
}

// ReadTimeout must not cancel a response that outlives it, such as a long stream.
func TestReadTimeoutDoesNotCancelALongResponse(t *testing.T) {
	const timeout = 50 * time.Millisecond
	ctxErr := make(chan error, 1)
	ds := bodyServer(32 << 10)
	server := slowServer(t, timeout, func(w http.ResponseWriter, r *http.Request) {
		rctx := core.NewDiffractLLMContextPool().Acquire(r.Context(), r, w)
		if _, derr := ds.readBody(rctx); derr != nil {
			ctxErr <- derr
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(4 * timeout)
		ctxErr <- r.Context().Err()
	})

	res, err := http.Post(server.URL, "application/json", strings.NewReader(`{"model":"gpt-4o"}`))
	require.NoError(t, err)
	defer res.Body.Close()

	assert.NoError(t, <-ctxErr, "the request context must survive past ReadTimeout")
}
