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
	admin := router.Group("/v1/admin")

	admin.GET("/stats", ds.handleStats)
	admin.POST("/sync/catalog", ds.handleModelCatalog)

	budgets := admin.Group("/budgets")
	{
		budgets.GET("", ds.listBudgets)
		budgets.POST("", ds.createBudget)
		budgets.GET("/:id", ds.getBudget)
		budgets.PUT("/:id", ds.updateBudget)
		budgets.DELETE("/:id", ds.deleteBudget)
	}

	keys := admin.Group("/virtual-keys")
	{
		keys.GET("", ds.listVirtualKeys)
		keys.POST("", ds.createVirtualKey)
		keys.GET("/:id", ds.getVirtualKey)
		keys.PUT("/:id/routing", ds.updateVirtualKeyRouting)
		keys.POST("/:id/rotate", ds.rotateVirtualKey)
		keys.DELETE("/:id", ds.revokeVirtualKey)
	}

	providers := admin.Group("/providers")
	{
		providers.GET("", ds.listProviders)
		providers.GET("/:providername", ds.getProvider)
		providers.GET("/:providername/settings", ds.getProviderSettings)
		providers.PUT("/:providername/settings", ds.updateProvider)

		providers.GET("/:providername/credentials", ds.listCredentials)
		providers.POST("/:providername/credentials", ds.createCredential)
		providers.PUT("/:providername/credentials/:id", ds.updateCredential)
		providers.DELETE("/:providername/credentials/:id", ds.deleteCredential)
	}

	models := admin.Group("/models")
	{
		models.GET("", ds.listModels)
		models.GET("/catalog", ds.listModelCatalog)
	}

	pricing := admin.Group("/pricing/custom")
	{
		pricing.GET("", ds.listCustomPricing)
		pricing.POST("", ds.createCustomPricing)
		pricing.PUT("/:id", ds.updateCustomPricing)
		pricing.DELETE("/:id", ds.deleteCustomPricing)
	}

	// ----- END OF ADMIN HANDLERS ------

	router.NoRoute(func(ctx *gin.Context) {
		ctx.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "unknown or unsupported endpoint"})
	})

	router.NoMethod(func(ctx *gin.Context) {
		ctx.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
	})

	return router, nil
}
