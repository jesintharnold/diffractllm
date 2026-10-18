package server

import (
	"errors"
	"io"
	"net/http"

	"diffractllm/internal/settings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (ds *DiffractLLMServer) registerSettings(admin *gin.RouterGroup) {
	serve := func(op string, do func(name string, c *gin.Context) (any, error)) gin.HandlerFunc {
		return func(c *gin.Context) {
			if ds.settings == nil {
				adminErr(c, http.StatusServiceUnavailable, "settings are not available")
				return
			}
			name := c.Param("scope")
			view, err := do(name, c)
			var invalid *settings.Error
			switch {
			case err == nil:
				c.JSON(http.StatusOK, view)
			case errors.Is(err, settings.ErrUnknownScope):
				adminErr(c, http.StatusNotFound, err.Error())
			case errors.As(err, &invalid):
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": invalid.Error(), "fields": invalid.Fields})
			case errors.Is(err, settings.ErrBadForm):
				adminErr(c, http.StatusBadRequest, err.Error())
			case errors.Is(err, settings.ErrNotApplied):
				ds.logger.Error(op+" "+name+" settings", zap.Error(err))
				adminErr(c, http.StatusInternalServerError, err.Error())
			default:
				ds.logger.Error(op+" "+name+" settings", zap.Error(err))
				adminErr(c, http.StatusInternalServerError, op+" "+name+" settings failed")
			}
		}
	}

	admin.GET("/settings/:scope", serve("read", func(name string, _ *gin.Context) (any, error) {
		return ds.settings.View(name)
	}))
	admin.PUT("/settings/:scope", serve("save", func(name string, c *gin.Context) (any, error) {
		form, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, adminMaxBody))
		if err != nil {
			return nil, errors.Join(settings.ErrBadForm, err)
		}
		return ds.settings.Save(name, form)
	}))
	admin.DELETE("/settings/:scope", serve("reset", func(name string, _ *gin.Context) (any, error) {
		return ds.settings.Reset(name)
	}))
}
