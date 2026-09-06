package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/stretchr/testify/require"
)

func TestValidateTaskPluginChannel(t *testing.T) {
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "true")

	source := `
export const meta = {apiVersion: 1, key: "channel-validation", name: "Validation", version: "1.0.0", author: {name: "Test"}, models: ["doc"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister("channel-validation") })
	baseURL := "https://example.com"

	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin, BaseURL: &baseURL}
	require.ErrorContains(t, validateChannel(channel, false), "task plugin key is required")

	missing := `{"task_plugin_key":"missing"}`
	channel.Setting = &missing
	require.ErrorContains(t, validateChannel(channel, false), "is not registered")

	longKey := `{"task_plugin_key":"` + strings.Repeat("x", 31) + `"}`
	channel.Setting = &longKey
	require.ErrorContains(t, validateChannel(channel, false), "must not exceed 30")

	valid := `{"task_plugin_key":"channel-validation"}`
	channel.Setting = &valid
	channel.BaseURL = nil
	require.ErrorContains(t, validateChannel(channel, false), "base URL is required")
}

func TestValidateTaskPluginChannelRejectsDisabledFeature(t *testing.T) {
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "false")

	baseURL := "https://example.com"
	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin, BaseURL: &baseURL}
	require.ErrorContains(t, validateChannel(channel, false), "task plugin feature is disabled")
}

func TestValidateMediaTaskChannelRejectsWhenOptionalFeaturesAreDisabled(t *testing.T) {
	usage_mode.SetPersistedOptionalFeatures(nil)
	t.Setenv("MODELPORT_ENABLE_MEDIA_TASKS", "false")
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "false")
	t.Cleanup(func() { usage_mode.SetPersistedOptionalFeatures(nil) })

	mediaTypes := []int{
		constant.ChannelTypeMidjourney,
		constant.ChannelTypeMidjourneyPlus,
		constant.ChannelTypeSunoAPI,
		constant.ChannelTypeKling,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeVidu,
		constant.ChannelTypeDoubaoVideo,
		constant.ChannelTypeSora,
	}
	for _, channelType := range mediaTypes {
		t.Run(constant.GetChannelTypeName(channelType), func(t *testing.T) {
			channel := &model.Channel{Type: channelType}
			require.ErrorContains(t, validateChannel(channel, false), "media task feature is disabled")
		})
	}
}

func TestValidateMediaTaskChannelAllowsTaskPluginFeature(t *testing.T) {
	usage_mode.SetPersistedOptionalFeatures(nil)
	t.Setenv("MODELPORT_ENABLE_MEDIA_TASKS", "false")
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "true")
	t.Cleanup(func() { usage_mode.SetPersistedOptionalFeatures(nil) })

	require.NoError(t, validateChannel(&model.Channel{Type: constant.ChannelTypeKling}, false))
}
