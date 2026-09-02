package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupControllerSetupTest(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Setup{}))

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousSetup := constant.Setup
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	constant.Setup = false
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		constant.Setup = previousSetup
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func performSetupRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	PostSetup(context)
	return recorder
}

func TestPostSetupAlwaysCreatesFixedRootOwner(t *testing.T) {
	db := setupControllerSetupTest(t)

	recorder := performSetupRequest(t, `{
		"username":"legacy-admin-name",
		"password":"root-password",
		"confirmPassword":"root-password",
		"SelfUseModeEnabled":false,
		"DemoSiteEnabled":true
	}`)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)

	var users []model.User
	require.NoError(t, db.Find(&users).Error)
	require.Len(t, users, 1)
	assert.Equal(t, "root", users[0].Username)
	assert.Equal(t, common.RoleRootUser, users[0].Role)
	assert.Equal(t, common.UserStatusEnabled, users[0].Status)

	var setup model.Setup
	require.NoError(t, db.First(&setup).Error)
	assert.NotZero(t, setup.InitializedAt)
}

func TestGetSetupDoesNotExposeLegacyModeFields(t *testing.T) {
	db := setupControllerSetupTest(t)
	passwordHash, err := common.Password2Hash("root-password")
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{
		Username: "root",
		Password: passwordHash,
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/setup", nil)
	GetSetup(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, true, response.Data["root_init"])
	assert.NotContains(t, response.Data, "SelfUseModeEnabled")
	assert.NotContains(t, response.Data, "DemoSiteEnabled")
}
