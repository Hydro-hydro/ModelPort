package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupStartupPrecheckDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previousDB := DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		initCol()
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestValidateCurrentDatabaseAllowsTrulyEmptyDatabase(t *testing.T) {
	db := setupStartupPrecheckDB(t)

	require.NoError(t, ValidateCurrentDatabase())
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	assert.Empty(t, tables)
}

func TestValidateCurrentDatabaseRejectsPopulatedDatabaseWithoutSetup(t *testing.T) {
	db := setupStartupPrecheckDB(t)
	require.NoError(t, db.AutoMigrate(&User{}))
	require.NoError(t, db.Create(&User{
		Id:       1,
		Username: "root",
		Password: "password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	err := ValidateCurrentDatabase()
	assert.ErrorIs(t, err, ErrSetupRecordMissing)
	tables, listErr := db.Migrator().GetTables()
	require.NoError(t, listErr)
	assert.Equal(t, []string{"users"}, tables)
	assert.False(t, db.Migrator().HasTable(&Setup{}))
}

func TestValidateCurrentDatabaseRejectsMismatchedSetupBeforeOtherReads(t *testing.T) {
	db := setupStartupPrecheckDB(t)
	require.NoError(t, db.AutoMigrate(&Setup{}))
	require.NoError(t, db.Create(&Setup{
		Version:       "legacy",
		InitializedAt: 1,
		Edition:       "new-api",
		SchemaVersion: 0,
	}).Error)

	err := ValidateCurrentDatabase()
	assert.ErrorIs(t, err, ErrSetupSchemaMismatch)
	tables, listErr := db.Migrator().GetTables()
	require.NoError(t, listErr)
	assert.Equal(t, []string{"setups"}, tables)
}

func TestValidateCurrentDatabaseAllowsCurrentEmptySchemaForSetupWizard(t *testing.T) {
	db := setupStartupPrecheckDB(t)
	usage_mode.SetPersistedOptionalFeatures(nil)
	for _, feature := range usage_mode.OptionalFeatures() {
		t.Setenv("MODELPORT_ENABLE_"+strings.ToUpper(string(feature)), "false")
	}
	require.NoError(t, migrateDB())

	require.NoError(t, ValidateCurrentDatabase())
	assert.True(t, db.Migrator().HasTable(&Setup{}))
	var setupCount int64
	require.NoError(t, db.Model(&Setup{}).Count(&setupCount).Error)
	assert.Zero(t, setupCount)
}

func TestValidateCurrentDatabaseAllowsBootstrapRowsBeforeSetup(t *testing.T) {
	db := setupStartupPrecheckDB(t)
	usage_mode.SetPersistedOptionalFeatures(nil)
	for _, feature := range usage_mode.OptionalFeatures() {
		t.Setenv("MODELPORT_ENABLE_"+strings.ToUpper(string(feature)), "false")
	}
	require.NoError(t, migrateDB())
	require.NoError(t, db.Create(&LoginEncryptionKey{
		Slot:          activeLoginEncryptionKeySlot,
		PrivateKeyPEM: "test-key",
	}).Error)
	require.NoError(t, db.Create(&AuthzRole{
		Key:     "root",
		Name:    "Root",
		BuiltIn: true,
		Enabled: true,
	}).Error)
	require.NoError(t, db.Create(&CasbinRule{
		Ptype: "p",
		V0:    "admin",
		V1:    "channel",
		V2:    "read",
	}).Error)

	require.NoError(t, ValidateCurrentDatabase())
}

func TestValidateCurrentDatabaseRejectsBusinessRowsAlongsideBootstrapRows(t *testing.T) {
	db := setupStartupPrecheckDB(t)
	usage_mode.SetPersistedOptionalFeatures(nil)
	for _, feature := range usage_mode.OptionalFeatures() {
		t.Setenv("MODELPORT_ENABLE_"+strings.ToUpper(string(feature)), "false")
	}
	require.NoError(t, migrateDB())
	require.NoError(t, db.Create(&AuthzRole{
		Key:     "root",
		Name:    "Root",
		BuiltIn: true,
		Enabled: true,
	}).Error)
	require.NoError(t, db.Create(&User{
		Id:       9,
		Username: "root",
		Password: "password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	err := ValidateCurrentDatabase()
	assert.ErrorIs(t, err, ErrSetupRecordMissing)
}

func TestValidateCurrentDatabaseRejectsCurrentSetupWithMissingCoreTable(t *testing.T) {
	db := setupStartupPrecheckDB(t)
	require.NoError(t, db.AutoMigrate(&Setup{}))
	require.NoError(t, db.Create(&Setup{
		Version:       "current",
		InitializedAt: 1,
		Edition:       SetupEditionModelPort,
		SchemaVersion: CurrentSchemaVersion,
	}).Error)

	err := ValidateCurrentDatabase()
	assert.ErrorIs(t, err, ErrDatabaseSchemaMismatch)
}

func TestValidateCurrentDatabaseRejectsMissingCurrentColumn(t *testing.T) {
	db := setupStartupPrecheckDB(t)
	usage_mode.SetPersistedOptionalFeatures(nil)
	for _, feature := range usage_mode.OptionalFeatures() {
		t.Setenv("MODELPORT_ENABLE_"+strings.ToUpper(string(feature)), "false")
	}
	require.NoError(t, migrateDB())
	require.NoError(t, db.Migrator().DropColumn(&BillingOperation{}, "operation_key"))

	err := ValidateCurrentDatabase()
	assert.ErrorIs(t, err, ErrDatabaseSchemaMismatch)
	assert.Contains(t, err.Error(), "billing_operations")
	assert.Contains(t, err.Error(), "operation_key")
}

func TestValidateCurrentLogDatabaseRejectsMissingCurrentColumn(t *testing.T) {
	db := setupStartupPrecheckDB(t)
	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Migrator().DropColumn(&Log{}, "billing_operation_key"))
	useLogSchemaTestDB(t, db)

	err := ValidateCurrentLogDatabase()
	assert.ErrorIs(t, err, ErrDatabaseSchemaMismatch)
	assert.Contains(t, err.Error(), "logs.billing_operation_key")
}
