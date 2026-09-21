package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"
	"diffractllm/internal/dbstore"
	"diffractllm/internal/governance"
	"diffractllm/internal/modelcatalog"
	logger "diffractllm/internal/observability/logging"
	"diffractllm/internal/providerplane"
	"diffractllm/internal/providers"
	azureprovider "diffractllm/internal/providers/azure"
	openaiprovider "diffractllm/internal/providers/openai"
	"diffractllm/internal/server"
	"diffractllm/internal/worker"

	"go.uber.org/zap"
)

const (
	StreamGrace     = 15 * time.Second
	ShutdownTimeout = 30 * time.Second
	DrainBudget     = worker.DrainTimeout + 5*time.Second
)

type Gateway interface {
	Initialize() error
	Start() error
	Stop() error
	WaitForSignal()
	IsRunning() bool
	Stats() []ComponentStats
	GetStatus() map[string]any
}

type gatewayImpl struct {
	cfg        *config.GatewayConfig
	logger     *zap.Logger
	source     *dbstore.DBSource
	governance *governance.Governance
	catalog    *modelcatalog.ModelCatalog
	modelplane *providerplane.ProviderPlane
	dataplane  *dataplane.SelectionEngine
	server     *server.DiffractLLMServer
	stopChan   chan struct{}
	isRunning  bool
	mu         sync.RWMutex
	startTime  time.Time
	cancelWork context.CancelFunc
}

type ComponentStats struct {
	Component string            `json:"component"`
	Jobs      []worker.JobStats `json:"jobs"`
}

func NewGateway() Gateway {
	return &gatewayImpl{
		stopChan: make(chan struct{}),
	}
}

func (gw *gatewayImpl) Initialize() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	gw.cfg = cfg
	base := logger.LogInit(cfg.Observability.LogLevel)
	gw.logger = base.With(zap.String("component", "gateway"))
	gw.logger.Info("initializing DiffractLLM gateway")

	source, err := dbstore.NewDBSource(base)
	if err != nil {
		return fmt.Errorf("db source: %w", err)
	}
	gw.source = source

	if err := source.Init(); err != nil {
		return fmt.Errorf("migrate and seed: %w", err)
	}

	upstreams, credentials, err := source.Load()
	if err != nil {
		return fmt.Errorf("boot snapshot: %w", err)
	}
	store := source.GetStore()

	gw.logger.Info("boot snapshot loaded", zap.Int("upstreams", len(upstreams)), zap.Int("credentials", len(credentials)), zap.String("store", source.Path()))
	gw.catalog = modelcatalog.NewModelCatalog(store, *cfg.ModelCatalog, base)
	gov, err := governance.NewGovernance(store, base)
	if err != nil {
		return fmt.Errorf("governance: %w", err)
	}
	gw.governance = gov

	gw.modelplane = providerplane.NewProviderPlane(credentials)
	gw.dataplane = dataplane.NewSelectionEngine(gw.modelplane, base)

	registry := providers.NewProviderInstance()
	transport := dataplane.NewTransport(cfg.Upstream, upstreamMap(upstreams), base)
	registry.Register(openaiprovider.New(transport, base))
	registry.Register(azureprovider.New(transport, base))

	gw.logger.Info("adapters registered", zap.Int("count", registry.Len()), zap.Any("providers", registry.Providers()))

	gw.server = server.NewDiffractLLMServer(base, cfg.ServerConfig, gov, gw.dataplane, gw.modelplane,
		gw.catalog, registry, transport, store,
	)

	gw.logger.Info("gateway initialized successfully")
	return nil
}

func upstreamMap(upstreams []*core.Upstream) map[core.Provider]*core.Upstream {
	out := make(map[core.Provider]*core.Upstream, len(upstreams))
	for _, u := range upstreams {
		if u != nil {
			out[u.Provider] = u
		}
	}
	return out
}

