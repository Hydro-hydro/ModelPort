package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openMainSchemaTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func useMainSchemaTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()

	previousDB := DB
	previousMainDatabaseType := common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousMainDatabaseType)
	})
}

func useLogSchemaTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()

	previousLogDB := LOG_DB
	previousLogDatabaseType := common.LogDatabaseType()
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousLogDatabaseType)
	})
}

func installCurrentCoreSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	useMainSchemaTestDB(t, db)
	require.NoError(t, migrateDB())
}

func TestRejectLegacySchemaRejectsNonEmptyDatabaseWithoutCurrentIdentity(t *testing.T) {
	db := openMainSchemaTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, quota INTEGER)").Error)
	useMainSchemaTestDB(t, db)

	err := rejectLegacySchema()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a current ModelPort personal schema")
	assert.False(t, db.Migrator().HasTable(&Setup{}))
}

func TestRejectLegacySchemaAllowsEmptyDatabase(t *testing.T) {
	db := openMainSchemaTestDB(t)
	useMainSchemaTestDB(t, db)

	require.NoError(t, rejectLegacySchema())
}

func TestRejectLegacySchemaDoesNotAddCurrentIdentityToExistingSetupTable(t *testing.T) {
	db := openMainSchemaTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE setups (id INTEGER PRIMARY KEY, version TEXT, initialized_at INTEGER)").Error)
	useMainSchemaTestDB(t, db)

	err := rejectLegacySchema()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a current ModelPort personal schema")
	assert.False(t, db.Migrator().HasColumn(&Setup{}, "edition"))
	assert.False(t, db.Migrator().HasColumn(&Setup{}, "schema_version"))
}

func TestRejectLegacySchemaAllowsCurrentUninitializedSchemaWithoutUsers(t *testing.T) {
	db := openMainSchemaTestDB(t)
	installCurrentCoreSchema(t, db)

	require.NoError(t, rejectLegacySchema())
	require.NoError(t, migrateDB())
}

func TestRejectLegacySchemaAllowsCurrentInitializedSchema(t *testing.T) {
	db := openMainSchemaTestDB(t)
	installCurrentCoreSchema(t, db)
	require.NoError(t, db.Create(&Setup{
		Version:       "test",
		InitializedAt: 1,
		Edition:       SetupEditionModelPort,
		SchemaVersion: CurrentSchemaVersion,
	}).Error)

	require.NoError(t, rejectLegacySchema())
}

func TestRejectLegacySchemaRejectsWrongSetupIdentity(t *testing.T) {
	db := openMainSchemaTestDB(t)
	installCurrentCoreSchema(t, db)
	require.NoError(t, db.Create(&Setup{
		Version:       "test",
		InitializedAt: 1,
		Edition:       "other-edition",
		SchemaVersion: CurrentSchemaVersion,
	}).Error)

	err := rejectLegacySchema()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not identify the current ModelPort personal schema")
}

func TestRejectLegacySchemaRejectsUsersWithoutSetupRecord(t *testing.T) {
	db := openMainSchemaTestDB(t)
	installCurrentCoreSchema(t, db)
	require.NoError(t, db.Create(&User{
		Username: "root",
		Password: "password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	err := rejectLegacySchema()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "contains users but no current setup record")
}

func TestMigrateLogDBRejectsMissingCurrentColumnWithoutMutatingIt(t *testing.T) {
	db := openMainSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Migrator().DropColumn(&Log{}, "billing_operation_key"))
	useLogSchemaTestDB(t, db)

	err := migrateLOGDB()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "logs.billing_operation_key")
	assert.False(t, db.Migrator().HasColumn(&Log{}, "billing_operation_key"))
}

func TestMigrateLogDBRejectsMissingBillingOperationIndex(t *testing.T) {
	db := openMainSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Migrator().DropIndex(&Log{}, "idx_logs_billing_operation_key"))
	useLogSchemaTestDB(t, db)

	err := migrateLOGDB()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "billing operation index")
	assert.False(t, db.Migrator().HasIndex(&Log{}, "idx_logs_billing_operation_key"))
}

func TestMigrateLogDBInitializesCurrentSchemaIdempotently(t *testing.T) {
	db := openMainSchemaTestDB(t)
	useLogSchemaTestDB(t, db)

	require.NoError(t, migrateLOGDB())
	require.NoError(t, migrateLOGDB())
	assert.True(t, db.Migrator().HasTable(&Log{}))
	assert.True(t, db.Migrator().HasColumn(&Log{}, "billing_operation_key"))
}

func TestLoadPersistedOptionalFeatureSettings(t *testing.T) {
	db := openMainSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	require.NoError(t, db.Create(&Option{
		Key:   usage_mode.OptionalFeatureOptionKey(usage_mode.FeatureTaskPlugins),
		Value: "true",
	}).Error)

	previousDB := DB
	DB = db
	usage_mode.SetPersistedOptionalFeatures(nil)
	t.Cleanup(func() {
		DB = previousDB
		usage_mode.SetPersistedOptionalFeatures(nil)
	})

	require.NoError(t, loadPersistedOptionalFeatureSettings())
	assert.True(t, usage_mode.IsFeatureEnabled(usage_mode.FeatureTaskPlugins))
	assert.True(t, usage_mode.IsFeatureEnabled(usage_mode.FeatureSystemTasks))
}

func TestLoadPersistedOptionalFeatureSettingsRejectsInvalidValue(t *testing.T) {
	db := openMainSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	require.NoError(t, db.Create(&Option{
		Key:   usage_mode.OptionalFeatureOptionKey(usage_mode.FeatureMediaTasks),
		Value: "maybe",
	}).Error)

	previousDB := DB
	DB = db
	usage_mode.SetPersistedOptionalFeatures(nil)
	t.Cleanup(func() {
		DB = previousDB
		usage_mode.SetPersistedOptionalFeatures(nil)
	})

	err := loadPersistedOptionalFeatureSettings()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feature.media_tasks")
}

func TestOptionalFeatureOptionAppliesAfterReload(t *testing.T) {
	db := openMainSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))

	previousDB := DB
	previousOptionMap := common.OptionMap
	DB = db
	common.OptionMap = make(map[string]string)
	usage_mode.SetPersistedOptionalFeatures(nil)
	t.Setenv("MODELPORT_ENABLE_TASK_PLUGINS", "false")
	t.Cleanup(func() {
		DB = previousDB
		common.OptionMap = previousOptionMap
		usage_mode.SetPersistedOptionalFeatures(nil)
	})

	require.NoError(t, UpdateOption(
		usage_mode.OptionalFeatureOptionKey(usage_mode.FeatureTaskPlugins),
		"true",
	))
	assert.False(t, usage_mode.IsFeatureEnabled(usage_mode.FeatureTaskPlugins))

	require.NoError(t, loadPersistedOptionalFeatureSettings())
	assert.True(t, usage_mode.IsFeatureEnabled(usage_mode.FeatureTaskPlugins))
}
