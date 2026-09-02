package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCleanupRemovedPersonalSchemaIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetDatabaseTypes(previousMain, previousLog) })

	require.NoError(t, db.AutoMigrate(&User{}, &Option{}))
	require.NoError(t, db.Create(&User{Id: 1001, Username: "legacy-admin", Password: "password", Quota: 1000}).Error)

	for _, column := range removedPersonalUserColumns {
		require.NoError(t, db.Exec("ALTER TABLE `users` ADD COLUMN `"+column+"` TEXT").Error)
	}
	for _, table := range removedPersonalTables {
		require.NoError(t, db.Exec("CREATE TABLE `"+table+"` (`id` INTEGER)").Error)
	}
	for _, key := range []string{"PayAddress", "payment_setting.amount_options", "checkin_setting.enabled", "ModelRatio"} {
		require.NoError(t, db.Create(&Option{Key: key, Value: "legacy"}).Error)
	}

	require.NoError(t, cleanupRemovedPersonalSchema(db))
	require.NoError(t, cleanupRemovedPersonalSchema(db))

	for _, table := range removedPersonalTables {
		assert.False(t, db.Migrator().HasTable(table), table)
	}
	for _, column := range removedPersonalUserColumns {
		assert.False(t, db.Migrator().HasColumn("users", column), column)
	}
	assert.True(t, db.Migrator().HasIndex("users", "idx_users_username"))
	assert.False(t, db.Migrator().HasTable("users__temp"))

	var user User
	require.NoError(t, db.First(&user, 1001).Error)
	assert.Equal(t, "legacy-admin", user.Username)
	assert.Equal(t, 1000, user.Quota)

	var options []Option
	require.NoError(t, db.Find(&options).Error)
	require.Len(t, options, 1)
	assert.Equal(t, "ModelRatio", options[0].Key)
}
