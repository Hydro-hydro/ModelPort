package controller

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Setup struct {
	Status       bool   `json:"status"`
	RootInit     bool   `json:"root_init"`
	DatabaseType string `json:"database_type"`
}

// SetupRequest contains only the values needed by the current fresh-install
// wizard. The personal edition always creates the fixed root owner and has no
// legacy mode or username migration inputs.
type SetupRequest struct {
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
}

func GetSetup(c *gin.Context) {
	if err := model.EnsurePersonalOwner(); err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"message": "无法确定个人版管理员账户，请检查数据库中的管理员记录",
		})
		return
	}
	if !constant.Setup && model.RootUserExists() {
		c.JSON(500, gin.H{
			"success": false,
			"message": "数据库已包含管理员账户但缺少当前初始化记录，请使用新的数据目录重新部署",
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
	if setup := model.GetSetup(); setup != nil && !setup.IsCurrentSchema() {
		c.JSON(200, gin.H{
			"success": false,
			"message": "数据库初始化记录版本不匹配，请使用新的数据目录重新部署",
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

	// Refuse to initialize a non-empty database that has no current setup record.
	// This keeps the setup endpoint limited to genuinely fresh installations;
	// existing data must be removed and initialized again explicitly.
	if err := model.EnsurePersonalOwner(); err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无法确定个人版管理员账户，请检查数据库中的管理员记录",
		})
		return
	}

	rootExists := model.RootUserExists()
	if rootExists {
		c.JSON(200, gin.H{
			"success": false,
			"message": "数据库已包含管理员账户但缺少当前初始化记录，请使用新的数据目录重新部署",
		})
		return
	}
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
	if err = model.DB.Transaction(func(tx *gorm.DB) error {
		var userCount int64
		if err := tx.Model(&model.User{}).Count(&userCount).Error; err != nil {
			return err
		}
		if userCount > 0 {
			return model.ErrPersonalOwnerNotFound
		}
		if err := tx.Create(&rootUser).Error; err != nil {
			return err
		}
		return tx.Create(&model.Setup{
			Version:       common.Version,
			InitializedAt: time.Now().Unix(),
			Edition:       model.SetupEditionModelPort,
			SchemaVersion: model.CurrentSchemaVersion,
		}).Error
	}); err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": "创建管理员账号失败: " + err.Error(),
		})
		return
	}

	// Update runtime setup status only after the user and setup row commit
	// successfully. A failed transaction must leave the process in setup mode.
	constant.Setup = true

	c.JSON(200, gin.H{
		"success": true,
		"message": "系统初始化成功",
	})
}
