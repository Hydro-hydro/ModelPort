package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/usage_mode"

	"github.com/gin-gonic/gin"
)

func TestStatus(c *gin.Context) {
	err := model.PingDB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "数据库连接失败",
		})
		return
	}
	// 获取HTTP统计信息
	httpStats := middleware.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "Server is running",
		"http_stats": httpStats,
	})
	return
}

func GetCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"usage_mode": string(usage_mode.CurrentMode()),
			"features":   usage_mode.Capabilities(),
		},
	})
}

func GetStatus(c *gin.Context) {

	cs := console_setting.GetConsoleSetting()
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	data := gin.H{
		"version":            common.Version,
		"start_time":         common.StartTime,
		"theme":              "default",
		"system_name":        common.SystemName,
		"logo":               common.Logo,
		"server_address":     system_setting.ServerAddress,
		"turnstile_check":    common.TurnstileCheckEnabled,
		"turnstile_site_key": common.TurnstileSiteKey,
		"docs_link":          operation_setting.GetGeneralSetting().DocsLink,
		"quota_per_unit":     common.QuotaPerUnit,
		// 兼容旧前端：保留 display_in_currency，同时提供新的 quota_display_type
		"display_in_currency":           operation_setting.IsCurrencyDisplay(),
		"quota_display_type":            operation_setting.GetQuotaDisplayType(),
		"custom_currency_symbol":        operation_setting.GetGeneralSetting().CustomCurrencySymbol,
		"custom_currency_exchange_rate": operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate,
		"enable_batch_update":           common.BatchUpdateEnabled,
		"enable_drawing":                common.DrawingEnabled,
		"enable_task":                   usage_mode.IsFeatureEnabled(usage_mode.FeatureMediaTasks) || usage_mode.IsFeatureEnabled(usage_mode.FeatureTaskPlugins),
		"enable_data_export":            common.DataExportEnabled,
		"data_export_default_time":      common.DataExportDefaultTime,
		"mj_notify_enabled":             setting.MjNotifyEnabled,
		"chats":                         setting.Chats,
		"usage_mode":                    string(usage_mode.CurrentMode()),
		"features":                      usage_mode.Capabilities(),
		"default_use_auto_group":        setting.GetDefaultUseAutoGroup(),

		"password_login_encryption_enabled": common.PasswordLoginEncryptionEnabled,

		// 面板启用开关
		"api_info_enabled":    cs.ApiInfoEnabled,
		"uptime_kuma_enabled": cs.UptimeKumaEnabled,
		"faq_enabled":         cs.FAQEnabled,

		// 侧边栏模块配置
		"SidebarModulesAdmin": common.OptionMap["SidebarModulesAdmin"],

		"setup": constant.Setup,
	}

	// 根据启用状态注入可选内容
	if cs.ApiInfoEnabled {
		data["api_info"] = console_setting.GetApiInfo()
	}
	if cs.FAQEnabled {
		data["faq"] = console_setting.GetFAQ()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
	return
}

func GetMidjourney(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["Midjourney"],
	})
	return
}
