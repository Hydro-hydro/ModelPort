package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetApiRouterRegistersModelPricingEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	require.NotPanics(t, func() {
		SetApiRouter(engine)
	})

	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/pricing" {
			assert.Equal(t, "github.com/QuantumNous/new-api/controller.GetPricing", route.Handler)
			return
		}
	}

	t.Fatal("GET /api/pricing route is not registered")
}
