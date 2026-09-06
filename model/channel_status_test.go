package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/usage_mode"
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

func TestOptionalChannelTypesStayOutOfRoutingAndModelLists(t *testing.T) {
	setupChannelStatusTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	usage_mode.SetPersistedOptionalFeatures(nil)
	t.Setenv("MODELPORT_ENABLE_MEDIA_TASKS", "false")
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "false")
	t.Cleanup(func() {
		usage_mode.SetPersistedOptionalFeatures(nil)
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		if previousMemoryCacheEnabled {
			InitChannelCache()
		}
	})

	channels := []Channel{
		{Id: 131, Name: "core", Key: "key-131", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled},
		{Id: 132, Name: "media", Key: "key-132", Type: constant.ChannelTypeKling, Status: common.ChannelStatusEnabled},
		{Id: 133, Name: "plugin", Key: "key-133", Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(&channel).Error)
	}
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "core-model", ChannelId: 131, Enabled: true},
		{Group: "default", Model: "media-model", ChannelId: 132, Enabled: true},
		{Group: "default", Model: "plugin-model", ChannelId: 133, Enabled: true},
	}).Error)

	InitChannelCache()
	selected, err := GetRandomSatisfiedChannel("default", "core-model", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 131, selected.Id)
	selected, err = GetRandomSatisfiedChannel("default", "media-model", 0, nil)
	require.NoError(t, err)
	assert.Nil(t, selected)
	selected, err = GetRandomSatisfiedChannel("default", "plugin-model", 0, nil)
	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, []string{"core-model"}, GetEnabledModels())

	common.MemoryCacheEnabled = false
	selected, err = GetChannel("default", "media-model", 0, nil)
	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.False(t, IsChannelEnabledForGroupModel("default", "media-model", 132))
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

func TestIsChannelEnabledForGroupModelRequiresEnabledChannelAndAbility(t *testing.T) {
	setupChannelStatusTest(t)

	enabled := Channel{Id: 123, Name: "enabled-helper", Key: "key-123", Status: common.ChannelStatusEnabled}
	disabled := Channel{Id: 124, Name: "disabled-helper", Key: "key-124", Status: common.ChannelStatusManuallyDisabled}
	require.NoError(t, DB.Create(&enabled).Error)
	require.NoError(t, DB.Create(&disabled).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "helper-model", ChannelId: enabled.Id, Enabled: true},
		{Group: "default", Model: "disabled-ability", ChannelId: enabled.Id, Enabled: false},
		{Group: "default", Model: "helper-model", ChannelId: disabled.Id, Enabled: true},
	}).Error)

	assert.True(t, IsChannelEnabledForGroupModel("default", "helper-model", enabled.Id))
	assert.False(t, IsChannelEnabledForGroupModel("default", "disabled-ability", enabled.Id))
	assert.False(t, IsChannelEnabledForGroupModel("default", "helper-model", disabled.Id))
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

func TestChannelStatusChangesPreservePerAbilityEnabledState(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "status-ability-tag"
	channel := Channel{
		Id:     112,
		Name:   "status-ability-preserve",
		Key:    "key-112",
		Status: common.ChannelStatusEnabled,
		Tag:    &tag,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "kept-enabled", ChannelId: channel.Id, Enabled: true, Tag: &tag},
		{Group: "default", Model: "kept-disabled", ChannelId: channel.Id, Enabled: false, Tag: &tag},
	}).Error)

	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual"))
	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, "restore"))

	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Order("model").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.False(t, abilities[0].Enabled, "disabled model ability must remain disabled after channel restore")
	assert.True(t, abilities[1].Enabled, "enabled model ability must remain enabled after channel restore")

	require.NoError(t, DisableChannelByTag(tag))
	require.NoError(t, EnableChannelByTag(tag))
	abilities = nil
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Order("model").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.False(t, abilities[0].Enabled, "tag channel toggle must preserve disabled model ability")
	assert.True(t, abilities[1].Enabled, "tag channel toggle must preserve enabled model ability")
}

func TestChannelUpdatePreservesExistingAbilityEnabledState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Id:     113,
		Name:   "ability-update-preserve",
		Key:    "key-113",
		Status: common.ChannelStatusEnabled,
		Models: "existing-model",
		Group:  "default",
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "existing-model", ChannelId: channel.Id, Enabled: false,
	}).Error)

	channel.Models = "existing-model,new-model"
	require.NoError(t, channel.Update())

	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Order("model").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.False(t, abilities[0].Enabled, "existing disabled ability must survive channel model edits")
	assert.True(t, abilities[1].Enabled, "new ability should inherit enabled channel state")
}

func TestFixAbilityRebuildsWithoutDroppingAbilityState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Id:     114,
		Name:   "fix-ability",
		Key:    "key-114",
		Status: common.ChannelStatusEnabled,
		Models: "repair-model",
		Group:  "default",
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "repair-model", ChannelId: channel.Id, Enabled: false},
		{Group: "orphan", Model: "orphan-model", ChannelId: 999, Enabled: true},
	}).Error)

	success, failures, err := FixAbility()
	require.NoError(t, err)
	assert.Equal(t, 1, success)
	assert.Zero(t, failures)

	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.False(t, abilities[0].Enabled, "repair must preserve an explicitly disabled ability")
	var orphanCount int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", 999).Count(&orphanCount).Error)
	assert.Zero(t, orphanCount)
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
