package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/performance_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"gorm.io/gorm"
)

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

func validateTurnstileOption(key, value string) error {
	trimmedValue := strings.TrimSpace(value)
	switch key {
	case "TurnstileCheckEnabled":
		if trimmedValue != "true" {
			return nil
		}
		if strings.TrimSpace(common.TurnstileSiteKey) == "" || strings.TrimSpace(common.TurnstileSecretKey) == "" {
			return fmt.Errorf("启用 Turnstile 前必须配置 Site Key 和 Secret Key")
		}
	case "TurnstileSiteKey":
		if common.TurnstileCheckEnabled && trimmedValue == "" {
			return fmt.Errorf("Turnstile 已启用，不能清空 Site Key")
		}
	case "TurnstileSecretKey":
		if common.TurnstileCheckEnabled && trimmedValue == "" {
			return fmt.Errorf("Turnstile 已启用，不能清空 Secret Key")
		}
	}
	return nil
}

func AllOption() ([]*Option, error) {
	var options []*Option
	var err error
	err = DB.Find(&options).Error
	return options, err
}

func InitOptionMap() {
	rateLimitConfig := setting.GetModelRequestRateLimitConfig()
	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)

	// 添加原有的系统配置
	common.OptionMap["FileUploadPermission"] = strconv.Itoa(common.FileUploadPermission)
	common.OptionMap["FileDownloadPermission"] = strconv.Itoa(common.FileDownloadPermission)
	common.OptionMap["ImageUploadPermission"] = strconv.Itoa(common.ImageUploadPermission)
	common.OptionMap["ImageDownloadPermission"] = strconv.Itoa(common.ImageDownloadPermission)
	common.OptionMap["TurnstileCheckEnabled"] = strconv.FormatBool(common.TurnstileCheckEnabled)
	common.OptionMap["AutomaticDisableChannelEnabled"] = strconv.FormatBool(common.AutomaticDisableChannelEnabled)
	common.OptionMap["AutomaticEnableChannelEnabled"] = strconv.FormatBool(common.AutomaticEnableChannelEnabled)
	common.OptionMap["LogConsumeEnabled"] = strconv.FormatBool(common.LogConsumeEnabled)
	common.OptionMap["DisplayInCurrencyEnabled"] = strconv.FormatBool(common.DisplayInCurrencyEnabled)
	common.OptionMap["DisplayTokenStatEnabled"] = strconv.FormatBool(common.DisplayTokenStatEnabled)
	common.OptionMap["DrawingEnabled"] = strconv.FormatBool(common.DrawingEnabled)
	jsplugin.DefaultRegistry.SetEnabled(constant.TaskPluginEnabled)
	jsplugin.DefaultRegistry.SetOverrideEnabled(constant.TaskPluginOverrideEnabled)
	for _, feature := range usage_mode.OptionalFeatures() {
		common.OptionMap[usage_mode.OptionalFeatureOptionKey(feature)] = strconv.FormatBool(usage_mode.IsFeatureEnabled(feature))
	}
	common.OptionMap[setting.TaskPluginMarketplaceSourcesKey] = setting.TaskPluginMarketplaceSources2JsonString()
	common.OptionMap[setting.TaskPluginDisabledFactoryKeysKey] = "[]"
	jsplugin.DefaultRegistry.SetDisabledFactoryKeys(nil)
	common.OptionMap["DataExportEnabled"] = strconv.FormatBool(common.DataExportEnabled)
	common.OptionMap["ChannelDisableThreshold"] = strconv.FormatFloat(common.ChannelDisableThreshold, 'f', -1, 64)
	common.OptionMap["SMTPServer"] = ""
	common.OptionMap["SMTPFrom"] = ""
	common.OptionMap["SMTPPort"] = strconv.Itoa(common.SMTPPort)
	common.OptionMap["SMTPAccount"] = ""
	common.OptionMap["SMTPToken"] = ""
	common.OptionMap["SMTPSSLEnabled"] = strconv.FormatBool(common.SMTPSSLEnabled)
	common.OptionMap["SMTPStartTLSEnabled"] = strconv.FormatBool(common.SMTPStartTLSEnabled)
	common.OptionMap["SMTPInsecureSkipVerify"] = strconv.FormatBool(common.SMTPInsecureSkipVerify)
	common.OptionMap["SMTPForceAuthLogin"] = strconv.FormatBool(common.SMTPForceAuthLogin)
	common.OptionMap["SystemName"] = common.SystemName
	common.OptionMap["Logo"] = common.Logo
	common.OptionMap["ServerAddress"] = ""
	common.OptionMap["TaskPublicAddress"] = system_setting.TaskPublicAddress
	common.OptionMap["WorkerUrl"] = system_setting.WorkerUrl
	common.OptionMap["WorkerValidKey"] = system_setting.WorkerValidKey
	common.OptionMap["WorkerAllowHttpImageRequestEnabled"] = strconv.FormatBool(system_setting.WorkerAllowHttpImageRequestEnabled)
	common.OptionMap["Chats"] = setting.Chats2JsonString()
	common.OptionMap["AutoGroups"] = setting.AutoGroups2JsonString()
	common.OptionMap["DefaultUseAutoGroup"] = strconv.FormatBool(setting.GetDefaultUseAutoGroup())
	common.OptionMap["MaxTokenAutoGroups"] = strconv.Itoa(setting.GetMaxTokenAutoGroups())
	common.OptionMap["TurnstileSiteKey"] = ""
	common.OptionMap["TurnstileSecretKey"] = ""
	common.OptionMap["PreConsumedQuota"] = strconv.Itoa(common.PreConsumedQuota)
	common.OptionMap["ModelRequestRateLimitCount"] = strconv.Itoa(rateLimitConfig.Count)
	common.OptionMap["ModelRequestRateLimitDurationMinutes"] = strconv.Itoa(rateLimitConfig.DurationMinutes)
	common.OptionMap["ModelRequestRateLimitSuccessCount"] = strconv.Itoa(rateLimitConfig.SuccessCount)
	common.OptionMap["ModelRequestRateLimitGroup"] = setting.ModelRequestRateLimitGroup2JSONString()
	common.OptionMap["ModelRatio"] = ratio_setting.ModelRatio2JSONString()
	common.OptionMap["ModelPrice"] = ratio_setting.ModelPrice2JSONString()
	common.OptionMap["CacheRatio"] = ratio_setting.CacheRatio2JSONString()
	common.OptionMap["CreateCacheRatio"] = ratio_setting.CreateCacheRatio2JSONString()
	common.OptionMap["GroupRatio"] = ratio_setting.GroupRatio2JSONString()
	common.OptionMap["GroupGroupRatio"] = ratio_setting.GroupGroupRatio2JSONString()
	common.OptionMap["CompletionRatio"] = ratio_setting.CompletionRatio2JSONString()
	common.OptionMap["ImageRatio"] = ratio_setting.ImageRatio2JSONString()
	common.OptionMap["AudioRatio"] = ratio_setting.AudioRatio2JSONString()
	common.OptionMap["AudioCompletionRatio"] = ratio_setting.AudioCompletionRatio2JSONString()
	//common.OptionMap["ChatLink"] = common.ChatLink
	//common.OptionMap["ChatLink2"] = common.ChatLink2
	common.OptionMap["QuotaPerUnit"] = strconv.FormatFloat(common.QuotaPerUnit, 'f', -1, 64)
	common.OptionMap["RetryTimes"] = strconv.Itoa(common.RetryTimes)
	common.OptionMap["DataExportInterval"] = strconv.Itoa(common.DataExportInterval)
	common.OptionMap["DataExportDefaultTime"] = common.DataExportDefaultTime
	common.OptionMap["MjNotifyEnabled"] = strconv.FormatBool(setting.MjNotifyEnabled)
	common.OptionMap["MjAccountFilterEnabled"] = strconv.FormatBool(setting.MjAccountFilterEnabled)
	common.OptionMap["MjModeClearEnabled"] = strconv.FormatBool(setting.MjModeClearEnabled)
	common.OptionMap["MjForwardUrlEnabled"] = strconv.FormatBool(setting.MjForwardUrlEnabled)
	common.OptionMap["MjActionCheckSuccessEnabled"] = strconv.FormatBool(setting.MjActionCheckSuccessEnabled)
	common.OptionMap["CheckSensitiveEnabled"] = strconv.FormatBool(setting.CheckSensitiveEnabled)
	common.OptionMap["ModelRequestRateLimitEnabled"] = strconv.FormatBool(rateLimitConfig.Enabled)
	common.OptionMap["CheckSensitiveOnPromptEnabled"] = strconv.FormatBool(setting.CheckSensitiveOnPromptEnabled)
	common.OptionMap["StopOnSensitiveEnabled"] = strconv.FormatBool(setting.StopOnSensitiveEnabled)
	common.OptionMap["SensitiveWords"] = setting.SensitiveWordsToString()
	common.OptionMap["StreamCacheQueueLength"] = strconv.Itoa(setting.StreamCacheQueueLength)
	common.OptionMap["AutomaticDisableKeywords"] = operation_setting.AutomaticDisableKeywordsToString()
	common.OptionMap["AutomaticDisableStatusCodes"] = operation_setting.AutomaticDisableStatusCodesToString()
	common.OptionMap["AutomaticRetryStatusCodes"] = operation_setting.AutomaticRetryStatusCodesToString()
	common.OptionMap["ExposeRatioEnabled"] = strconv.FormatBool(ratio_setting.IsExposeRatioEnabled())

	// 自动添加所有注册的模型配置
	modelConfigs := config.GlobalConfig.ExportAllConfigs()
	for k, v := range modelConfigs {
		common.OptionMap[k] = v
	}

	common.OptionMapRWMutex.Unlock()
	loadOptionsFromDatabase()
}

