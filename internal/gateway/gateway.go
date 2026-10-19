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
	"diffractllm/internal/buildinfo"
	"diffractllm/internal/core"
	"diffractllm/internal/dataplane"
	"diffractllm/internal/dbstore"
	"diffractllm/internal/governance"
	metricsengine "diffractllm/internal/metrics-engine"
	"diffractllm/internal/modelcatalog"
	"diffractllm/internal/observability"
	"diffractllm/internal/observability/logging"
	"diffractllm/internal/providerplane"
	"diffractllm/internal/providers"
	azureprovider "diffractllm/internal/providers/azure"
	openaiprovider "diffractllm/internal/providers/openai"
	"diffractllm/internal/server"
	"diffractllm/internal/settings"
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
	metrics    *metricsengine.MetricsEngine
	modelplane *providerplane.ProviderPlane
	dataplane  *dataplane.SelectionEngine
	server     *server.DiffractLLMServer
	stopChan   chan struct{}
	isRunning  bool
	mu         sync.RWMutex
	startTime  time.Time
	bootStart  time.Time // for the "started" line
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
	// Before any constructor: they all take their logger from logging.For.
	logging.Init(cfg.Observability.LogLevel, cfg.Observability.LogFormat)
	gw.logger = logging.For("gateway")
	gw.bootStart = time.Now()
	gw.logger.Info("starting", zap.String("version", buildinfo.Version),
		zap.String("log_level", cfg.Observability.LogLevel), zap.String("log_format", cfg.Observability.LogFormat))

	source, err := dbstore.NewDBSource()
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

	reg := settings.NewRegistry(store)
	if _, err := observability.Register(reg, cfg); err != nil {
		return fmt.Errorf("observability: %w", err)
	}
	govDefaults, err := governance.FromConfig(*cfg.Governance)
	if err != nil {
		return fmt.Errorf("governance config: %w", err)
	}
	govSettings, err := governance.Register(reg, govDefaults)
	if err != nil {
		return err
	}

	gw.catalog = modelcatalog.NewModelCatalog(store, *cfg.ModelCatalog)
	gov, err := governance.NewGovernance(store, govSettings.Running())
	if err != nil {
		return fmt.Errorf("governance: %w", err)
	}
	gw.governance = gov

	gw.modelplane = providerplane.NewProviderPlane(credentials)
	gw.dataplane = dataplane.NewSelectionEngine(gw.modelplane)

	registry := providers.NewProviderInstance()
	transport := dataplane.NewTransport(cfg.Upstream, upstreamMap(upstreams))
	registry.Register(openaiprovider.New(transport))
	registry.Register(azureprovider.New(transport))

	gw.logger.Info("providers ready", zap.Any("providers", registry.Providers()),
		zap.Int("upstreams", len(upstreams)), zap.Int("credentials", len(credentials)))

	gw.server = server.NewDiffractLLMServer(cfg.ServerConfig, gov, gw.dataplane, gw.modelplane,
		gw.catalog, registry, transport, store,
	)
	gw.server.SetSettings(reg)

	if mc := cfg.MetricsEngine; mc != nil && mc.Enabled {
		events := metricsengine.NewDuckDBStore(context.Background())
		if err := events.Init(mc.DSN, context.Background()); err != nil {
			return fmt.Errorf("metrics store %q: %w", mc.DSN, err)
		}
		gw.metrics = metricsengine.NewMetricsEngine(events, mc)
		if err := metricsengine.RegisterHooks(gw.server.HookEngine, gw.metrics); err != nil {
			return fmt.Errorf("metrics hooks: %w", err)
		}
		gw.server.SetMetrics(gw.metrics)
	}
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

	if err := gw.catalog.Start(workCtx); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}

	if err := gw.governance.Start(workCtx); err != nil {
		return fmt.Errorf("governance: %w", err)
	}

	if gw.metrics != nil {
		if err := gw.metrics.Start(workCtx); err != nil {
			return fmt.Errorf("metrics: %w", err)
		}
	}

	if err := gw.server.Start(); err != nil {
		return fmt.Errorf("http server: %w", err)
	}

	gw.isRunning = true
	gw.startTime = time.Now()
	gw.logger.Info("started", zap.Duration("took", time.Since(gw.bootStart).Round(time.Millisecond)))
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
		lg.Debug("stopping http server")
		if err := gw.server.Shutdown(httpCtx, StreamGrace); err != nil {
			lg.Error("http shutdown failed", zap.Error(err))
			stopErr = errors.Join(stopErr, fmt.Errorf("http: %w", err))
		}
		cancelHTTP()
	}

	// Outside the running check: a failed Start must still close the DuckDB file.
	if gw.metrics != nil {
		metricsCtx, cancelMetrics := context.WithTimeout(context.Background(), DrainBudget)
		lg.Debug("flushing metrics events")
		if err := gw.metrics.Shutdown(metricsCtx); err != nil {
			lg.Error("metrics shutdown failed", zap.Error(err))
			stopErr = errors.Join(stopErr, fmt.Errorf("metrics: %w", err))
		}
		cancelMetrics()
		gw.metrics = nil
	}

	if gw.isRunning {
		govCtx, cancelGov := context.WithTimeout(context.Background(), DrainBudget)
		lg.Debug("draining governance")
		if err := gw.governance.Shutdown(govCtx); err != nil {
			lg.Error("governance shutdown failed", zap.Error(err))
			stopErr = errors.Join(stopErr, fmt.Errorf("governance: %w", err))
		}
		cancelGov()

		catCtx, cancelCat := context.WithTimeout(context.Background(), DrainBudget)
		lg.Debug("stopping model catalog")
		if err := gw.catalog.Shutdown(catCtx); err != nil {
			lg.Error("catalog shutdown failed", zap.Error(err))
			stopErr = errors.Join(stopErr, fmt.Errorf("catalog: %w", err))
		}
		cancelCat()
	}

	gw.isRunning = false

	if gw.source != nil {
		lg.Debug("closing store")
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

	lg.Info("stopped")
	_ = lg.Sync()
	return nil
}

func (gw *gatewayImpl) WaitForSignal() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	select {
	case sig := <-signalChan:
		gw.logger.Info("shutting down", zap.String("signal", sig.String()))
	case <-gw.stopChan:
		gw.logger.Info("shutting down", zap.String("signal", "stop"))
	case err, ok := <-gw.server.ServeError():
		if ok && err != nil {
			gw.logger.Error("http server failed", zap.Error(err))
		}
	}

	signal.Stop(signalChan)

	if err := gw.Stop(); err != nil {
		gw.logger.Error("shutdown error", zap.Error(err))
	}
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
	stats := []ComponentStats{
		{Component: "governance", Jobs: gw.governance.Stats()},
		{Component: "catalog", Jobs: gw.catalog.Stats()},
	}
	if gw.metrics != nil {
		stats = append(stats, ComponentStats{Component: "metrics", Jobs: gw.metrics.Stats()})
	}
	return stats
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
