package main

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupStartupOwnerTest(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Setup{}))

	previousDB := model.DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousSetup := constant.Setup
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	constant.Setup = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		constant.Setup = previousSetup
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestValidatePersonalOwnerAtStartupAllowsEmptyUninitializedDatabase(t *testing.T) {
	setupStartupOwnerTest(t)

	require.NoError(t, validatePersonalOwnerAtStartup())
}

func TestValidatePersonalOwnerAtStartupRejectsAmbiguousUninitializedDatabase(t *testing.T) {
	db := setupStartupOwnerTest(t)
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "admin-a", Password: "password", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id: 2, Username: "admin-b", Password: "password", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled,
	}).Error)

	err := validatePersonalOwnerAtStartup()
	assert.ErrorIs(t, err, model.ErrPersonalOwnerAmbiguous)
	var setupCount int64
	require.NoError(t, db.Model(&model.Setup{}).Count(&setupCount).Error)
	assert.Zero(t, setupCount)
}

func TestValidatePersonalOwnerAtStartupRejectsNonEmptyDatabaseWithoutSetupRecord(t *testing.T) {
	db := setupStartupOwnerTest(t)
	require.NoError(t, db.Create(&model.User{
		Id: 3, Username: "root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled,
	}).Error)

	err := validatePersonalOwnerAtStartup()
	assert.ErrorIs(t, err, model.ErrSetupRecordMissing)
}

func TestValidatePersonalOwnerAtStartupRejectsMismatchedSetupSchema(t *testing.T) {
	db := setupStartupOwnerTest(t)
	require.NoError(t, db.Create(&model.User{
		Id: 5, Username: "root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Setup{
		Version:       "legacy",
		InitializedAt: 1,
		Edition:       "new-api",
		SchemaVersion: 0,
	}).Error)

	model.CheckSetup()

	err := validatePersonalOwnerAtStartup()
	assert.ErrorIs(t, err, model.ErrSetupSchemaMismatch)
}

func TestCheckSetupDoesNotCreateSetupRecordForExistingRoot(t *testing.T) {
	db := setupStartupOwnerTest(t)
	require.NoError(t, db.Create(&model.User{
		Id: 4, Username: "root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled,
	}).Error)

	model.CheckSetup()

	assert.False(t, constant.Setup)
	var setupCount int64
	require.NoError(t, db.Model(&model.Setup{}).Count(&setupCount).Error)
	assert.Zero(t, setupCount)
}

func TestValidatePersonalOwnerAtStartupRejectsInitializedDatabaseWithoutOwner(t *testing.T) {
	db := setupStartupOwnerTest(t)
	require.NoError(t, db.Create(&model.Setup{
		Version:       "test",
		InitializedAt: 1,
		Edition:       model.SetupEditionModelPort,
		SchemaVersion: model.CurrentSchemaVersion,
	}).Error)
	constant.Setup = true

	err := validatePersonalOwnerAtStartup()
	assert.ErrorIs(t, err, model.ErrPersonalOwnerNotFound)
}
