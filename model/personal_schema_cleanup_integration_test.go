package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPersonalSchemaCleanupMySQL(t *testing.T) {
	testPersonalSchemaCleanupOnDSN(t, common.DatabaseTypeMySQL, "PERSONAL_SCHEMA_MYSQL_DSN", os.Getenv("PERSONAL_SCHEMA_MYSQL_DSN"), mysql.Open)
}

func TestPersonalSchemaCleanupPostgreSQL(t *testing.T) {
	testPersonalSchemaCleanupOnDSN(t, common.DatabaseTypePostgreSQL, "PERSONAL_SCHEMA_POSTGRES_DSN", os.Getenv("PERSONAL_SCHEMA_POSTGRES_DSN"), postgres.Open)
}

func testPersonalSchemaCleanupOnDSN(t *testing.T, databaseType common.DatabaseType, envName, dsn string, dialector func(string) gorm.Dialector) {
	t.Helper()
	if dsn == "" {
		t.Skipf("set %s to run this database integration test", envName)
	}

	db, err := gorm.Open(dialector(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	quote := func(identifier string) string {
		if databaseType == common.DatabaseTypePostgreSQL {
			return `"` + identifier + `"`
		}
		return "`" + identifier + "`"
	}
	resetPersonalSchemaIntegrationDatabase(t, db, quote)

	require.NoError(t, db.AutoMigrate(&User{}, &Option{}, &Token{}))
	for _, table := range removedPersonalTables {
		assert.False(t, db.Migrator().HasTable(table), table)
	}

	require.NoError(t, db.Create(&User{Id: 1001, Username: "legacy-admin", Password: "password", Quota: 1000}).Error)
	require.NoError(t, db.Create(&Token{Id: 2001, UserId: 1001, Key: "sk-preserved-token"}).Error)

	for _, column := range removedPersonalUserColumns {
		require.NoError(t, db.Exec("ALTER TABLE "+quote("users")+" ADD COLUMN "+quote(column)+" TEXT").Error)
	}
	for _, table := range removedPersonalTables {
		require.NoError(t, db.Exec("CREATE TABLE "+quote(table)+" ("+quote("id")+" BIGINT)").Error)
	}
	for _, key := range []string{"GitHubOAuthEnabled", "discord.enabled", "oidc.enabled", "passkey.enabled", "ModelRatio"} {
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

	var user User
	require.NoError(t, db.First(&user, 1001).Error)
	assert.Equal(t, "legacy-admin", user.Username)
	var token Token
	require.NoError(t, db.First(&token, 2001).Error)
	assert.Equal(t, "sk-preserved-token", token.Key)

	var options []Option
	require.NoError(t, db.Find(&options).Error)
	require.Len(t, options, 1)
	assert.Equal(t, "ModelRatio", options[0].Key)
}

func resetPersonalSchemaIntegrationDatabase(t *testing.T, db *gorm.DB, quote func(string) string) {
	t.Helper()
	for _, table := range append(append([]string{}, removedPersonalTables...), "tokens", "options", "users") {
		require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+quote(table)).Error)
	}
}