func loadOptionsFromDatabase() {
	options, err := AllOption()
	if err != nil {
		// A transient database read failure must not replace the currently
		// published runtime configuration with an empty result. Keep the last
		// known-good values and let the next sync pass retry the read.
		common.SysLog("failed to load options from database: " + err.Error())
		return
	}

	// Turnstile validation depends on the other two values. Apply the related
	// options in an order that matches the persisted target state instead of
	// relying on the database's unspecified row order.
	turnstileOptions := make(map[string]*Option, 3)
	for _, option := range options {
		switch option.Key {
		case "TurnstileSiteKey", "TurnstileSecretKey", "TurnstileCheckEnabled":
			turnstileOptions[option.Key] = option
		}
	}
	turnstileOrder := []string{"TurnstileCheckEnabled", "TurnstileSiteKey", "TurnstileSecretKey"}
	if enabled, ok := turnstileOptions["TurnstileCheckEnabled"]; ok && strings.TrimSpace(enabled.Value) == "true" {
		turnstileOrder = []string{"TurnstileSiteKey", "TurnstileSecretKey", "TurnstileCheckEnabled"}
	}
	for _, key := range turnstileOrder {
		option, ok := turnstileOptions[key]
		if !ok {
			continue
		}
		if err := updateOptionMap(option.Key, option.Value); err != nil {
			common.SysLog("failed to update option map: " + err.Error())
		}
	}
	for _, option := range options {
		if _, ok := turnstileOptions[option.Key]; ok {
			continue
		}
		if isRetiredPersonalOption(option.Key) {
			continue
		}
		err := updateOptionMap(option.Key, option.Value)
		if err != nil {
			common.SysLog("failed to update option map: " + err.Error())
		}
	}
}

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing options from database")
		loadOptionsFromDatabase()
	}
}

