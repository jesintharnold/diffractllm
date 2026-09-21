package server

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const RequestIDKey = "request_id"

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.Must(uuid.NewV7()).String()
		}
		c.Header("X-Request-ID", requestID)
		c.Set(RequestIDKey, requestID)
		c.Next()
	}
}

func AccessLogMiddleware(logger *zap.Logger) gin.HandlerFunc {
	log := logger.WithOptions(zap.AddStacktrace(zapcore.FatalLevel)).With(zap.String("component", "http"))
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("took", time.Since(start)),
			zap.String("client_ip", c.ClientIP()),
			zap.String(RequestIDKey, c.GetString(RequestIDKey)),
		}
		if query != "" {
			fields = append(fields, zap.String("query", query))
		}
		if size := c.Writer.Size(); size > 0 {
			fields = append(fields, zap.Int("bytes", size))
		}
		if errs := c.Errors.ByType(gin.ErrorTypePrivate).String(); errs != "" {
			fields = append(fields, zap.String("errors", errs))
		}

		switch {
		case path == "/health" || path == "/ready":
			log.Debug("request", fields...)
		case status >= http.StatusInternalServerError:
			log.Error("request", fields...)
		case status >= http.StatusBadRequest:
			log.Warn("request", fields...)
		default:
			log.Info("request", fields...)
		}
	}
}

func HandleRecovery(logger *zap.Logger) gin.RecoveryFunc {
	return func(c *gin.Context, err any) {
		logger.Error("handler panicked", zap.Any("panic", err), zap.String("path", c.Request.URL.Path), zap.Bool("mid_response", c.Writer.Written()), zap.String("stack", string(debug.Stack())))

		if c.Writer.Written() {
			c.Abort()
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}
