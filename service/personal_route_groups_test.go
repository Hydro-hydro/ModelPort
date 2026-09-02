package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPersonalUsableGroupsUsesConfiguredRouteGroups(t *testing.T) {
	originalRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":0.5}`))

	assert.Equal(t, map[string]string{
		"default": "default",
		"premium": "premium",
	}, GetPersonalUsableGroups())
}

func TestGetPersonalAutoGroupsFiltersUnknownAndDuplicateRouteGroups(t *testing.T) {
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["premium","missing","premium","default"]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":0.5}`))

	assert.Equal(t, []string{"premium", "default"}, GetPersonalAutoGroups())
}

func TestFilterTokenAutoGroupsUsesRouteGroupsAndLimit(t *testing.T) {
	originalMax := setting.GetMaxTokenAutoGroups()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", originalMax)))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})

	require.NoError(t, setting.UpdateMaxTokenAutoGroups("2"))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":0.5}`))

	assert.Equal(t, []string{"premium", "default"}, FilterTokenAutoGroups([]string{"missing", "premium", "default", "premium"}))
}

func TestGetRouteGroupRatioKeepsCrossGroupOverride(t *testing.T) {
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalOverrides := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalOverrides))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":0.5}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"default":{"premium":0.3}}`))

	assert.Equal(t, 0.3, GetRouteGroupRatio("default", "premium"))
	assert.Equal(t, 1.0, GetRouteGroupRatio("default", "premium-missing"))
}
