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
	BodyBytes   []byte
	SDKProvider Provider
	Modelkey    ModelKey
	RequestKind RequestKind
	Writer      http.ResponseWriter
	metadata    map[DiffractLLMContextKey]any
	aborted     atomic.Bool
	HookLog     HookLog

	// === GOVERNANCE FIELDS ===
	ClientID      string
	Mode          VKMode
	AllowedModels map[ModelKey]struct{}
	ModelPools    map[string]struct{}
	BudgetRef     string
	VirtualKeyID  string
	VirtualKey    string
	AuthFrozen    bool

	// === LOAD BALANCER ====
	// TargetModel      *Model

	// === PROXY OUTCOME FIELDS ===
	UpstreamStatus int
	TTFB           time.Duration

	// === RESPONSE OUTCOME FIELDS ===
	RequestCompleted bool
	ResponseStatus   int
	ResponseBytes    int
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
func (rc *DiffractLLMContext) SetHeader(key, value string) {
	rc.Writer.Header().Set(key, value)
}
func (rc *DiffractLLMContext) Flush() {
	if f, ok := rc.Writer.(http.Flusher); ok {
		f.Flush()
	}
}
func (rc *DiffractLLMContext) JSON(code int, obj any) {
	rc.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	rc.Writer.WriteHeader(code)
	data, err := sonic.Marshal(obj)
	if err != nil {
		return
	}
	rc.Writer.Write(data)
	rc.aborted.Store(true)
}

func (rc *DiffractLLMContext) WriteData(code int, contentType string, data []byte) {
	rc.Writer.Header().Set("Content-Type", contentType)
	rc.Writer.WriteHeader(code)
	rc.Writer.Write(data)
}

func (rc *DiffractLLMContext) Abort() {
	rc.aborted.Store(true)
}

func (rc *DiffractLLMContext) reset() {
	rc.ctx = nil
	rc.Request = nil
	rc.BodyBytes = nil
	rc.SDKProvider = ""
	rc.Modelkey = ModelKey{}
	rc.RequestKind = ""
	rc.Writer = nil
	rc.aborted.Store(false)

	// === GOVERNANCE ===
	rc.ClientID = ""
	rc.Mode = VKAllowedModel
	rc.AllowedModels = nil
	rc.ModelPools = nil
	rc.BudgetRef = ""
	rc.VirtualKeyID = ""
	rc.VirtualKey = ""

	rc.AuthFrozen = false
	rc.UpstreamStatus = 0
	rc.TTFB = 0

	rc.RequestCompleted = false
	rc.ResponseStatus = 0
	rc.ResponseBytes = 0

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
