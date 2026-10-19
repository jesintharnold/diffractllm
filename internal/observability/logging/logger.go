package logging

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	root  = zap.NewNop() // tests and early boot: silent
)

// Init builds the real logger once at boot, before any component is constructed.
// format is "console" (default) or "json".
func Init(lvl, format string) {
	level.SetLevel(parseLevel(strings.ToLower(lvl)))
	enc := zapcore.NewConsoleEncoder(consoleConfig)
	if format == FormatJSON {
		enc = zapcore.NewJSONEncoder(jsonConfig)
	}
	root = zap.New(zapcore.NewCore(enc, zapcore.Lock(os.Stdout), level)) // no caller, no auto stack traces
}

// For returns the logger for one component.
func For(component string) *zap.Logger { return root.Named(component) }

// SetLevel changes the level at runtime (the settings registry calls it).
func SetLevel(levelStr string) { level.SetLevel(parseLevel(strings.ToLower(levelStr))) }

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
