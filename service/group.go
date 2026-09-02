package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// GetPersonalUsableGroups returns every configured route group. Personal mode
// has one owner, so route-group availability is no longer derived from an
// account's user-group permissions.
func GetPersonalUsableGroups() map[string]string {
	groups := make(map[string]string)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groups[groupName] = groupName
	}
	return groups
}

// IsSelectableRouteGroup reports whether a concrete configured route group can
// be assigned to a token or selected by the playground.
func IsSelectableRouteGroup(groupName string) bool {
	return groupName != "" && groupName != "auto" && ratio_setting.ContainsGroupRatio(groupName)
}

// GetPersonalAutoGroups returns the configured automatic routing order after
// removing groups that no longer exist in the pricing configuration.
func GetPersonalAutoGroups() []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range setting.GetAutoGroups() {
		if !IsSelectableRouteGroup(group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

// FilterTokenAutoGroups applies route-group existence checks before the
// per-token limit. It intentionally does not fall back to the global Auto
// list when a token has an explicit snapshot.
func FilterTokenAutoGroups(groups []string) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsSelectableRouteGroup(group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups for the current token.
// The absence of the context value means that the token inherits the complete
// global Auto list; a present (even empty) value is an explicit token snapshot.
func GetRequestAutoGroups(c *gin.Context) []string {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetPersonalAutoGroups()
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterTokenAutoGroups(groups)
}

// GetGroupsEnabledModels 按 groups 顺序获取各分组启用的模型并去重
func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; !ok {
				seen[modelName] = struct{}{}
				models = append(models, modelName)
			}
		}
	}
	return models
}

// GetRouteGroupRatio returns the effective ratio for a route group while
// preserving the existing cross-group pricing override behavior.
func GetRouteGroupRatio(baseGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(baseGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
