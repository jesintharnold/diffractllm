package core

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
)

type DiffractLLMContext struct {
	ctx         context.Context
	Request     *http.Request
	RequestID   string
	BodyBytes   []byte
	SDKProvider Provider
	Modelkey    CatalogKey
	RequestKind RequestKind
	Writer      http.ResponseWriter
	metadata    map[DiffractLLMContextKey]any
	aborted     atomic.Bool
	HookLog     HookLog

	// === GOVERNANCE FIELDS ===
	ClientID         string
	BudgetRef        string
	VirtualKeyID     string
	VirtualKeyPolicy *VirtualKey
	AuthFrozen       bool

	// === LOAD BALANCER ====
	RequestedProvider  Provider
	RequestedModel     string
	SelectedCredential *Credential

	// === PROXY OUTCOME FIELDS ===
	UpstreamStatus int
	TTFB           time.Duration
	UpstreamModel  string

	// === RESPONSE OUTCOME FIELDS ===
	RequestCompleted bool
	ResponseStatus   int
	ResponseBytes    int
	Error            *DiffractLLMError

	Usage *Usage

	Cost               float64
	StreamChunks       int32
	StreamFinishReason FinishReason
	StreamAborted      bool
	StartedAt          time.Time
}

func (rc *DiffractLLMContext) Context() context.Context { return rc.ctx }
func (rc *DiffractLLMContext) IsAborted() bool          { return rc.aborted.Load() }
func (rc *DiffractLLMContext) Set(key DiffractLLMContextKey, value any) error {
	if _, exists := rc.metadata[key]; exists {
		return fmt.Errorf("key '%s' already exists in context", key)
	}
	rc.metadata[key] = value
	return nil
}

func (rc *DiffractLLMContext) Get(key DiffractLLMContextKey) (any, bool) {
	val, ok := rc.metadata[key]
	return val, ok
}

func (rc *DiffractLLMContext) Overwrite(key DiffractLLMContextKey, value any) {
	rc.metadata[key] = value
}

func (rc *DiffractLLMContext) Write(p []byte) (int, error) {
	n, err := rc.Writer.Write(p)
	rc.ResponseBytes += n
	return n, err
}
func (rc *DiffractLLMContext) SetHeader(key, value string) {
	rc.Writer.Header().Set(key, value)
}
func (rc *DiffractLLMContext) Flush() {
	if f, ok := rc.Writer.(http.Flusher); ok {
		f.Flush()
	}
}
func (rc *DiffractLLMContext) JSON(code int, obj any) {

	data, err := sonic.Marshal(obj)
	if err != nil {
		return
	}

	rc.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	rc.Writer.WriteHeader(code)
	rc.Write(data)
	rc.aborted.Store(true)
}

func (rc *DiffractLLMContext) WriteSSE(event string, body []byte) error {
	size := len(body) + 8
	if event != "" {
		size += len(event) + 8
	}

	buf := make([]byte, 0, size)
	if event != "" {
		buf = append(buf, "event: "...)
		buf = append(buf, event...)
		buf = append(buf, '\n')
	}
	buf = append(buf, "data: "...)
	buf = append(buf, body...)
	buf = append(buf, '\n', '\n')

	n, err := rc.Writer.Write(buf)
	rc.ResponseBytes += n
	if err != nil {
		return err
	}
	rc.Flush()
	return nil
}

func (rc *DiffractLLMContext) WriteData(code int, contentType string, data []byte) {
	rc.Writer.Header().Set("Content-Type", contentType)
	rc.Writer.WriteHeader(code)
	rc.Write(data)
}

func (rc *DiffractLLMContext) Abort() {
	rc.aborted.Store(true)
}

func (rc *DiffractLLMContext) reset() {
	rc.ctx = nil
	rc.Request = nil
	rc.RequestID = ""
	rc.BodyBytes = nil
	rc.SDKProvider = ""
	rc.Modelkey = CatalogKey{}
	rc.RequestKind = ""
	rc.Writer = nil
	rc.aborted.Store(false)

	// === GOVERNANCE ===
	rc.ClientID = ""
	rc.BudgetRef = ""
	rc.VirtualKeyID = ""
	rc.VirtualKeyPolicy = nil
	rc.RequestedProvider = ""
	rc.RequestedModel = ""
	rc.SelectedCredential = nil

	rc.AuthFrozen = false
	rc.UpstreamStatus = 0
	rc.TTFB = 0
	rc.UpstreamModel = ""

	rc.RequestCompleted = false
	rc.ResponseStatus = 0
	rc.ResponseBytes = 0
	rc.Error = nil
	rc.Usage = nil
	rc.Cost = 0
	rc.StreamChunks = 0
	rc.StreamFinishReason = ""
	rc.StreamAborted = false
	rc.StartedAt = time.Time{}

	// Hook logs we are performing a reset - Important for flush
	rc.HookLog.reset()
	for k := range rc.metadata {
		delete(rc.metadata, k)
	}
}

type DiffractLLMContextPool struct {
	pool sync.Pool
}

func NewDiffractLLMContextPool() *DiffractLLMContextPool {
	return &DiffractLLMContextPool{
		pool: sync.Pool{
			New: func() any {
				return &DiffractLLMContext{
					metadata: make(map[DiffractLLMContextKey]any, 4),
				}
			},
		},
	}
}

func (p *DiffractLLMContextPool) Acquire(ctx context.Context, req *http.Request, w http.ResponseWriter) *DiffractLLMContext {
	rc := p.pool.Get().(*DiffractLLMContext)
	rc.ctx = ctx
	rc.Request = req
	rc.Writer = w
	return rc
}

func (p *DiffractLLMContextPool) Release(rc *DiffractLLMContext) {
	rc.reset()
	p.pool.Put(rc)
}
