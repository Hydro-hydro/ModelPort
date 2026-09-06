package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LoginRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	PasswordEncrypted string `json:"password_encrypted"`
	EncryptionKeyID   string `json:"encryption_key_id"`
}

var (
	errUserPasswordUnset    = errors.New("user password is not set")
	errOriginalPasswordFail = errors.New("original password is incorrect")
)

func GetPasswordEncryptionKey(c *gin.Context) {
	keyID, publicKey := common.PasswordEncryptionPublicKey()
	if keyID == "" || publicKey == "" {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	common.ApiSuccess(c, gin.H{
		"enabled":    common.PasswordLoginEncryptionEnabled,
		"kid":        keyID,
		"public_key": publicKey,
	})
}

func Login(c *gin.Context) {
	var loginRequest LoginRequest
	err := common.DecodeJson(c.Request.Body, &loginRequest)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// username remains accepted in the request for old clients, but the
	// personal edition always authenticates the single root owner.
	password := loginRequest.Password
	if common.PasswordLoginEncryptionEnabled {
		if loginRequest.PasswordEncrypted == "" || loginRequest.EncryptionKeyID == "" {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		password, err = common.DecryptPassword(loginRequest.PasswordEncrypted, loginRequest.EncryptionKeyID)
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
			return
		}
	}
	if password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	owner, ownerErr := model.GetPersonalOwner()
	if ownerErr != nil {
		common.SysLog(fmt.Sprintf("Personal owner login lookup failed: %v", ownerErr))
		common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
		return
	}
	if !common.ValidatePasswordAndHash(password, owner.Password) {
		common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
		return
	}
	user := *owner

	setupLogin(&user, c)
}

// loginMethodFromContext 根据请求路径推导登录方式，用于登录审计日志。
func loginMethodFromContext(c *gin.Context) string {
	switch c.FullPath() {
	case "/api/user/login":
		return "password"
	default:
		return "unknown"
	}
}

// recordLoginAudit 记录登录成功审计日志（对所有用户启用，仅记录成功，不记录失败）。
func recordLoginAudit(user *model.User, c *gin.Context) {
	method := loginMethodFromContext(c)
	ip := c.ClientIP()
	extra := map[string]interface{}{
		"login_method": method,
		"user_agent":   c.Request.UserAgent(),
	}
	content := fmt.Sprintf("Logged in successfully via %s", method)
	model.RecordLoginLog(user.Id, user.Username, content, ip, "login", map[string]interface{}{
		"method": method,
	}, extra)
}

// setupLogin creates a server-controlled login Session and returns the shared
// authentication bundle used by every login method.
func setupLogin(user *model.User, c *gin.Context) {
	setupLoginAtAuthVersion(user, 0, c)
}

func setupLoginAtAuthVersion(user *model.User, expectedAuthVersion int64, c *gin.Context) {
	if user == nil || user.Id <= 0 || user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgAuthUserBanned)
		return
	}
	currentUser, err := model.GetUserById(user.Id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var bundle *service.AuthBundle
	if expectedAuthVersion > 0 {
		bundle, err = service.CreateLoginSessionAtAuthVersion(
			user.Id,
			expectedAuthVersion,
			loginMethodFromContext(c),
			c.ClientIP(),
			c.Request.UserAgent(),
		)
	} else {
		bundle, err = service.CreateLoginSession(
			user.Id,
			loginMethodFromContext(c),
			c.ClientIP(),
			c.Request.UserAgent(),
		)
	}
	if err != nil {
		writeAuthSessionError(c, err)
		return
	}
	model.UpdateUserLastLoginAt(user.Id)
	service.WriteRefreshCookie(c, bundle.RefreshToken)
	setAuthNoStore(c)
	recordLoginAudit(user, c)
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
		"data": gin.H{
			"access_token":      bundle.AccessToken,
			"token_type":        bundle.TokenType,
			"access_expires_at": bundle.AccessExpiresAt,
			"session":           bundle.Session,
			"user":              buildSelfUserData(currentUser),
		},
	})
}

func GenerateAccessToken(c *gin.Context) {
	id := c.GetInt("id")
	// get rand int 28-32
	randI := common.GetRandomInt(4)
	key, err := common.GenerateRandomKey(29 + randI)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGenerateFailed)
		common.SysLog("failed to generate key: " + err.Error())
		return
	}
	if model.DB.Where("access_token = ?", key).First(&model.User{}).RowsAffected != 0 {
		common.ApiErrorI18n(c, i18n.MsgUuidDuplicate)
		return
	}

	if err := model.UpdateUserAccessToken(id, key); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    key,
	})
	return
}

