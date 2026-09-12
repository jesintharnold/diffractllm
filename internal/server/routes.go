package server

import (
	"diffractllm/internal/core"
	"diffractllm/internal/providers"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (ds *DiffractLLMServer) routeHandlers() (http.Handler, error) {

	descriptors := []providers.RouteDescriptor{}
	descriptors = append(descriptors, providers.OpenAIDescriptors...)

	// Router configs alone
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(ds.config.TrustedProxies); err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}

	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	router.Use(RequestIDMiddleware(), gin.CustomRecoveryWithWriter(nil, HandleRecovery(ds.logger)))

	// Route Descriptors
	groups := make(map[core.Provider]*gin.RouterGroup)
	for index := range descriptors {
		if !descriptors[index].AddToRoute {
			continue
		}

		descriptor := descriptors[index]
		sdkPath, exists := groups[descriptor.SDK]
		if !exists {
			sdkPath = router.Group("/" + string(descriptor.SDK))
			groups[descriptor.SDK] = sdkPath
		}
		sdkPath.Handle(descriptor.Method, descriptor.Path, func(ctx *gin.Context) {
			ds.GenericRequestHandler(ctx.Writer, ctx.Request, &descriptor)
		})
	}

	// ---- START OF ADMIN HANDLERS ----

	// ----- END OF ADMIN HANDLERS ------

	router.NoRoute(func(ctx *gin.Context) {
		ctx.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "unknown or unsupported endpoint"})
	})

	router.NoMethod(func(ctx *gin.Context) {
		ctx.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
	})

	return router, nil
}
