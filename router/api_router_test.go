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

func TestSetApiRouterOmitsRemovedPublicContentRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetApiRouter(engine)

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	for _, path := range []string{
		"/api/notice",
		"/api/about",
		"/api/user-agreement",
		"/api/privacy-policy",
		"/api/home_page_content",
	} {
		_, registered := routes[http.MethodGet+" "+path]
		assert.False(t, registered, path)
	}
}

func TestSetApiRouterOmitsDisabledOptionalFeatureRoutes(t *testing.T) {
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "false")
	t.Setenv("MODELPORT_ENABLE_MEDIA_TASKS", "false")
	t.Setenv("MODELPORT_ENABLE_DEPLOYMENTS", "false")
	t.Setenv("MODELPORT_ENABLE_MULTI_NODE", "false")

	engine := gin.New()
	SetApiRouter(engine)

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		"GET /api/plugin/task",
		"GET /api/task_plugin_options",
		"GET /api/mj/",
		"GET /api/task",
		"GET /api/system-task/list",
		"GET /api/system-info/instances",
		"GET /api/deployments/",
	} {
		_, registered := routes[route]
		assert.False(t, registered, route)
	}
}

func TestSetApiRouterRegistersTaskRoutesForTaskPlugins(t *testing.T) {
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "true")
	t.Setenv("MODELPORT_ENABLE_MEDIA_TASKS", "false")

	engine := gin.New()
	SetApiRouter(engine)

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	_, taskRegistered := routes[http.MethodGet+" /api/task"]
	assert.True(t, taskRegistered)
	_, mjRegistered := routes[http.MethodGet+" /api/mj/"]
	assert.False(t, mjRegistered)
}
