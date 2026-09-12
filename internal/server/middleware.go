package server

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.Must(uuid.NewV7()).String()
		}
		c.Header("X-Request-ID", requestID)
		c.Next()
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
