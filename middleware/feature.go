package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/gin-gonic/gin"
)

// RequireFeature prevents access to functionality disabled by the instance's
// usage policy. The response deliberately uses 404 so disabled platform
// capabilities are not advertised by direct API probing.
func RequireFeature(feature usage_mode.Feature) gin.HandlerFunc {
	return func(c *gin.Context) {
		if usage_mode.IsFeatureEnabled(feature) {
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
			"success": false,
			"code":    "FEATURE_DISABLED",
			"feature": string(feature),
			"message": "当前运行模式未启用此功能",
		})
	}
}
