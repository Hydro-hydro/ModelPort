package model

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
// channel2advancedCustomConfig caches parsed Advanced Custom (type 58) configs so
// path-aware selection avoids re-parsing JSON per request. Refreshed on full sync.
var channel2advancedCustomConfig map[int]*kitdto.AdvancedCustomConfig
var channelSyncLock sync.RWMutex

// Serializes database-backed cache rebuilds with incremental channel updates so
// an older DB snapshot cannot publish after a newer one.
var channelCacheUpdateLock sync.Mutex

// cloneChannel returns an ownership-safe snapshot of a cached channel. The
// cache stores mutable DTOs (ChannelInfo maps and key slices in particular), so
// returning the cache's pointer would let request handlers race with cache
// refreshes or status updates after the read lock is released.
func cloneChannel(channel *Channel) *Channel {
	if channel == nil {
		return nil
	}
	cloned := *channel
	cloned.OpenAIOrganization = cloneStringPtr(channel.OpenAIOrganization)
	cloned.TestModel = cloneStringPtr(channel.TestModel)
	cloned.BaseURL = cloneStringPtr(channel.BaseURL)
	cloned.Weight = cloneUintPtr(channel.Weight)
	cloned.ModelMapping = cloneStringPtr(channel.ModelMapping)
	cloned.StatusCodeMapping = cloneStringPtr(channel.StatusCodeMapping)
	cloned.Priority = cloneInt64Ptr(channel.Priority)
	cloned.AutoBan = cloneIntPtr(channel.AutoBan)
	cloned.Tag = cloneStringPtr(channel.Tag)
	cloned.Setting = cloneStringPtr(channel.Setting)
	cloned.ParamOverride = cloneStringPtr(channel.ParamOverride)
	cloned.HeaderOverride = cloneStringPtr(channel.HeaderOverride)
	cloned.Remark = cloneStringPtr(channel.Remark)
	cloned.Keys = append([]string(nil), channel.Keys...)
	cloned.ChannelInfo = cloneChannelInfo(channel.ChannelInfo)
	return &cloned
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneUintPtr(value *uint) *uint {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneChannelInfo(info ChannelInfo) ChannelInfo {
	cloned := info
	if info.MultiKeyStatusList != nil {
		cloned.MultiKeyStatusList = make(map[int]int, len(info.MultiKeyStatusList))
		for index, status := range info.MultiKeyStatusList {
			cloned.MultiKeyStatusList[index] = status
		}
	}
	if info.MultiKeyDisabledReason != nil {
		cloned.MultiKeyDisabledReason = make(map[int]string, len(info.MultiKeyDisabledReason))
		for index, reason := range info.MultiKeyDisabledReason {
			cloned.MultiKeyDisabledReason[index] = reason
		}
	}
	if info.MultiKeyDisabledTime != nil {
		cloned.MultiKeyDisabledTime = make(map[int]int64, len(info.MultiKeyDisabledTime))
		for index, timestamp := range info.MultiKeyDisabledTime {
			cloned.MultiKeyDisabledTime[index] = timestamp
		}
	}
	return cloned
}

func cloneAdvancedCustomConfig(config *kitdto.AdvancedCustomConfig) *kitdto.AdvancedCustomConfig {
	if config == nil {
		return nil
	}
	cloned := *config
	cloned.Routes = make([]kitdto.AdvancedCustomRoute, len(config.Routes))
	for index, route := range config.Routes {
		cloned.Routes[index] = route
		cloned.Routes[index].Models = append([]string(nil), route.Models...)
		if route.Auth != nil {
			auth := *route.Auth
			cloned.Routes[index].Auth = &auth
		}
	}
	return &cloned
}

func InitChannelCache() {
	channelCacheUpdateLock.Lock()
	defer channelCacheUpdateLock.Unlock()
	if !common.MemoryCacheEnabled {
		InvalidatePricingCache()
		rebuildTaskAliasView()
		return
	}
	newChannelId2channel := make(map[int]*Channel)
	newChannel2advancedCustomConfig := make(map[int]*kitdto.AdvancedCustomConfig)
	var channels []*Channel
	if DB == nil {
		return
	}
	if err := DB.Find(&channels).Error; err != nil {
		common.SysLog("channels cache sync skipped: " + err.Error())
		return
	}
	for _, channel := range channels {
		cachedChannel := cloneChannel(channel)
		newChannelId2channel[channel.Id] = cachedChannel
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
				newChannel2advancedCustomConfig[channel.Id] = cloneAdvancedCustomConfig(config)
			}
		}
	}
	var abilities []*Ability
	if err := DB.Find(&abilities).Error; err != nil {
		common.SysLog("abilities cache sync skipped: " + err.Error())
		return
	}
	newGroup2model2channels := make(map[string]map[string][]int)
	for _, ability := range abilities {
		if !ability.Enabled {
			continue
		}
		channel, ok := newChannelId2channel[ability.ChannelId]
		if !ok || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		model2channels := newGroup2model2channels[ability.Group]
		if model2channels == nil {
			model2channels = make(map[string][]int)
			newGroup2model2channels[ability.Group] = model2channels
		}
		model2channels[ability.Model] = append(model2channels[ability.Model], ability.ChannelId)
	}

	// sort by priority
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			sort.Slice(channels, func(i, j int) bool {
				return newChannelId2channel[channels[i]].GetPriority() > newChannelId2channel[channels[j]].GetPriority()
			})
			newGroup2model2channels[group][model] = channels
		}
	}

	channelSyncLock.Lock()
	group2model2channels = newGroup2model2channels
	//channelsIDM = newChannelId2channel
	for i, channel := range newChannelId2channel {
		if channel.ChannelInfo.IsMultiKey {
			channel.Keys = channel.GetKeys()
			if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
				if oldChannel, ok := channelsIDM[i]; ok {
					// 存在旧的渠道，如果是多key且轮询，保留轮询索引信息
					if oldChannel.ChannelInfo.IsMultiKey && oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
						channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
					}
				}
			}
		}
	}
	channelsIDM = newChannelId2channel
	channel2advancedCustomConfig = newChannel2advancedCustomConfig
	channelSyncLock.Unlock()
	// Lock ordering: InvalidatePricingCache acquires updatePricingLock, and
	// GetPricing (holding updatePricingLock) nests channelSyncLock.RLock via
	// loadPricingAdvancedCustomConfigs. channelSyncLock MUST be released before
	// invalidating the pricing cache, otherwise the reversed order deadlocks.
	InvalidatePricingCache()
	rebuildTaskAliasView()
	common.SysLog("channels synced from database")
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