func (gw *gatewayImpl) Start() error {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	if gw.isRunning {
		return fmt.Errorf("gateway is already running")
	}
	if gw.server == nil {
		return fmt.Errorf("gateway is not initialized")
	}

	workCtx, cancelWork := context.WithCancel(context.Background())
	gw.cancelWork = cancelWork

	gw.logger.Info("starting model catalog")
	if err := gw.catalog.Start(workCtx); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}

	gw.logger.Info("starting governance sync and flush jobs")
	if err := gw.governance.Start(workCtx); err != nil {
		return fmt.Errorf("governance: %w", err)
	}

	gw.logger.Info("starting http server")
	if err := gw.server.Start(); err != nil {
		return fmt.Errorf("http server: %w", err)
	}

	gw.isRunning = true
	gw.startTime = time.Now()
	gw.logger.Info("gateway started successfully", zap.Int("port", gw.cfg.ServerConfig.Port))
	return nil
}

func (gw *gatewayImpl) Stop() error {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	lg := gw.logger
	if lg == nil {
		lg = zap.NewNop()
	}

	var stopErr error

	if gw.isRunning {
		httpCtx, cancelHTTP := context.WithTimeout(context.Background(), ShutdownTimeout)
		lg.Info("[1/4] stopping http server")
		if err := gw.server.Shutdown(httpCtx, StreamGrace); err != nil {
			lg.Error("http shutdown failed", zap.Error(err))
			stopErr = errors.Join(stopErr, fmt.Errorf("http: %w", err))
		}
		cancelHTTP()

		govCtx, cancelGov := context.WithTimeout(context.Background(), DrainBudget)
		lg.Info("[2/4] draining governance")
		if err := gw.governance.Shutdown(govCtx); err != nil {
			lg.Error("governance shutdown failed", zap.Error(err))
			stopErr = errors.Join(stopErr, fmt.Errorf("governance: %w", err))
		}
		cancelGov()

		catCtx, cancelCat := context.WithTimeout(context.Background(), DrainBudget)
		lg.Info("[3/4] stopping model catalog")
		if err := gw.catalog.Shutdown(catCtx); err != nil {
			lg.Error("catalog shutdown failed", zap.Error(err))
			stopErr = errors.Join(stopErr, fmt.Errorf("catalog: %w", err))
		}
		cancelCat()
	}

	gw.isRunning = false

	if gw.source != nil {
		lg.Info("[4/4] closing store")
		if err := gw.source.Close(); err != nil {
			lg.Error("store close failed", zap.Error(err))
			stopErr = errors.Join(stopErr, fmt.Errorf("store: %w", err))
		}
		gw.source = nil
	}

	if gw.cancelWork != nil {
		gw.cancelWork()
		gw.cancelWork = nil
	}

	if stopErr != nil {
		return stopErr
	}

	lg.Info("gateway stopped successfully")
	_ = lg.Sync()
	return nil
}

func (gw *gatewayImpl) WaitForSignal() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	select {
	case sig := <-signalChan:
		gw.logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	case <-gw.stopChan:
		gw.logger.Info("stop channel triggered")
	case err, ok := <-gw.server.ServeError():
		if ok && err != nil {
			gw.logger.Error("http server failed", zap.Error(err))
		}
	}

	signal.Stop(signalChan)

	gw.logger.Info("shutting down gracefully")
	if err := gw.Stop(); err != nil {
		gw.logger.Error("shutdown error", zap.Error(err))
		return
	}
	gw.logger.Info("shutdown completed successfully")
}

func (gw *gatewayImpl) IsRunning() bool {
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	return gw.isRunning
}

func (gw *gatewayImpl) Stats() []ComponentStats {
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	if !gw.isRunning {
		return nil
	}
	return []ComponentStats{
		{Component: "governance", Jobs: gw.governance.Stats()},
		{Component: "catalog", Jobs: gw.catalog.Stats()},
	}
}

func (gw *gatewayImpl) GetStatus() map[string]any {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	status := map[string]any{"running": gw.isRunning}
	if gw.cfg != nil {
		status["port"] = gw.cfg.ServerConfig.Port
		status["config_source"] = gw.cfg.ServerConfig.ConfigSource
		status["log_level"] = gw.cfg.Observability.LogLevel
	}
	if gw.source != nil {
		status["store"] = gw.source.Path()
	}
	if gw.isRunning {
		status["uptime_seconds"] = math.Round(time.Since(gw.startTime).Seconds()*100) / 100
	}
	return status
}
