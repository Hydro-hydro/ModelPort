package model

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func testBillingOperationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()

	tableName := fmt.Sprintf("billing_operation_migration_%d", time.Now().UnixNano())
	tableDB := db.Table(tableName)
	t.Cleanup(func() { _ = db.Migrator().DropTable(tableName) })

	// Running the migration twice models both a fresh database and an upgrade
	// restart. The second pass must not alter or reject the existing schema.
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

func TestBillingOperationMigrationSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	testBillingOperationMigration(t, db)
}

func TestBillingOperationMigrationMySQL(t *testing.T) {
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

func TestBillingOperationMigrationPostgreSQL(t *testing.T) {
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
