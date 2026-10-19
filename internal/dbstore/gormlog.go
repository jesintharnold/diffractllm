package dbstore

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

const logSlowQuery = 200 * time.Millisecond

// gormLogger sends GORM's output to [dbstore]; query errors are returned to callers, so only slow queries are logged.
type gormLogger struct {
	log  *zap.Logger
	slow time.Duration
}

func (g gormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return g }

func (g gormLogger) Info(context.Context, string, ...any) {}

func (g gormLogger) Warn(_ context.Context, msg string, args ...any) {
	g.log.Warn("gorm", zap.String("detail", fmt.Sprintf(msg, args...)))
}

func (g gormLogger) Error(_ context.Context, msg string, args ...any) {
	g.log.Error("gorm", zap.String("detail", fmt.Sprintf(msg, args...)))
}

func (g gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), _ error) {
	if took := time.Since(begin); took >= g.slow {
		_, rows := fc()
		// No SQL text: it carries bound values (keys, ciphertext).
		g.log.Warn("slow query", zap.Duration("took", took), zap.Int64("rows", rows), zap.String("at", utils.FileWithLineNum()))
	}
}
