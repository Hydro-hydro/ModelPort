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

func TestPersonalCapabilitiesUseExplicitAllowlist(t *testing.T) {
	require.True(t, IsPersonalUse())
	assert.False(t, IsFeatureEnabled(FeatureRegistration))
	assert.False(t, IsFeatureEnabled(FeatureUserManagement))
	assert.True(t, IsFeatureEnabled(FeatureCoreRelay))
	assert.True(t, IsFeatureEnabled(FeatureChannelManagement))

	capabilities := Capabilities()
	for _, removed := range []string{"affiliation", "wallet", "payments", "subscriptions", "redemptions", "checkin", "pricing_portal", "rankings"} {
		assert.NotContains(t, capabilities, removed)
	}
	assert.False(t, capabilities[string(FeatureMediaTasks)])
	assert.False(t, capabilities[string(FeatureTaskPlugins)])
	assert.False(t, capabilities[string(FeatureSystemTasks)])
	assert.False(t, capabilities[string(FeatureDeployments)])
	assert.False(t, capabilities[string(FeatureMultiNode)])
	assert.False(t, IsFeatureEnabled(Feature("future_platform_feature")))
	assert.Len(t, capabilities, len(allFeatures))
}

func TestOptionalPersonalFeaturesAreOptIn(t *testing.T) {
	SetPersistedOptionalFeatures(nil)
	t.Setenv("MODELPORT_ENABLE_SYSTEM_TASKS", "true")
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "true")
	t.Setenv("MODELPORT_ENABLE_MEDIA_TASKS", "false")

	assert.True(t, IsFeatureEnabled(FeatureSystemTasks))
	assert.True(t, IsFeatureEnabled(FeatureTaskPlugins))
	assert.False(t, IsFeatureEnabled(FeatureMediaTasks))
	assert.False(t, IsFeatureEnabled(FeatureDeployments))
	assert.False(t, IsFeatureEnabled(FeatureMultiNode))
}

func TestTaskFeaturesImplicitlyEnableSystemTasks(t *testing.T) {
	SetPersistedOptionalFeatures(nil)
	t.Setenv("MODELPORT_ENABLE_SYSTEM_TASKS", "false")
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "true")
	t.Setenv("MODELPORT_ENABLE_MEDIA_TASKS", "false")

	assert.True(t, IsFeatureEnabled(FeatureTaskPlugins))
	assert.True(t, IsFeatureEnabled(FeatureSystemTasks))
}

func TestPersistedOptionalFeatureOverridesEnvironmentUntilRestart(t *testing.T) {
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "true")
	SetPersistedOptionalFeatures(map[Feature]bool{FeatureTaskPlugins: false})
	t.Cleanup(func() { SetPersistedOptionalFeatures(nil) })

	assert.False(t, IsFeatureEnabled(FeatureTaskPlugins))
}

func TestTaskFeatureDependencyUsesPersistedConfiguration(t *testing.T) {
	t.Setenv("MODELPORT_ENABLE_SYSTEM_TASKS", "false")
	SetPersistedOptionalFeatures(map[Feature]bool{FeatureTaskPlugins: true})
	t.Cleanup(func() { SetPersistedOptionalFeatures(nil) })

	assert.True(t, IsFeatureEnabled(FeatureTaskPlugins))
	assert.True(t, IsFeatureEnabled(FeatureSystemTasks))
}

func TestOptionalFeatureOptionKeysAreStable(t *testing.T) {
	assert.Equal(t,
		[]string{
			"feature.system_tasks",
			"feature.media_tasks",
			"feature.task_plugins",
			"feature.deployments",
			"feature.multi_node",
		},
		OptionalFeatureOptionKeys(),
	)
	assert.True(t, IsOptionalFeatureOptionKey("feature.task_plugins"))
	assert.False(t, IsOptionalFeatureOptionKey("feature.unknown"))
}
