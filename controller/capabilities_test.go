package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCapabilitiesReturnsPersonalModeMatrix(t *testing.T) {
	originalSelfUse := operation_setting.SelfUseModeEnabled
	originalDemo := operation_setting.DemoSiteEnabled
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUse
		operation_setting.DemoSiteEnabled = originalDemo
	})
	operation_setting.SelfUseModeEnabled = true
	operation_setting.DemoSiteEnabled = false

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)

	GetCapabilities(context)

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			UsageMode string          `json:"usage_mode"`
			Features  map[string]bool `json:"features"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "personal", payload.Data.UsageMode)
	assert.False(t, payload.Data.Features["registration"])
	assert.False(t, payload.Data.Features["payments"])
	assert.True(t, payload.Data.Features["core_relay"])
	assert.True(t, payload.Data.Features["channel_management"])
}
