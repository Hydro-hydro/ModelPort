package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetRouteGroups(c *gin.Context) {
	baseGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if baseGroup == "" {
		baseGroup = c.GetString("group")
	}
	if baseGroup == "" {
		baseGroup, _ = model.GetUserGroup(c.GetInt("id"), false)
	}
	usableGroups := make(map[string]map[string]interface{})
	for groupName := range service.GetPersonalUsableGroups() {
		usableGroups[groupName] = map[string]interface{}{
			"ratio": service.GetRouteGroupRatio(baseGroup, groupName),
			"desc":  groupName,
		}
	}
	if autoGroups := service.GetPersonalAutoGroups(); len(autoGroups) > 0 {
		usableGroups["auto"] = map[string]interface{}{
			"ratio": "自动",
			"desc":  "自动分组",
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
