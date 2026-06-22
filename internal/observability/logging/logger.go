package logging

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	log          *zap.Logger
	logsingleton sync.Once
)

func parseLevel(levelStr string) zapcore.Level {
	switch levelStr {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "dpanic":
		return zapcore.DPanicLevel
	case "panic":
		return zapcore.PanicLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

func logNew(loglevel string) *zap.Logger {
	var loggerInstance *zap.Logger
	logsingleton.Do(func() {

		level := parseLevel(strings.ToLower(loglevel))
		zapConfig := zap.Config{
			Level:            zap.NewAtomicLevelAt(level),
			Development:      false,
			Encoding:         "json",
			EncoderConfig:    zap.NewProductionEncoderConfig(),
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
		}
		var err error
		loggerInstance, err = zapConfig.Build()
		if err != nil {
			panic(err)
		}
		// defer loggerInstance.Sync()
		loggerInstance.Info("Custom logger initialized")
	})
	return loggerInstance
}

func LogInit(LOGLEVEL string) *zap.Logger {
	if log == nil {
		log = logNew(LOGLEVEL)
	}
	return log
}

func PrintAggregatedErrors(err error) {
	if err == nil {
		return
	}

	if merr, ok := err.(*multierror.Error); ok {
		if len(merr.Errors) == 0 {
			fmt.Fprintf(os.Stderr, "  - %v\n", merr.Error())
			return
		}
		fmt.Fprintln(os.Stderr, "Errors:")
		for index, err := range merr.Errors {
			fmt.Fprintf(os.Stderr, "  %d)%v\n", index+1, err)
		}
		return
	}

	fmt.Fprintf(os.Stderr, "Error: %v\n", err)

}
