package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

func TestUpdateChannelStatusPersistsMultiKeyState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "multi-key-status",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	changed := UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key")
	require.True(t, changed)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.NotZero(t, stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveStatusStateFromSingleKeySnapshotPreservesUnownedColumns(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:        "single-key-status",
		Key:         "original-key",
		Status:      common.ChannelStatusEnabled,
		Models:      "original-model",
		Group:       "default",
		UsedQuota:   100,
		ChannelInfo: ChannelInfo{},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)

	concurrentChannelInfo := ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 1,
	}
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"key":          "rotated-key",
		"used_quota":   gorm.Expr("used_quota + ?", 250),
		"models":       "concurrent-model",
		"channel_info": concurrentChannelInfo,
	}).Error)

	stale.Status = common.ChannelStatusManuallyDisabled
	stale.SetOtherInfo(map[string]interface{}{
		"status_reason": "manual operation",
		"status_time":   int64(1234),
	})
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, "rotated-key", stored.Key)
	assert.Equal(t, int64(350), stored.UsedQuota)
	assert.Equal(t, "concurrent-model", stored.Models)
	assert.Equal(t, concurrentChannelInfo, stored.ChannelInfo)

	otherInfo := stored.GetOtherInfo()
	assert.Equal(t, "manual operation", otherInfo["status_reason"])
	assert.Equal(t, float64(1234), otherInfo["status_time"])
}

func TestInitChannelCacheUsesEnabledAbilitiesAndChannelStatus(t *testing.T) {
	truncateTables(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		if previousMemoryCacheEnabled {
			InitChannelCache()
		}
	})

	channels := []Channel{
		{Id: 101, Name: "enabled", Key: "key-101", Status: common.ChannelStatusEnabled, Models: "cache-model", Group: "default"},
		{Id: 102, Name: "ability-disabled", Key: "key-102", Status: common.ChannelStatusEnabled, Models: "cache-model", Group: "default"},
		{Id: 103, Name: "channel-disabled", Key: "key-103", Status: common.ChannelStatusManuallyDisabled, Models: "cache-model", Group: "default"},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(&channel).Error)
	}
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "cache-model", ChannelId: 101, Enabled: true}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "cache-model", ChannelId: 102, Enabled: false}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "cache-model", ChannelId: 103, Enabled: true}).Error)

	InitChannelCache()
	selected, err := GetRandomSatisfiedChannel("default", "cache-model", 0, nil)

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 101, selected.Id)
}

func TestEnabledAbilityQueriesExcludeDisabledChannels(t *testing.T) {
	setupChannelStatusTest(t)

	enabled := Channel{Id: 121, Name: "enabled-query", Key: "key-121", Status: common.ChannelStatusEnabled}
	disabled := Channel{Id: 122, Name: "disabled-query", Key: "key-122", Status: common.ChannelStatusManuallyDisabled}
	require.NoError(t, DB.Create(&enabled).Error)
	require.NoError(t, DB.Create(&disabled).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "query-enabled", ChannelId: enabled.Id, Enabled: true}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "query-disabled", ChannelId: disabled.Id, Enabled: true}).Error)

	assert.Equal(t, []string{"query-enabled"}, GetGroupEnabledModels("default"))
	assert.Equal(t, []string{"query-enabled"}, GetEnabledModels())
	abilities := GetAllEnableAbilities()
	require.Len(t, abilities, 1)
	assert.Equal(t, enabled.Id, abilities[0].ChannelId)
}

func TestUpdateChannelStatusRestoresEnabledRouteAfterDisable(t *testing.T) {
	truncateTables(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		if previousMemoryCacheEnabled {
			InitChannelCache()
		}
	})

	channel := Channel{
		Id:     111,
		Name:   "status-cache",
		Key:    "key-111",
		Status: common.ChannelStatusEnabled,
		Models: "cache-model",
		Group:  "default",
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "cache-model", ChannelId: channel.Id, Enabled: true}).Error)
	InitChannelCache()

	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual"))
	selected, err := GetRandomSatisfiedChannel("default", "cache-model", 0, nil)
	require.NoError(t, err)
	assert.Nil(t, selected)

	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, "restore"))
	selected, err = GetRandomSatisfiedChannel("default", "cache-model", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, channel.Id, selected.Id)
}

