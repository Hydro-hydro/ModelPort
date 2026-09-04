package controller

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type Setup struct {
	Status       bool   `json:"status"`
	RootInit     bool   `json:"root_init"`
	DatabaseType string `json:"database_type"`
}

// SetupRequest keeps the legacy username and mode fields so older clients can
// still submit their existing payload shape. Personal edition initialization
// deliberately ignores those fields and always creates the root owner.
type SetupRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	ConfirmPassword    string `json:"confirmPassword"`
	SelfUseModeEnabled bool   `json:"SelfUseModeEnabled"`
	DemoSiteEnabled    bool   `json:"DemoSiteEnabled"`
}

func GetSetup(c *gin.Context) {
	if err := model.EnsurePersonalOwner(); err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"message": "无法确定个人版管理员账户，请检查数据库中的管理员记录",
		})
		return
	}

	setup := Setup{
		Status:       constant.Setup,
		RootInit:     model.RootUserExists(),
		DatabaseType: string(common.MainDatabaseType()),
	}
	c.JSON(200, gin.H{
		"success": true,
		"data":    setup,
	})
}

func PostSetup(c *gin.Context) {
	// Check if setup is already completed
	if constant.Setup {
		c.JSON(200, gin.H{
			"success": false,
			"message": "系统已经初始化完成",
		})
		return
	}

	var req SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": "请求参数有误",
		})
		return
	}

	// Reconcile a legacy administrator before initialization. A single legacy
	// admin is promoted by EnsurePersonalOwner; ambiguous or disabled account
	// layouts must be fixed manually instead of creating another owner.
	if err := model.EnsurePersonalOwner(); err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无法确定个人版管理员账户，请检查数据库中的管理员记录",
		})
		return
	}

	rootExists := model.RootUserExists()
	if !rootExists {
		if req.Password != req.ConfirmPassword {
			c.JSON(200, gin.H{
				"success": false,
				"message": "两次输入的密码不一致",
			})
			return
		}

		if len(req.Password) < 8 {
			c.JSON(200, gin.H{
				"success": false,
				"message": "密码长度至少为8个字符",
			})
			return
		}

		hashedPassword, err := common.Password2Hash(req.Password)
		if err != nil {
			c.JSON(200, gin.H{
				"success": false,
				"message": "系统错误: " + err.Error(),
			})
			return
		}
		rootUser := model.User{
			Username:    "root",
			Password:    hashedPassword,
			Role:        common.RoleRootUser,
			Status:      common.UserStatusEnabled,
			DisplayName: "Root User",
			AccessToken: nil,
		}
		if err = model.DB.Create(&rootUser).Error; err != nil {
			c.JSON(200, gin.H{
				"success": false,
				"message": "创建管理员账号失败: " + err.Error(),
			})
			return
		}
	}

	// Update setup status
	constant.Setup = true

	setup := model.Setup{
		Version:       common.Version,
		InitializedAt: time.Now().Unix(),
	}
	if err := model.DB.Create(&setup).Error; err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": "系统初始化失败: " + err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": "系统初始化成功",
	})
}
