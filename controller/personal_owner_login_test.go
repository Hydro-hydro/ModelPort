package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPersonalOwnerLoginTest(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.Log{}))

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedis := common.RedisEnabled
	previousSessionSecret := common.SessionSecret
	previousPasswordLogin := common.PasswordLoginEnabled
	previousPasswordEncryption := common.PasswordLoginEncryptionEnabled
	previousSelfUse := operation_setting.SelfUseModeEnabled
	previousDemo := operation_setting.DemoSiteEnabled
	previousSetup := constant.Setup
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.SessionSecret = "personal-owner-login-test-secret"
	common.PasswordLoginEnabled = true
	common.PasswordLoginEncryptionEnabled = false
	operation_setting.SelfUseModeEnabled = true
	operation_setting.DemoSiteEnabled = false
	constant.Setup = true
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSessionSecret
		common.PasswordLoginEnabled = previousPasswordLogin
		common.PasswordLoginEncryptionEnabled = previousPasswordEncryption
		operation_setting.SelfUseModeEnabled = previousSelfUse
		operation_setting.DemoSiteEnabled = previousDemo
		constant.Setup = previousSetup
	})
	return db
}

func performPersonalLogin(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	Login(context)
	return recorder
}

func TestPersonalLoginUsesAdministratorPasswordWithoutUsername(t *testing.T) {
	db := setupPersonalOwnerLoginTest(t)
	passwordHash, err := common.Password2Hash("root-password")
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "root", Password: passwordHash, Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, AuthVersion: 1,
	}).Error)
	require.NoError(t, model.EnsurePersonalOwner())

	recorder := performPersonalLogin(t, `{"password":"root-password"}`)
	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			User struct {
				Id int `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, 1, response.Data.User.Id)
}

func TestPersonalLoginIgnoresCommonUserUsernameAndRejectsWrongPassword(t *testing.T) {
	db := setupPersonalOwnerLoginTest(t)
	rootHash, err := common.Password2Hash("root-password")
	require.NoError(t, err)
	commonHash, err := common.Password2Hash("common-password")
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{
		Id: 2, Username: "root", Password: rootHash, Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, AuthVersion: 1,
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id: 3, Username: "ordinary", Password: commonHash, Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AuthVersion: 1,
	}).Error)
	require.NoError(t, model.EnsurePersonalOwner())

	rootLogin := performPersonalLogin(t, `{"username":"ordinary","password":"root-password"}`)
	assert.Equal(t, http.StatusOK, rootLogin.Code)

	commonLogin := performPersonalLogin(t, `{"username":"root","password":"common-password"}`)
	assert.Equal(t, http.StatusOK, commonLogin.Code)
	assert.Contains(t, commonLogin.Body.String(), `"success":false`)

	wrongPassword := performPersonalLogin(t, `{"password":"wrong-password"}`)
	assert.Equal(t, http.StatusOK, wrongPassword.Code)
	assert.Contains(t, wrongPassword.Body.String(), `"success":false`)
}
