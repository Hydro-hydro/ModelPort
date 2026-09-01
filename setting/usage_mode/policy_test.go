package usage_mode

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentModePrecedence(t *testing.T) {
	originalSelfUse := operation_setting.SelfUseModeEnabled
	originalDemo := operation_setting.DemoSiteEnabled
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUse
		operation_setting.DemoSiteEnabled = originalDemo
	})

	operation_setting.SelfUseModeEnabled = false
	operation_setting.DemoSiteEnabled = false
	assert.Equal(t, ModeExternal, CurrentMode())

	operation_setting.SelfUseModeEnabled = true
	operation_setting.DemoSiteEnabled = false
	assert.Equal(t, ModePersonal, CurrentMode())

	operation_setting.DemoSiteEnabled = true
	assert.Equal(t, ModeDemo, CurrentMode())
}

func TestPersonalCapabilitiesDisablePlatformFeaturesOnly(t *testing.T) {
	originalSelfUse := operation_setting.SelfUseModeEnabled
	originalDemo := operation_setting.DemoSiteEnabled
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUse
		operation_setting.DemoSiteEnabled = originalDemo
	})

	operation_setting.SelfUseModeEnabled = true
	operation_setting.DemoSiteEnabled = false

	require.True(t, IsPersonalUse())
	assert.False(t, IsFeatureEnabled(FeatureRegistration))
	assert.False(t, IsFeatureEnabled(FeaturePayments))
	assert.False(t, IsFeatureEnabled(FeatureUserManagement))
	assert.True(t, IsFeatureEnabled(FeatureCoreRelay))
	assert.True(t, IsFeatureEnabled(FeatureChannelManagement))

	capabilities := Capabilities()
	assert.False(t, capabilities[string(FeatureSubscriptions)])
	assert.False(t, capabilities[string(FeatureDeployments)])
	assert.Len(t, capabilities, len(allFeatures))
}

func TestExternalAndDemoModesKeepFeaturesAvailable(t *testing.T) {
	originalSelfUse := operation_setting.SelfUseModeEnabled
	originalDemo := operation_setting.DemoSiteEnabled
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUse
		operation_setting.DemoSiteEnabled = originalDemo
	})

	for _, mode := range []Mode{ModeExternal, ModeDemo} {
		operation_setting.SelfUseModeEnabled = mode == ModePersonal
		operation_setting.DemoSiteEnabled = mode == ModeDemo
		assert.True(t, IsFeatureEnabled(FeaturePayments), "mode=%s", mode)
		assert.True(t, IsFeatureEnabled(FeatureRegistration), "mode=%s", mode)
	}
}