func GetSelf(c *gin.Context) {
	id := c.GetInt("id")
	userRole := c.GetInt("role")
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	responseData := buildSelfUserData(user)
	// The authenticated role is loaded from GetUserCache. It should equal the
	// row role, but use it for capabilities so GetSelf and login/refresh remain
	// consistent with the authorization decision made for this request.
	permissions := calculateUserPermissions()
	permissions["admin_permissions"] = authz.Capabilities(id, userRole)
	responseData["permissions"] = permissions

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    responseData,
	})
	return
}

// buildSelfUserData is the single safe dashboard-user DTO used by GetSelf,
// login and refresh. It intentionally excludes password, management PAT and
// administrator-only remarks.
func buildSelfUserData(user *model.User) map[string]interface{} {
	userSetting := user.GetSetting()
	permissions := calculateUserPermissions()
	permissions["admin_permissions"] = authz.Capabilities(user.Id, user.Role)
	settingJSON := user.Setting
	if sanitized, err := common.Marshal(userSetting); err == nil {
		settingJSON = string(sanitized)
	}
	return map[string]interface{}{
		"id":              user.Id,
		"username":        user.Username,
		"display_name":    user.DisplayName,
		"role":            user.Role,
		"status":          user.Status,
		"email":           user.Email,
		"group":           user.Group,
		"used_quota":      user.UsedQuota,
		"request_count":   user.RequestCount,
		"setting":         settingJSON,
		"sidebar_modules": userSetting.SidebarModules, // 正确提取sidebar_modules字段
		"permissions":     permissions,
	}
}

// 计算用户权限的辅助函数
func calculateUserPermissions() map[string]interface{} {
	return map[string]interface{}{
		"sidebar_settings": false,
		"sidebar_modules":  map[string]interface{}{},
	}
}

func GetUserModels(c *gin.Context) {
	groups := service.GetPersonalUsableGroups()
	group := c.Query("group")
	var groupsToQuery []string
	switch {
	case group == "":
		for g := range groups {
			groupsToQuery = append(groupsToQuery, g)
		}
	case group == "auto":
		groupsToQuery = service.GetPersonalAutoGroups()
	default:
		if _, ok := groups[group]; ok {
			groupsToQuery = []string{group}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    service.GetGroupsEnabledModels(groupsToQuery),
	})
}

func UpdateSelf(c *gin.Context) {
	var requestData map[string]interface{}
	if err := common.DecodeJson(c.Request.Body, &requestData); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 检查是否是用户设置更新请求 (sidebar_modules 或 language)
	if sidebarModules, sidebarExists := requestData["sidebar_modules"]; sidebarExists {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新sidebar_modules字段
		if sidebarModulesStr, ok := sidebarModules.(string); ok {
			currentSetting.SidebarModules = sidebarModulesStr
		}

		if err := model.UpdateUserSetting(user.Id, currentSetting); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 检查是否是语言偏好更新请求
	if language, langExists := requestData["language"]; langExists {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新language字段
		if langStr, ok := language.(string); ok {
			currentSetting.Language = langStr
		}

		if err := model.UpdateUserSetting(user.Id, currentSetting); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 原有的用户信息更新逻辑
	var user model.User
	requestDataBytes, err := common.Marshal(requestData)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err = common.Unmarshal(requestDataBytes, &user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	if user.Password == "" {
		user.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidInput)
		return
	}

	cleanUser := model.User{
		Id:          c.GetInt("id"),
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
	}
	if user.Password == "$I_LOVE_U" {
		user.Password = "" // rollback to what it should be
		cleanUser.Password = ""
	}
	updatePassword, err := checkUpdatePassword(user.OriginalPassword, user.Password, cleanUser.Id)
	if err != nil {
		if errors.Is(err, errUserPasswordUnset) {
			common.ApiErrorI18n(c, i18n.MsgUserPasswordUnset)
			return
		}
		if errors.Is(err, errOriginalPasswordFail) {
			common.ApiErrorI18n(c, i18n.MsgUserOriginalPasswordError)
			return
		}
		common.ApiError(c, err)
		return
	}
	if updatePassword {
		identity, ok := middleware.GetSessionAuthIdentity(c)
		if !ok {
			common.ApiError(c, errors.New("当前认证方式不支持安全验证"))
			return
		}
		if err := model.DB.Transaction(func(tx *gorm.DB) error {
			return cleanUser.UpdateWithTx(tx, true)
		}); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := model.PublishUserAuthCache(cleanUser.Id); err != nil {
			common.ApiError(c, err)
			return
		}
		bundle, err := service.AdvanceCurrentSessionToUserVersion(identity, "password_changed")
		if err != nil {
			common.ApiError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"access_token":      bundle.AccessToken,
				"token_type":        bundle.TokenType,
				"access_expires_at": bundle.AccessExpiresAt,
				"session":           bundle.Session,
			},
		})
		return
	}
	if err := cleanUser.Update(false); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
	return
}

