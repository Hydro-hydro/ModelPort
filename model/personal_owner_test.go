package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPersonalOwnerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}))

	previousDB := DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		initCol()
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestEnsurePersonalOwnerAllowsEmptyDatabaseForSetup(t *testing.T) {
	setupPersonalOwnerTestDB(t)

	require.NoError(t, EnsurePersonalOwner())
	_, err := GetPersonalOwner()
	assert.ErrorIs(t, err, ErrPersonalOwnerNotFound)
}

func TestEnsurePersonalOwnerKeepsSingleRoot(t *testing.T) {
	db := setupPersonalOwnerTestDB(t)
	require.NoError(t, db.Create(&User{
		Id: 1, Username: "root", Password: "password", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
	}).Error)

	require.NoError(t, EnsurePersonalOwner())
	owner, err := GetPersonalOwner()
	require.NoError(t, err)
	assert.Equal(t, 1, owner.Id)
	assert.Equal(t, common.RoleRootUser, owner.Role)
}

func TestEnsurePersonalOwnerPromotesSingleAdministratorIdempotently(t *testing.T) {
	db := setupPersonalOwnerTestDB(t)
	require.NoError(t, db.Create(&User{
		Id: 2, Username: "admin", Password: "password", Role: common.RoleAdminUser, Status: common.UserStatusEnabled,
	}).Error)

	require.NoError(t, EnsurePersonalOwner())
	require.NoError(t, EnsurePersonalOwner())

	owner, err := GetPersonalOwner()
	require.NoError(t, err)
	assert.Equal(t, 2, owner.Id)
	assert.Equal(t, common.RoleRootUser, owner.Role)
}

func TestEnsurePersonalOwnerRejectsCommonOnlyDatabase(t *testing.T) {
	db := setupPersonalOwnerTestDB(t)
	require.NoError(t, db.Create(&User{
		Id: 3, Username: "common", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
	}).Error)

	assert.ErrorIs(t, EnsurePersonalOwner(), ErrPersonalOwnerNotFound)
}

func TestEnsurePersonalOwnerRejectsMultipleAdministrators(t *testing.T) {
	db := setupPersonalOwnerTestDB(t)
	require.NoError(t, db.Create(&User{
		Id: 4, Username: "root-a", Password: "password", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&User{
		Id: 5, Username: "admin-b", Password: "password", Role: common.RoleAdminUser, Status: common.UserStatusEnabled,
	}).Error)

	assert.ErrorIs(t, EnsurePersonalOwner(), ErrPersonalOwnerAmbiguous)
	assert.ErrorIs(t, func() error {
		_, err := GetPersonalOwner()
		return err
	}(), ErrPersonalOwnerAmbiguous)
}

func TestEnsurePersonalOwnerRejectsDisabledAdministrator(t *testing.T) {
	db := setupPersonalOwnerTestDB(t)
	require.NoError(t, db.Create(&User{
		Id: 6, Username: "disabled-root", Password: "password", Role: common.RoleRootUser, Status: common.UserStatusDisabled,
	}).Error)

	assert.ErrorIs(t, EnsurePersonalOwner(), ErrPersonalOwnerDisabled)
	assert.ErrorIs(t, func() error {
		_, err := GetPersonalOwner()
		return err
	}(), ErrPersonalOwnerDisabled)
}

func TestValidatePersonalOwnerRejectsHistoricalUser(t *testing.T) {
	db := setupPersonalOwnerTestDB(t)
	require.NoError(t, db.Create(&User{
		Id: 10, Username: "root", Password: "password", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&User{
		Id: 11, Username: "historical-user", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
	}).Error)

	require.NoError(t, ValidatePersonalOwner(10))
	assert.ErrorIs(t, ValidatePersonalOwner(11), ErrPersonalOwnerMismatch)
}