func GetRandomSatisfiedChannel(
	group string,
	model string,
	retry int,
	filters []dto.ChannelFilter,
) (*Channel, error) {
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		return GetChannel(group, model, retry, filters)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	// First, try to find channels with the exact model name.
	channels, _ := filterCandidateIDs(group2model2channels[group][model], model, filters)

	// If no channels found, try to find channels with the normalized model name.
	if len(channels) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		channels, _ = filterCandidateIDs(group2model2channels[group][normalizedModel], model, filters)
	}

	if len(channels) == 0 {
		return nil, nil
	}

	if len(channels) == 1 {
		if channel, ok := channelsIDM[channels[0]]; ok {
			return cloneChannel(channel), nil
		}
		return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channels[0])
	}

	uniquePriorities := make(map[int]bool)
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			uniquePriorities[int(channel.GetPriority())] = true
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}
	var sortedUniquePriorities []int
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry >= len(uniquePriorities) {
		retry = len(uniquePriorities) - 1
	}
	targetPriority := int64(sortedUniquePriorities[retry])

	// get the priority for the given retry number
	var sumWeight = 0
	var targetChannels []*Channel
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			if channel.GetPriority() == targetPriority {
				sumWeight += channel.GetWeight()
				targetChannels = append(targetChannels, channel)
			}
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}

	if len(targetChannels) == 0 {
		return nil, errors.New(fmt.Sprintf("no channel found, group: %s, model: %s, priority: %d", group, model, targetPriority))
	}

	// smoothing factor and adjustment
	smoothingFactor := 1
	smoothingAdjustment := 0

	if sumWeight == 0 {
		// when all channels have weight 0, set sumWeight to the number of channels and set smoothing adjustment to 100
		// each channel's effective weight = 100
		sumWeight = len(targetChannels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(targetChannels) < 10 {
		// when the average weight is less than 10, set smoothing factor to 100
		smoothingFactor = 100
	}

	// Calculate the total weight of all channels up to endIdx
	totalWeight := sumWeight * smoothingFactor

	// Generate a random value in the range [0, totalWeight)
	randomWeight := rand.Intn(totalWeight)

	// Find a channel based on its weight
	for _, channel := range targetChannels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return cloneChannel(channel), nil
		}
	}
	// return null if no channel is not found
	return nil, errors.New("channel not found")
}

func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return cloneChannel(c), nil
}

func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		info := cloneChannelInfo(channel.ChannelInfo)
		return &info, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	info := cloneChannelInfo(c.ChannelInfo)
	return &info, nil
}

func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	if status == common.ChannelStatusEnabled {
		// Rebuild from persisted Channel and Ability state so an enabled channel
		// is only restored when its abilities are enabled as well.
		InitChannelCache()
		return
	}
	channelCacheUpdateLock.Lock()
	defer channelCacheUpdateLock.Unlock()
	channelSyncLock.Lock()
	changed := false
	if channel, ok := channelsIDM[id]; ok {
		// Never mutate the published snapshot. Readers may still be using it
		// after dropping the read lock, and status changes must be atomic from
		// their perspective.
		updated := cloneChannel(channel)
		updated.Status = status
		channelsIDM[id] = updated
		changed = channel.Status != status
	}
	removeChannelFromRouteIndexLocked(id)
	channelSyncLock.Unlock()
	if changed {
		InvalidatePricingCache()
	}
}