func checkUpdatePassword(originalPassword string, newPassword string, userId int) (updatePassword bool, err error) {
	if newPassword == "" {
		return
	}
	var currentUser *model.User
	currentUser, err = model.GetUserById(userId, true)
	if err != nil {
		return
	}

	// 密码不为空,需要验证原密码
	if currentUser.Password == "" {
		err = errUserPasswordUnset
		return
	}
	if !common.ValidatePasswordAndHash(originalPassword, currentUser.Password) {
		err = errOriginalPasswordFail
		return
	}
	updatePassword = true
	return
}

type UpdateUserSettingRequest struct {
	NotifyType                       string `json:"notify_type"`
	WebhookUrl                       string `json:"webhook_url,omitempty"`
	WebhookSecret                    string `json:"webhook_secret,omitempty"`
	NotificationEmail                string `json:"notification_email,omitempty"`
	BarkUrl                          string `json:"bark_url,omitempty"`
	GotifyUrl                        string `json:"gotify_url,omitempty"`
	GotifyToken                      string `json:"gotify_token,omitempty"`
	GotifyPriority                   int    `json:"gotify_priority,omitempty"`
	UpstreamModelUpdateNotifyEnabled *bool  `json:"upstream_model_update_notify_enabled,omitempty"`
	AcceptUnsetModelRatioModel       bool   `json:"accept_unset_model_ratio_model"`
	RecordIpLog                      bool   `json:"record_ip_log"`
}

func UpdateUserSetting(c *gin.Context) {
	var req UpdateUserSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	notifyType := req.NotifyType
	if notifyType == "" {
		notifyType = dto.NotifyTypeEmail
	}
	// Validate the notification transport. Notifications are used for channel
	// and upstream model events; they are no longer tied to quota alerts.
	if notifyType != dto.NotifyTypeEmail && notifyType != dto.NotifyTypeWebhook && notifyType != dto.NotifyTypeBark && notifyType != dto.NotifyTypeGotify {
		common.ApiErrorI18n(c, i18n.MsgSettingInvalidType)
		return
	}

	// 如果是webhook类型,验证webhook地址
	if notifyType == dto.NotifyTypeWebhook {
		if req.WebhookUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.WebhookUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookInvalid)
			return
		}
	}

	// 如果是邮件类型，验证邮箱地址
	if notifyType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		// 验证邮箱格式
		if !strings.Contains(req.NotificationEmail, "@") {
			common.ApiErrorI18n(c, i18n.MsgSettingEmailInvalid)
			return
		}
	}

	// 如果是Bark类型，验证Bark URL
	if notifyType == dto.NotifyTypeBark {
		if req.BarkUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.BarkUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.BarkUrl, "https://") && !strings.HasPrefix(req.BarkUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	// 如果是Gotify类型，验证Gotify URL和Token
	if notifyType == dto.NotifyTypeGotify {
		if req.GotifyUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlEmpty)
			return
		}
		if req.GotifyToken == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyTokenEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.GotifyUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.GotifyUrl, "https://") && !strings.HasPrefix(req.GotifyUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	existingSettings := user.GetSetting()
	upstreamModelUpdateNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
	if user.Role >= common.RoleAdminUser && req.UpstreamModelUpdateNotifyEnabled != nil {
		upstreamModelUpdateNotifyEnabled = *req.UpstreamModelUpdateNotifyEnabled
	}

	// 构建设置
	settings := dto.UserSetting{
		NotifyType:                       notifyType,
		UpstreamModelUpdateNotifyEnabled: upstreamModelUpdateNotifyEnabled,
		AcceptUnsetRatioModel:            req.AcceptUnsetModelRatioModel,
		RecordIpLog:                      req.RecordIpLog,
	}

	// 如果是webhook类型,添加webhook相关设置
	if notifyType == dto.NotifyTypeWebhook {
		settings.WebhookUrl = req.WebhookUrl
		if req.WebhookSecret != "" {
			settings.WebhookSecret = req.WebhookSecret
		}
	}

	// 如果提供了通知邮箱，添加到设置中
	if notifyType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		settings.NotificationEmail = req.NotificationEmail
	}

	// 如果是Bark类型，添加Bark URL到设置中
	if notifyType == dto.NotifyTypeBark {
		settings.BarkUrl = req.BarkUrl
	}

	// 如果是Gotify类型，添加Gotify配置到设置中
	if notifyType == dto.NotifyTypeGotify {
		settings.GotifyUrl = req.GotifyUrl
		settings.GotifyToken = req.GotifyToken
		// Gotify优先级范围0-10，超出范围则使用默认值5
		if req.GotifyPriority < 0 || req.GotifyPriority > 10 {
			settings.GotifyPriority = 5
		} else {
			settings.GotifyPriority = req.GotifyPriority
		}
	}

	// 更新用户设置
	if err := model.UpdateUserSetting(user.Id, settings); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
		return
	}

	common.ApiSuccessI18n(c, i18n.MsgSettingSaved, nil)
}