func TestAbilityUpdatesRefreshRouteIndex(t *testing.T) {
	truncateTables(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		if previousMemoryCacheEnabled {
			InitChannelCache()
		}
	})

	tag := "ability-cache-tag"
	channel := Channel{
		Id:     514,
		Name:   "ability-cache",
		Key:    "key-514",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "ability-cache-model",
		ChannelId: channel.Id,
		Enabled:   true,
		Tag:       &tag,
	}).Error)
	InitChannelCache()

	selected, err := GetRandomSatisfiedChannel("default", "ability-cache-model", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)

	require.NoError(t, UpdateAbilityStatus(channel.Id, false))
	selected, err = GetRandomSatisfiedChannel("default", "ability-cache-model", 0, nil)
	require.NoError(t, err)
	assert.Nil(t, selected)

	require.NoError(t, UpdateAbilityStatusByTag(tag, true))
	selected, err = GetRandomSatisfiedChannel("default", "ability-cache-model", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, channel.Id, selected.Id)
}

func TestBatchSetChannelTagRecreatesAbilitiesInTransaction(t *testing.T) {
	setupChannelStatusTest(t)
	channel := Channel{
		Id:     515,
		Name:   "batch-tag",
		Key:    "key-515",
		Status: common.ChannelStatusEnabled,
		Models: "batch-model",
		Group:  "default",
	}
	oldTag := "old-tag"
	channel.Tag = &oldTag
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "batch-model", ChannelId: channel.Id,
		Enabled: true, Tag: &oldTag,
	}).Error)

	newTag := "new-tag"
	require.NoError(t, BatchSetChannelTag([]int{channel.Id}, &newTag))

	var ability Ability
	require.NoError(t, DB.First(&ability, "channel_id = ?", channel.Id).Error)
	require.NotNil(t, ability.Tag)
	assert.Equal(t, newTag, *ability.Tag)
}

func TestCacheReadsReturnIndependentChannelSnapshots(t *testing.T) {
	truncateTables(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		if previousMemoryCacheEnabled {
			InitChannelCache()
		}
	})

	channel := Channel{
		Id:     512,
		Name:   "snapshot-channel",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyStatusList:   map[int]int{1: common.ChannelStatusAutoDisabled},
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "snapshot-model", ChannelId: channel.Id, Enabled: true}).Error)
	InitChannelCache()

	first, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	first.Status = common.ChannelStatusManuallyDisabled
	first.ChannelInfo.MultiKeyStatusList[0] = common.ChannelStatusAutoDisabled
	first.Keys[0] = "mutated"

	info, err := CacheGetChannelInfo(channel.Id)
	require.NoError(t, err)
	info.MultiKeyStatusList[2] = common.ChannelStatusAutoDisabled

	second, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, second.Status)
	assert.NotContains(t, second.ChannelInfo.MultiKeyStatusList, 0)
	assert.NotContains(t, second.ChannelInfo.MultiKeyStatusList, 2)
	assert.Equal(t, "key-a", second.Keys[0])
}

func TestCacheUpdateChannelRebuildsAbilityRoute(t *testing.T) {
	truncateTables(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		if previousMemoryCacheEnabled {
			InitChannelCache()
		}
	})

	channel := Channel{
		Id:     513,
		Name:   "incremental-cache",
		Key:    "key-513",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "incremental-model", ChannelId: channel.Id, Enabled: true}).Error)
	InitChannelCache()

	selected, err := GetRandomSatisfiedChannel("default", "incremental-model", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)

	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", false).Error)
	updated, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	CacheUpdateChannel(updated)
	selected, err = GetRandomSatisfiedChannel("default", "incremental-model", 0, nil)
	require.NoError(t, err)
	assert.Nil(t, selected)

	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", true).Error)
	updated, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	CacheUpdateChannel(updated)
	selected, err = GetRandomSatisfiedChannel("default", "incremental-model", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, channel.Id, selected.Id)
}