func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled || channel == nil {
		return
	}
	channelCacheUpdateLock.Lock()
	defer channelCacheUpdateLock.Unlock()

	// Ability.Enabled is part of the route contract, so an incremental cache
	// update must read the persisted abilities before publishing the channel.
	// A failed read leaves both the old channel and its route index untouched.
	if DB == nil {
		return
	}
	var abilities []Ability
	if err := DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error; err != nil {
		common.SysLog(fmt.Sprintf("channel cache update skipped: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	updated := cloneChannel(channel)

	channelSyncLock.Lock()

	if channelsIDM == nil {
		channelsIDM = make(map[int]*Channel)
	}
	if oldChannel, ok := channelsIDM[updated.Id]; ok {
		logger.LogDebug(nil, "CacheUpdateChannel before: id=%d, name=%s, status=%d, polling_index=%d", updated.Id, updated.Name, updated.Status, oldChannel.ChannelInfo.MultiKeyPollingIndex)
		if updated.ChannelInfo.IsMultiKey && updated.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling &&
			oldChannel.ChannelInfo.IsMultiKey && oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
			updated.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
		}
	}
	if updated.ChannelInfo.IsMultiKey {
		updated.Keys = updated.GetKeys()
	}
	channelsIDM[updated.Id] = updated
	if channel2advancedCustomConfig == nil {
		channel2advancedCustomConfig = make(map[int]*kitdto.AdvancedCustomConfig)
	}
	if group2model2channels == nil {
		group2model2channels = make(map[string]map[string][]int)
	}
	delete(channel2advancedCustomConfig, updated.Id)
	if updated.Type == constant.ChannelTypeAdvancedCustom {
		if config := updated.GetOtherSettings().AdvancedCustom; config != nil {
			channel2advancedCustomConfig[updated.Id] = cloneAdvancedCustomConfig(config)
		}
	}
	removeChannelFromRouteIndexLocked(updated.Id)
	if updated.Status == common.ChannelStatusEnabled {
		for _, ability := range abilities {
			if !ability.Enabled {
				continue
			}
			model2channels := group2model2channels[ability.Group]
			if model2channels == nil {
				model2channels = make(map[string][]int)
				group2model2channels[ability.Group] = model2channels
			}
			model2channels[ability.Model] = append(model2channels[ability.Model], updated.Id)
		}
		sortRouteIndexLocked()
	}
	logger.LogDebug(nil, "CacheUpdateChannel after: id=%d, name=%s, status=%d, polling_index=%d", updated.Id, updated.Name, updated.Status, updated.ChannelInfo.MultiKeyPollingIndex)
	// Lock ordering: do NOT hold channelSyncLock while calling
	// InvalidatePricingCache. GetPricing acquires updatePricingLock first and then
	// channelSyncLock.RLock (via loadPricingAdvancedCustomConfigs); acquiring
	// updatePricingLock while holding channelSyncLock would be an AB-BA deadlock.
	channelSyncLock.Unlock()
	InvalidatePricingCache()
}

func removeChannelFromRouteIndexLocked(id int) {
	for group, model2channels := range group2model2channels {
		for model, channelIDs := range model2channels {
			filtered := channelIDs[:0]
			for _, channelID := range channelIDs {
				if channelID != id {
					filtered = append(filtered, channelID)
				}
			}
			if len(filtered) == 0 {
				delete(model2channels, model)
			} else {
				model2channels[model] = filtered
			}
		}
		if len(model2channels) == 0 {
			delete(group2model2channels, group)
		}
	}
}

// updateCachedChannelPollingIndex publishes only the polling cursor while
// retaining the rest of the immutable channel snapshot.
func updateCachedChannelPollingIndex(id, index int) {
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	channel, ok := channelsIDM[id]
	if !ok {
		return
	}
	updated := cloneChannel(channel)
	updated.ChannelInfo.MultiKeyPollingIndex = index
	channelsIDM[id] = updated
}

func sortRouteIndexLocked() {
	for group, model2channels := range group2model2channels {
		for model, channelIDs := range model2channels {
			sort.SliceStable(channelIDs, func(i, j int) bool {
				left := channelsIDM[channelIDs[i]]
				right := channelsIDM[channelIDs[j]]
				if left == nil || right == nil {
					return left != nil
				}
				return left.GetPriority() > right.GetPriority()
			})
			model2channels[model] = channelIDs
		}
		group2model2channels[group] = model2channels
	}
}
