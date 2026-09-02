package usage_mode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentModeAlwaysPersonal(t *testing.T) {
	assert.Equal(t, ModePersonal, CurrentMode())
	assert.True(t, IsPersonalUse())
}

func TestPersonalCapabilitiesDisablePlatformFeaturesOnly(t *testing.T) {
	require.True(t, IsPersonalUse())
	assert.False(t, IsFeatureEnabled(FeatureRegistration))
	assert.False(t, IsFeatureEnabled(FeatureUserManagement))
	assert.True(t, IsFeatureEnabled(FeatureCoreRelay))
	assert.True(t, IsFeatureEnabled(FeatureChannelManagement))

	capabilities := Capabilities()
	for _, removed := range []string{"affiliation", "wallet", "payments", "subscriptions", "redemptions", "checkin", "pricing_portal", "rankings"} {
		assert.NotContains(t, capabilities, removed)
	}
	assert.True(t, capabilities[string(FeatureTaskPlugins)])
	assert.True(t, capabilities[string(FeatureDeployments)])
	assert.True(t, capabilities[string(FeatureMultiNode)])
	assert.Len(t, capabilities, len(allFeatures))
}