func validateOptionValue(key string, value string) error {
	if err := validateTurnstileOption(key, value); err != nil {
		return err
	}
	if usage_mode.IsOptionalFeatureOptionKey(key) {
		if _, err := strconv.ParseBool(strings.TrimSpace(value)); err != nil {
			return fmt.Errorf("optional feature %s must be a boolean: %w", key, err)
		}
		return nil
	}
	if strings.HasPrefix(key, "feature.") {
		return fmt.Errorf("unknown optional feature option: %s", key)
	}
	if key == operation_setting.ToolPriceOptionKey {
		return operation_setting.ValidateToolPricesJSON(value)
	}
	if key == operation_setting.ChannelTestConcurrencyOptionKey {
		return operation_setting.ValidateChannelTestConcurrency(value)
	}
	if key == "MaxTokenAutoGroups" {
		return setting.ValidateMaxTokenAutoGroups(value)
	}
	if key == "ModelRequestRateLimitGroup" {
		return setting.CheckModelRequestRateLimitGroup(value)
	}
	if key == "AutomaticDisableStatusCodes" || key == "AutomaticRetryStatusCodes" {
		_, err := operation_setting.ParseHTTPStatusCodeRanges(value)
		return err
	}
	if separator := strings.IndexByte(key, '.'); separator > 0 {
		configName, configKey := key[:separator], key[separator+1:]
		if config.GlobalConfig.Get(configName) != nil {
			return config.GlobalConfig.Validate(configName, map[string]string{configKey: value})
		}
	}
	switch key {
	case "ModelRatio", "ModelPrice", "CompletionRatio", "CacheRatio", "CreateCacheRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio":
		return ratio_setting.ValidateFlatRatioJSON(value)
	case "GroupRatio":
		return ratio_setting.CheckGroupRatio(value)
	case "GroupGroupRatio":
		return ratio_setting.ValidateNestedRatioJSON(value)
	}
	return nil
}

