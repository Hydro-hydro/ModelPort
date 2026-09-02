package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireFeatureBlocksDisabledPersonalFeature(t *testing.T) {
	originalSelfUse := operation_setting.SelfUseModeEnabled
	originalDemo := operation_setting.DemoSiteEnabled
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUse
		operation_setting.DemoSiteEnabled = originalDemo
	})
	operation_setting.SelfUseModeEnabled = true
	operation_setting.DemoSiteEnabled = false

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/registration", RequireFeature(usage_mode.FeatureRegistration), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/registration", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "FEATURE_DISABLED")
	assert.Contains(t, recorder.Body.String(), "registration")
}

func TestRequireFeatureKeepsFeatureAvailableOutsidePersonalMode(t *testing.T) {
	originalSelfUse := operation_setting.SelfUseModeEnabled
	originalDemo := operation_setting.DemoSiteEnabled
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUse
		operation_setting.DemoSiteEnabled = originalDemo
	})
	operation_setting.SelfUseModeEnabled = false
	operation_setting.DemoSiteEnabled = false

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/registration", RequireFeature(usage_mode.FeatureRegistration), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/registration", nil)
	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
}
