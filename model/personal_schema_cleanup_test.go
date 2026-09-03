package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openPersonalSchemaCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestPersonalModelsDoNotRecreateRemovedAuthenticationTables(t *testing.T) {
	db := openPersonalSchemaCleanupTestDB(t)
	require.NoError(t, db.AutoMigrate(&User{}, &Option{}))

	for _, table := range removedPersonalTables {
		assert.False(t, db.Migrator().HasTable(table), table)
	}
}

func TestCleanupRemovedPersonalSchemaIsIdempotent(t *testing.T) {
	db := openPersonalSchemaCleanupTestDB(t)

	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()
	t.Cleanup(func() {
		common.SetDatabaseTypes(previousMain, previousLog)
		initCol()
	})

	require.NoError(t, db.AutoMigrate(
		&User{},
		&Option{},
		&Token{},
		&Channel{},
		&Log{},
		&Task{},
		&QuotaData{},
		&PerfMetric{},
		&SystemInstance{},
	))
	require.NoError(t, db.Create(&User{Id: 1001, Username: "legacy-admin", Password: "password", Quota: 1000}).Error)
	require.NoError(t, db.Create(&Token{Id: 2001, UserId: 1001, Key: "sk-preserved-token"}).Error)
	require.NoError(t, db.Create(&Channel{Id: 3001, Name: "preserved-channel", Key: "channel-key"}).Error)
	require.NoError(t, db.Create(&Log{Id: 4001, UserId: 1001, Content: "preserved-log"}).Error)
	require.NoError(t, db.Create(&Task{ID: 5001, TaskID: "preserved-task", UserId: 1001}).Error)
	require.NoError(t, db.Create(&QuotaData{Id: 6001, UserID: 1001, ModelName: "preserved-model"}).Error)
	require.NoError(t, db.Create(&PerfMetric{Id: 7001, ModelName: "preserved-model", Group: "default", BucketTs: 1, RequestCount: 1}).Error)
	require.NoError(t, db.Create(&SystemInstance{NodeName: "preserved-node"}).Error)

	for _, column := range removedPersonalUserColumns {
		require.NoError(t, db.Exec("ALTER TABLE `users` ADD COLUMN `"+column+"` TEXT").Error)
		require.NoError(t, db.Exec("CREATE INDEX `idx_legacy_users_"+column+"` ON `users` (`"+column+"`)").Error)
	}
	for _, table := range removedPersonalTables {
		require.NoError(t, db.Exec("CREATE TABLE `"+table+"` (`id` INTEGER)").Error)
	}
	for _, key := range []string{
		"PayAddress",
		"payment_setting.amount_options",
		"checkin_setting.enabled",
		"discord.enabled",
		"oidc.enabled",
		"passkey.enabled",
		"SelfUseModeEnabled",
		"DemoSiteEnabled",
		"QuotaForNewUser",
		"DefaultCollapseSidebar",
		"UserUsableGroups",
		"group_ratio_setting.group_special_usable_group",
		"HeaderNavModules",
		"Notice",
		"About",
		"HomePageContent",
		"Footer",
		"Announcements",
		"console_setting.announcements",
		"console_setting.announcements_enabled",
		"legal.user_agreement",
		"legal.privacy_policy",
		"ModelRatio",
	} {
		require.NoError(t, db.Create(&Option{Key: key, Value: "legacy"}).Error)
	}

	require.NoError(t, cleanupRemovedPersonalSchema(db))
	require.NoError(t, cleanupRemovedPersonalSchema(db))

	for _, table := range removedPersonalTables {
		assert.False(t, db.Migrator().HasTable(table), table)
	}
	for _, column := range removedPersonalUserColumns {
		assert.False(t, db.Migrator().HasColumn("users", column), column)
		assert.False(t, db.Migrator().HasIndex("users", "idx_legacy_users_"+column), column)
	}
	assert.True(t, db.Migrator().HasIndex("users", "idx_users_username"))
	assert.False(t, db.Migrator().HasTable("users__temp"))

	var user User
	require.NoError(t, db.First(&user, 1001).Error)
	assert.Equal(t, "legacy-admin", user.Username)
	assert.Equal(t, 1000, user.Quota)

	var token Token
	require.NoError(t, db.First(&token, 2001).Error)
	assert.Equal(t, "sk-preserved-token", token.Key)

	var channel Channel
	require.NoError(t, db.First(&channel, 3001).Error)
	assert.Equal(t, "preserved-channel", channel.Name)

	var logEntry Log
	require.NoError(t, db.First(&logEntry, 4001).Error)
	assert.Equal(t, "preserved-log", logEntry.Content)

	var task Task
	require.NoError(t, db.First(&task, 5001).Error)
	assert.Equal(t, "preserved-task", task.TaskID)

	var quotaData QuotaData
	require.NoError(t, db.First(&quotaData, 6001).Error)
	assert.Equal(t, "preserved-model", quotaData.ModelName)

	var metric PerfMetric
	require.NoError(t, db.First(&metric, 7001).Error)
	assert.Equal(t, int64(1), metric.RequestCount)

	var instance SystemInstance
	require.NoError(t, db.Where("node_name = ?", "preserved-node").First(&instance).Error)
	assert.Equal(t, "preserved-node", instance.NodeName)

	var options []Option
	require.NoError(t, db.Find(&options).Error)
	require.Len(t, options, 1)
	assert.Equal(t, "ModelRatio", options[0].Key)
}

func TestRemovedPublicContentOptionsAreIgnored(t *testing.T) {
	db := openPersonalSchemaCleanupTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))

	previousDB := DB
	previousMap := common.OptionMap
	DB = db
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB = previousDB
		common.OptionMap = previousMap
	})

	for _, key := range []string{
		"Notice",
		"About",
		"HomePageContent",
		"Footer",
		"Announcements",
		"console_setting.announcements",
		"console_setting.announcements_enabled",
		"legal.user_agreement",
		"legal.privacy_policy",
	} {
		require.NoError(t, UpdateOption(key, "legacy"), key)
		_, published := common.OptionMap[key]
		assert.False(t, published, key)
	}

	var options []Option
	require.NoError(t, db.Find(&options).Error)
	assert.Empty(t, options)
}

func TestRemovedPublicContentIsNotRegisteredAsConfiguration(t *testing.T) {
	exported := config.GlobalConfig.ExportAllConfigs()
	for _, key := range []string{
		"Notice",
		"About",
		"HomePageContent",
		"Footer",
		"console_setting.announcements",
		"console_setting.announcements_enabled",
		"legal.user_agreement",
		"legal.privacy_policy",
	} {
		assert.NotContains(t, exported, key)
	}
	assert.Nil(t, config.GlobalConfig.Get("legal"))
}