func UpdateOption(key string, value string) error {
	if isFixedPersonalModeOption(key) || isRemovedPublicContentOption(key) || isRetiredPersonalOption(key) {
		return nil
	}
	if err := validateOptionValue(key, value); err != nil {
		return err
	}
	// Persist first, in one transaction. Runtime state is only published after
	// this succeeds, so a failed write cannot leave a live-only setting.
	err := DB.Transaction(func(tx *gorm.DB) error {
		option := Option{Key: key}
		if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
			return err
		}
		option.Value = value
		return tx.Save(&option).Error
	})
	if err != nil {
		return err
	}
	// Update OptionMap
	return updateOptionMap(key, value)
}

// UpdateOptionsBulk persists multiple key/value pairs in a single database
// transaction, then dispatches them through updateOptionMap in one pass. If
// any DB write fails the whole transaction rolls back and no in-memory state
// is touched, which keeps related runtime settings consistent.
func UpdateOptionsBulk(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	filteredValues := make(map[string]string, len(values))
	for key, value := range values {
		if isFixedPersonalModeOption(key) || isRemovedPublicContentOption(key) || isRetiredPersonalOption(key) {
			continue
		}
		filteredValues[key] = value
	}
	if len(filteredValues) == 0 {
		return nil
	}
	for key, value := range filteredValues {
		if err := validateOptionValue(key, value); err != nil {
			return err
		}
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		for k, v := range filteredValues {
			option := Option{Key: k}
			if err := tx.FirstOrCreate(&option, Option{Key: k}).Error; err != nil {
				return err
			}
			option.Value = v
			if err := tx.Save(&option).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for k, v := range filteredValues {
		if err := updateOptionMap(k, v); err != nil {
			return err
		}
	}
	return nil
}

func isFixedPersonalModeOption(key string) bool {
	return key == "DemoSiteEnabled" || key == "SelfUseModeEnabled"
}

func isRetiredPersonalOption(key string) bool {
	switch key {
	case "PasswordLoginEnabled", "TaskEnabled", "TaskPluginEnabled", "TaskPluginOverrideEnabled":
		return true
	default:
		return false
	}
}

func isRemovedPublicContentOption(key string) bool {
	switch key {
	case "Notice", "About", "HomePageContent", "Footer", "Announcements",
		"console_setting.announcements", "console_setting.announcements_enabled",
		"legal.user_agreement", "legal.privacy_policy", "HeaderNavModules":
		return true
	default:
		return false
	}
}

func updateOptionMap(key string, value string) (err error) {
	if isFixedPersonalModeOption(key) || isRemovedPublicContentOption(key) || isRetiredPersonalOption(key) {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, key)
		common.OptionMapRWMutex.Unlock()
		return nil
	}
	if err := validateTurnstileOption(key, value); err != nil {
		return err
	}
	// 检查是否是模型配置 - 使用更规范的方式处理
	if handled, updateErr := handleConfigUpdate(key, value); handled {
		if updateErr != nil {
			return updateErr
		}
		common.OptionMapRWMutex.Lock()
		common.OptionMap[key] = value
		common.OptionMapRWMutex.Unlock()
		if isPricingOptionKey(key) {
			InvalidatePricingCache()
		}
		return nil // 已由配置系统处理
	}

	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	previousValue, hadPreviousValue := common.OptionMap[key]
	common.OptionMap[key] = value

	// 处理传统配置项...
	if strings.HasSuffix(key, "Permission") {
		intValue, _ := strconv.Atoi(value)
		switch key {
		case "FileUploadPermission":
			common.FileUploadPermission = intValue
		case "FileDownloadPermission":
			common.FileDownloadPermission = intValue
		case "ImageUploadPermission":
			common.ImageUploadPermission = intValue
		case "ImageDownloadPermission":
			common.ImageDownloadPermission = intValue
		}
	}
	if strings.HasSuffix(key, "Enabled") || key == "DefaultUseAutoGroup" || key == "SMTPForceAuthLogin" || key == "SMTPInsecureSkipVerify" {
		boolValue := value == "true"
		switch key {
		case "TurnstileCheckEnabled":
			common.TurnstileCheckEnabled = boolValue
		case "AutomaticDisableChannelEnabled":
			common.AutomaticDisableChannelEnabled = boolValue
		case "AutomaticEnableChannelEnabled":
			common.AutomaticEnableChannelEnabled = boolValue
		case "LogConsumeEnabled":
			common.LogConsumeEnabled = boolValue
		case "DisplayInCurrencyEnabled":
			// 兼容旧字段：同步到新配置 general_setting.quota_display_type（运行时生效）
			// true -> USD, false -> TOKENS
			newVal := "USD"
			if !boolValue {
				newVal = "TOKENS"
			}
			if cfg := config.GlobalConfig.Get("general_setting"); cfg != nil {
				_ = config.UpdateConfigFromMap(cfg, map[string]string{"quota_display_type": newVal})
			}
		case "DisplayTokenStatEnabled":
			common.DisplayTokenStatEnabled = boolValue
		case "DrawingEnabled":
			common.DrawingEnabled = boolValue
		case "DataExportEnabled":
			common.DataExportEnabled = boolValue
		case "MjNotifyEnabled":
			setting.MjNotifyEnabled = boolValue
		case "MjAccountFilterEnabled":
			setting.MjAccountFilterEnabled = boolValue
		case "MjModeClearEnabled":
			setting.MjModeClearEnabled = boolValue
		case "MjForwardUrlEnabled":
			setting.MjForwardUrlEnabled = boolValue
		case "MjActionCheckSuccessEnabled":
			setting.MjActionCheckSuccessEnabled = boolValue
		case "CheckSensitiveEnabled":
			setting.CheckSensitiveEnabled = boolValue
		case "CheckSensitiveOnPromptEnabled":
			setting.CheckSensitiveOnPromptEnabled = boolValue
		case "ModelRequestRateLimitEnabled":
			setting.SetModelRequestRateLimitEnabled(boolValue)
		case "StopOnSensitiveEnabled":
			setting.StopOnSensitiveEnabled = boolValue
		case "SMTPSSLEnabled":
			common.SMTPSSLEnabled = boolValue
		case "SMTPStartTLSEnabled":
			common.SMTPStartTLSEnabled = boolValue
		case "SMTPInsecureSkipVerify":
			common.SMTPInsecureSkipVerify = boolValue
		case "SMTPForceAuthLogin":
			common.SMTPForceAuthLogin = boolValue
		case "WorkerAllowHttpImageRequestEnabled":
			system_setting.WorkerAllowHttpImageRequestEnabled = boolValue
		case "DefaultUseAutoGroup":
			setting.SetDefaultUseAutoGroup(boolValue)
		case "ExposeRatioEnabled":
			ratio_setting.SetExposeRatioEnabled(boolValue)
		}
	}
	if key == setting.TaskPluginDisabledFactoryKeysKey {
		jsplugin.DefaultRegistry.SetDisabledFactoryKeys(setting.ParseTaskPluginDisabledFactoryKeys(value))
	}
	switch key {
	case "SMTPServer":
		common.SMTPServer = value
	case "SMTPPort":
		intValue, _ := strconv.Atoi(value)
		common.SMTPPort = intValue
	case "SMTPAccount":
		common.SMTPAccount = value
	case "SMTPFrom":
		common.SMTPFrom = value
	case "SMTPToken":
		common.SMTPToken = value
	case "ServerAddress":
		system_setting.ServerAddress = value
	case "TaskPublicAddress":
		system_setting.TaskPublicAddress = value
	case "WorkerUrl":
		system_setting.WorkerUrl = value
	case "WorkerValidKey":
		system_setting.WorkerValidKey = value
	case "SystemName":
		common.SystemName = value
	case "Logo":
		common.Logo = value
	case "TurnstileSiteKey":
		common.TurnstileSiteKey = value
	case "TurnstileSecretKey":
		common.TurnstileSecretKey = value
	case "PreConsumedQuota":
		common.PreConsumedQuota, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitCount":
		count, _ := strconv.Atoi(value)
		setting.SetModelRequestRateLimitCount(count)
	case "ModelRequestRateLimitDurationMinutes":
		duration, _ := strconv.Atoi(value)
		setting.SetModelRequestRateLimitDurationMinutes(duration)
	case "ModelRequestRateLimitSuccessCount":
		successCount, _ := strconv.Atoi(value)
		setting.SetModelRequestRateLimitSuccessCount(successCount)
	case "ModelRequestRateLimitGroup":
		err = setting.UpdateModelRequestRateLimitGroupByJSONString(value)
	case "RetryTimes":
		common.RetryTimes, _ = strconv.Atoi(value)
	case "DataExportInterval":
		common.DataExportInterval, _ = strconv.Atoi(value)
	case "DataExportDefaultTime":
		common.DataExportDefaultTime = value
	case "ModelRatio":
		err = ratio_setting.UpdateModelRatioByJSONString(value)
	case "GroupRatio":
		err = ratio_setting.UpdateGroupRatioByJSONString(value)
	case "GroupGroupRatio":
		err = ratio_setting.UpdateGroupGroupRatioByJSONString(value)
	case "CompletionRatio":
		err = ratio_setting.UpdateCompletionRatioByJSONString(value)
	case "ModelPrice":
		err = ratio_setting.UpdateModelPriceByJSONString(value)
	case "CacheRatio":
		err = ratio_setting.UpdateCacheRatioByJSONString(value)
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(value)
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(value)
	case "ChannelDisableThreshold":
		common.ChannelDisableThreshold, _ = strconv.ParseFloat(value, 64)
	case "QuotaPerUnit":
		common.QuotaPerUnit, _ = strconv.ParseFloat(value, 64)
	case "SensitiveWords":
		setting.SensitiveWordsFromString(value)
	case "AutomaticDisableKeywords":
		operation_setting.AutomaticDisableKeywordsFromString(value)
	case "AutomaticDisableStatusCodes":
		err = operation_setting.AutomaticDisableStatusCodesFromString(value)
	case "AutomaticRetryStatusCodes":
		err = operation_setting.AutomaticRetryStatusCodesFromString(value)
	case "StreamCacheQueueLength":
		setting.StreamCacheQueueLength, _ = strconv.Atoi(value)
	}
	if err != nil {
		if hadPreviousValue {
			common.OptionMap[key] = previousValue
		} else {
			delete(common.OptionMap, key)
		}
		return err
	}
	if isPricingOptionKey(key) {
		// Pricing responses include model and group multipliers. Invalidate the
		// derived display snapshot immediately after a successful replacement so
		// the one-minute pricing refresh window cannot expose stale values.
		InvalidatePricingCache()
	}
	return nil
}

func isPricingOptionKey(key string) bool {
	if strings.HasPrefix(key, "billing_setting.") || strings.HasPrefix(key, "group_ratio_setting.") {
		return true
	}
	switch key {
	case "ModelRatio", "ModelPrice", "CompletionRatio", "CacheRatio", "CreateCacheRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio", "GroupRatio", "GroupGroupRatio":
		return true
	default:
		return false
	}
}

// handleConfigUpdate 处理分层配置更新，返回是否已处理
func handleConfigUpdate(key, value string) (bool, error) {
	if key == operation_setting.ToolPriceOptionKey {
		operation_setting.LoadToolPricesFromJSONString(value)
		return true, nil
	}

	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return false, nil // 不是分层配置
	}

	configName := parts[0]
	configKey := parts[1]

	// 获取配置对象
	cfg := config.GlobalConfig.Get(configName)
	if cfg == nil {
		return false, nil // 未注册的配置
	}

	// 更新配置
	configMap := map[string]string{
		configKey: value,
	}
	if err := config.GlobalConfig.Update(configName, configMap); err != nil {
		return true, err
	}

	// 特定配置的后处理
	if configName == "performance_setting" {
		performance_setting.UpdateAndSync()
	} else if configName == "billing_setting" {
		InvalidatePricingCache()
		ratio_setting.InvalidateExposedDataCache()
	}

	return true, nil // 已处理
}
