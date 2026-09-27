package metricsengine

import (
	config "diffractllm/configs"

	"go.uber.org/zap"
)

type MetricsEngine struct {
	OLAPStore *OLAPStore
	Logger    *zap.Logger
	config    *config.MetricsEngineConfig
}
