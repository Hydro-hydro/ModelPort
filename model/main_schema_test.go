package model

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/glebarez/sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
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
	useMainSchemaTestDBForType(t, db, common.DatabaseTypeSQLite)
}

func useMainSchemaTestDBForType(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()

	previousDB := DB
	previousMainDatabaseType := common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(databaseType)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousMainDatabaseType)
	})
}

func openConfiguredMainSchemaTestDB(t *testing.T, envKey string, databaseType common.DatabaseType) *gorm.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(envKey))
	if dsn == "" {
		t.Skipf("%s is not configured", envKey)
	}

	var dialector gorm.Dialector
	switch databaseType {
	case common.DatabaseTypeMySQL:
		config, err := mysql.ParseDSN(dsn)
		require.NoError(t, err)
		config.ParseTime = true
		dialector = gormmysql.Open(config.FormatDSN())
	case common.DatabaseTypePostgreSQL:
		dialector = postgres.New(postgres.Config{
			DSN:                  dsn,
			PreferSimpleProtocol: true,
		})
	default:
		t.Fatalf("unsupported database type: %s", databaseType)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}

func assertCurrentCoreSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, coreModel := range []interface{}{
		&Channel{}, &Token{}, &User{}, &UserSession{}, &Option{},
		&LoginEncryptionKey{}, &Ability{}, &Log{}, &QuotaData{},
		&Model{}, &Vendor{}, &PrefillGroup{}, &Setup{}, &PerfMetric{},
		&BillingOperation{}, &CasbinRule{}, &AuthzRole{},
	} {
		assert.True(t, db.Migrator().HasTable(coreModel))
	}
}

func assertOptionalSchemaDisabled(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, optionalModel := range []interface{}{
		&Task{}, &TaskPlugin{}, &Midjourney{}, &SystemTask{},
		&SystemTaskLock{}, &SystemInstance{},
	} {
		assert.False(t, db.Migrator().HasTable(optionalModel))
	}
}

func testMigrateDBInitializesCurrentSchema(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	useMainSchemaTestDBForType(t, db, databaseType)
	usage_mode.SetPersistedOptionalFeatures(nil)
	for _, feature := range usage_mode.OptionalFeatures() {
		t.Setenv("MODELPORT_ENABLE_"+strings.ToUpper(string(feature)), "false")
	}
	t.Cleanup(func() { usage_mode.SetPersistedOptionalFeatures(nil) })

	require.NoError(t, migrateDB())
	require.NoError(t, migrateDB())
	assertCurrentCoreSchema(t, db)
	assertOptionalSchemaDisabled(t, db)

	usage_mode.SetPersistedOptionalFeatures(map[usage_mode.Feature]bool{
		usage_mode.FeatureMediaTasks: true,
	})
	require.NoError(t, migrateDB())
	require.NoError(t, migrateDB())
	assert.True(t, db.Migrator().HasTable(&Task{}))
	assert.True(t, db.Migrator().HasTable(&Midjourney{}))
	assert.True(t, db.Migrator().HasTable(&SystemTask{}))
	assert.True(t, db.Migrator().HasTable(&SystemTaskLock{}))
	assert.False(t, db.Migrator().HasTable(&TaskPlugin{}))
	assert.False(t, db.Migrator().HasTable(&SystemInstance{}))
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

func TestMigrateDBInitializesCurrentCoreSchemaIdempotently(t *testing.T) {
	db := openMainSchemaTestDB(t)
	useMainSchemaTestDB(t, db)
	usage_mode.SetPersistedOptionalFeatures(nil)
	for _, feature := range usage_mode.OptionalFeatures() {
		t.Setenv("MODELPORT_ENABLE_"+strings.ToUpper(string(feature)), "false")
	}
	t.Cleanup(func() {
		usage_mode.SetPersistedOptionalFeatures(nil)
	})

	require.NoError(t, migrateDB())
	require.NoError(t, migrateDB())

	assertCurrentCoreSchema(t, db)
	assertOptionalSchemaDisabled(t, db)
}

func TestMigrateDBInitializesCurrentSchemaMySQL(t *testing.T) {
	db := openConfiguredMainSchemaTestDB(t, "TEST_MYSQL_DSN", common.DatabaseTypeMySQL)
	testMigrateDBInitializesCurrentSchema(t, db, common.DatabaseTypeMySQL)
}

func TestMigrateDBInitializesCurrentSchemaPostgreSQL(t *testing.T) {
	db := openConfiguredMainSchemaTestDB(t, "TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL)
	testMigrateDBInitializesCurrentSchema(t, db, common.DatabaseTypePostgreSQL)
}

func TestMigrateDBCreatesEnabledMediaTaskSchema(t *testing.T) {
	db := openMainSchemaTestDB(t)
	useMainSchemaTestDB(t, db)
	usage_mode.SetPersistedOptionalFeatures(map[usage_mode.Feature]bool{
		usage_mode.FeatureMediaTasks: true,
	})
	t.Cleanup(func() {
		usage_mode.SetPersistedOptionalFeatures(nil)
	})

	require.NoError(t, migrateDB())
	assert.True(t, db.Migrator().HasTable(&Task{}))
	assert.True(t, db.Migrator().HasTable(&Midjourney{}))
	assert.True(t, db.Migrator().HasTable(&SystemTask{}))
	assert.True(t, db.Migrator().HasTable(&SystemTaskLock{}))
	assert.False(t, db.Migrator().HasTable(&TaskPlugin{}))
	assert.False(t, db.Migrator().HasTable(&SystemInstance{}))
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
