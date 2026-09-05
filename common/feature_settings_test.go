package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/stretchr/testify/assert"
)

func TestRefreshFeatureDerivedSettingsUsesPersistedFeatureState(t *testing.T) {
	originalUpdateTask := constant.UpdateTask
	originalTaskPluginEnabled := constant.TaskPluginEnabled
	originalTaskPluginOverrideEnabled := constant.TaskPluginOverrideEnabled
	usage_mode.SetPersistedOptionalFeatures(nil)
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "false")
	t.Setenv("UPDATE_TASK", "")
	t.Setenv("TASK_PLUGIN_OVERRIDE_ENABLED", "true")
	t.Cleanup(func() {
		constant.UpdateTask = originalUpdateTask
		constant.TaskPluginEnabled = originalTaskPluginEnabled
		constant.TaskPluginOverrideEnabled = originalTaskPluginOverrideEnabled
		usage_mode.SetPersistedOptionalFeatures(nil)
	})

	RefreshFeatureDerivedSettings()
	assert.False(t, constant.TaskPluginEnabled)
	assert.False(t, constant.UpdateTask)

	usage_mode.SetPersistedOptionalFeatures(map[usage_mode.Feature]bool{
		usage_mode.FeatureTaskPlugins: true,
	})
	RefreshFeatureDerivedSettings()
	assert.True(t, constant.TaskPluginEnabled)
	assert.True(t, constant.TaskPluginOverrideEnabled)
	assert.True(t, constant.UpdateTask)
}
