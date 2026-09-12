package server

import (
	config "diffractllm/configs"
	"diffractllm/internal/core"
	"net/http"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

type DiffractLLMServer struct {
	httpServer *http.Server
	logger     *zap.Logger
	CtxPool    *core.DiffractLLMContextPool
	HookEngine *core.HookEngine
	isReady    atomic.Bool
	config     *config.ServerConfig
	stopChan   <-chan struct{}
	startOnce  sync.Once
}

func NewDiffractLLMServer(logger *zap.Logger, hookengine *core.HookEngine, config *config.ServerConfig, stopchan <-chan struct{}) *DiffractLLMServer {
	hookEngine := core.NewHookEngine(logger)
	
	return &server
}
