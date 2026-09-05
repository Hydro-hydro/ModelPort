package model

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/usage_mode"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func testBillingOperationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()

	tableName := fmt.Sprintf("billing_operation_migration_%d", time.Now().UnixNano())
	tableDB := db.Table(tableName)
	t.Cleanup(func() { _ = db.Migrator().DropTable(tableName) })

	// Running the current-schema initialization twice models a fresh database
	// and a repeated startup. The second pass must remain idempotent.
	require.NoError(t, tableDB.AutoMigrate(&BillingOperation{}))
	require.NoError(t, tableDB.AutoMigrate(&BillingOperation{}))
	require.True(t, db.Migrator().HasTable(tableName))

	indexName := db.NamingStrategy.IndexName(tableName, "operation_key")
	require.True(t, db.Migrator().HasIndex(tableName, indexName))

	operation := &BillingOperation{
		OperationKey:     "migration-test-operation",
		RequestID:        "migration-test-request",
		PreConsumedQuota: 10,
		ActualQuota:      7,
		Status:           BillingOperationReserved,
		NextRetryAt:      time.Now().Unix(),
		CreatedAt:        time.Now().Unix(),
		UpdatedAt:        time.Now().Unix(),
	}
	require.NoError(t, tableDB.Create(operation).Error)

	duplicate := &BillingOperation{OperationKey: operation.OperationKey}
	assert.Error(t, tableDB.Create(duplicate).Error)

}

func TestBillingOperationSchemaIdempotentSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	testBillingOperationMigration(t, db)
}

func TestBillingOperationSchemaIdempotentMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	testBillingOperationMigration(t, db)
}

func TestBillingOperationSchemaIdempotentPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	testBillingOperationMigration(t, db)
}

func testLoadPersistedOptionalFeatureSettings(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()

	require.NoError(t, db.AutoMigrate(&Option{}))
	t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&Option{})) })
	require.NoError(t, db.Create(&Option{
		Key:   usage_mode.OptionalFeatureOptionKey(usage_mode.FeatureMediaTasks),
		Value: "true",
	}).Error)

	previousDB := DB
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(databaseType, databaseType)
	initCol()
	usage_mode.SetPersistedOptionalFeatures(nil)
	t.Setenv("MODELPORT_ENABLE_MEDIA_TASKS", "false")
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		initCol()
		usage_mode.SetPersistedOptionalFeatures(nil)
	})

	require.NoError(t, loadPersistedOptionalFeatureSettings())
	assert.True(t, usage_mode.IsFeatureEnabled(usage_mode.FeatureMediaTasks))
}

func TestLoadPersistedOptionalFeatureSettingsMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("feature_options_%d_", time.Now().UnixNano())},
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	testLoadPersistedOptionalFeatureSettings(t, db, common.DatabaseTypeMySQL)
}

func TestLoadPersistedOptionalFeatureSettingsPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("feature_options_%d_", time.Now().UnixNano())},
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	testLoadPersistedOptionalFeatureSettings(t, db, common.DatabaseTypePostgreSQL)
}
