package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireFeatureBlocksDisabledPersonalFeature(t *testing.T) {
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

func TestRequireFeatureAllowsEnabledPersonalFeature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/relay", RequireFeature(usage_mode.FeatureCoreRelay), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/relay", nil)
	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
}
