package server

import (
	"context"
	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"
	"diffractllm/internal/dbstore"
	"diffractllm/internal/governance"
	"diffractllm/internal/modelcatalog"
	"diffractllm/internal/providerplane"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type DiffractLLMServer struct {
	httpServer *http.Server
	logger     *zap.Logger

	CtxPool *core.DiffractLLMContextPool

	isReady   atomic.Bool
	config    *config.ServerConfig
	startOnce sync.Once
	serveErr  chan error

	HookEngine      *core.HookEngine
	governance      *governance.Governance
	selectionEngine *dataplane.SelectionEngine
	CredentialPlane *providerplane.ProviderPlane
	ModelCatalog    *modelcatalog.ModelCatalog
	dbStore         *dbstore.Store
}

func NewDiffractLLMServer(logger *zap.Logger, config *config.ServerConfig, gov *governance.Governance, selectionEngine *dataplane.SelectionEngine, credentialPlane *providerplane.ProviderPlane, catalog *modelcatalog.ModelCatalog,
	store *dbstore.Store) *DiffractLLMServer {
	hookEngine := core.NewHookEngine(logger)
	governance.RegisterHooks(hookEngine, logger, gov, catalog)
	return &DiffractLLMServer{
		logger:          logger,
		CtxPool:         core.NewDiffractLLMContextPool(),
		config:          config,
		serveErr:        make(chan error, 1),
		HookEngine:      hookEngine,
		governance:      gov,
		selectionEngine: selectionEngine,
		CredentialPlane: credentialPlane,
		ModelCatalog:    catalog,
		dbStore:         store,
	}
}

func (ds *DiffractLLMServer) Start() error {
	var serverErr error
	ds.startOnce.Do(func() {
		handler, err := ds.routeHandlers()
		if err != nil {
			serverErr = err
			return
		}

		ds.httpServer = &http.Server{
			Addr:              fmt.Sprintf(":%d", ds.config.Port),
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      0, // Disabled to support SSE streaming; Provider Transports will take care of the enforcement
			IdleTimeout:       60 * time.Second,
		}

		listener, err := net.Listen("tcp", ds.httpServer.Addr)
		if err != nil {
			serverErr = err
			return
		}
		ds.isReady.Store(true)
		ds.logger.Info("http server listening", zap.String("addr", ds.httpServer.Addr))

		go func() {
			err := ds.httpServer.Serve(listener)
			ds.isReady.Store(false)
			if err != nil && err != http.ErrServerClosed {
				ds.logger.Error("http server error", zap.Error(err))
				ds.serveErr <- err
				return
			}
			ds.logger.Info("http server stopped serving")
			close(ds.serveErr)
		}()
	})
	return serverErr
}

func (ds *DiffractLLMServer) ServeError() <-chan error { return ds.serveErr }

func (ds *DiffractLLMServer) Shutdown(ctx context.Context) error {
	if ds.httpServer == nil {
		return nil
	}
	ds.isReady.Store(false)
	return ds.httpServer.Shutdown(ctx)
}
func (ds *DiffractLLMServer) Status() bool {
	return ds.isReady.Load()
}
