package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/usage_mode"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine, assets WebAssets) {
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	if usage_mode.IsFeatureEnabled(usage_mode.FeatureTaskPlugins) {
		SetTaskPluginProtocolRouter(router)
		SetTaskRouter(router)
	}
	if usage_mode.IsFeatureEnabled(usage_mode.FeatureMediaTasks) {
		SetVideoRouter(router)
	}
	// The plugin registry is part of the optional task-plugin subsystem. Do not
	// initialize it (or load plugin routes) in the default personal gateway
	// mode; the no-op dispatcher simply lets the normal web fallback continue.
	var pluginDispatcher gin.HandlerFunc = func(c *gin.Context) { c.Next() }
	if usage_mode.IsFeatureEnabled(usage_mode.FeatureTaskPlugins) {
		pluginDispatcher = SetPluginRouter(router)
	}
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, assets, pluginDispatcher)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(
			pluginDispatcher,
			middleware.RouteTag("web"),
			func(c *gin.Context) {
				c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
			},
		)
	}
}
