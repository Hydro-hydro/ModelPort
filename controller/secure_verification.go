package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/gin-gonic/gin"
)

const secureVerificationMethodPassword = "password"

type UniversalVerifyRequest struct {
	Method            string `json:"method"`
	PasswordEncrypted string `json:"password_encrypted"`
	EncryptionKeyID   string `json:"encryption_key_id"`
	Scope             string `json:"scope"`
}

func UniversalVerify(c *gin.Context) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "当前认证方式不支持安全验证"})
		return
	}
	if c.GetInt("role") < common.RoleAdminUser {
		common.ApiError(c, errors.New("仅管理员可以执行安全验证"))
		return
	}

	var request UniversalVerifyRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, fmt.Errorf("参数错误: %v", err))
		return
	}
	if request.Method != secureVerificationMethodPassword {
		common.ApiError(c, errors.New("安全验证必须使用管理员密码"))
		return
	}
	if !usage_mode.IsPersonalUse() {
		common.ApiError(c, errors.New("当前仅个人模式支持管理员密码安全验证"))
		return
	}
	if request.Scope != securityProofScopeChannelKeyRead {
		common.ApiError(c, errors.New("不支持的安全验证范围"))
		return
	}
	if strings.TrimSpace(request.PasswordEncrypted) == "" || strings.TrimSpace(request.EncryptionKeyID) == "" {
		common.ApiError(c, errors.New("管理员密码不能为空"))
		return
	}

	password, err := common.DecryptPassword(request.PasswordEncrypted, request.EncryptionKeyID)
	if err != nil {
		common.ApiError(c, errors.New("管理员密码验证失败"))
		return
	}
	owner, err := model.GetUserById(identity.UserID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if owner.Role < common.RoleAdminUser || owner.Status != common.UserStatusEnabled || !common.ValidatePasswordAndHash(password, owner.Password) {
		common.ApiError(c, errors.New("管理员密码验证失败"))
		return
	}

	proofToken, expiresAt, err := service.IssueSecurityProof(identity, request.Method, []string{request.Scope})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(identity.UserID, model.LogTypeSystem, "通用安全验证成功 (验证方式: 管理员密码)")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "验证成功",
		"data": gin.H{
			"proof_token": proofToken,
			"expires_at":  expiresAt,
			"method":      request.Method,
			"scope":       request.Scope,
		},
	})
}
