package logging

import (
	"fmt"
	"time"

	"go.uber.org/zap/zapcore"
)

const (
	FormatConsole = "console"
	FormatJSON    = "json"
)

const componentWidth = 10 // longest name: "governance"

// consoleConfig prints "[time] [LEVEL] [component] message  {fields}"; no caller.
var consoleConfig = zapcore.EncoderConfig{
	TimeKey:          "time",
	LevelKey:         "level",
	NameKey:          "component",
	MessageKey:       "msg",
	StacktraceKey:    "stack",
	LineEnding:       zapcore.DefaultLineEnding,
	ConsoleSeparator: " ",
	EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString("[" + t.Format("2006-01-02 15:04:05.000") + "]")
	},
	EncodeLevel: func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(fmt.Sprintf("[%-5s]", l.CapitalString()))
	},
	EncodeName: func(name string, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(fmt.Sprintf("[%-*s]", componentWidth, name))
	},
	EncodeDuration: zapcore.StringDurationEncoder,
}

var jsonConfig = zapcore.EncoderConfig{
	TimeKey:        "time",
	LevelKey:       "level",
	NameKey:        "component",
	MessageKey:     "msg",
	StacktraceKey:  "stack",
	LineEnding:     zapcore.DefaultLineEnding,
	EncodeTime:     zapcore.ISO8601TimeEncoder,
	EncodeLevel:    zapcore.LowercaseLevelEncoder,
	EncodeName:     zapcore.FullNameEncoder,
	EncodeDuration: zapcore.StringDurationEncoder,
}
