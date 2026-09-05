package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBatchUpdateRequeuesFailedWrites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Channel{}, &Token{}))

	previousDB := DB
	previousBatchEnabled := common.BatchUpdateEnabled
	previousStores := batchUpdateStores
	previousLocks := batchUpdateLocks
	DB = db
	common.BatchUpdateEnabled = true
	batchUpdateStores = make([]map[int]int, BatchUpdateTypeCount)
	batchUpdateLocks = make([]sync.Mutex, BatchUpdateTypeCount)
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateStores[i] = make(map[int]int)
	}
	t.Cleanup(func() {
		DB = previousDB
		common.BatchUpdateEnabled = previousBatchEnabled
		batchUpdateStores = previousStores
		batchUpdateLocks = previousLocks
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	addNewRecord(BatchUpdateTypeTokenQuota, 10, 5)
	addNewRecord(BatchUpdateTypeChannelUsedQuota, 11, 7)
	addNewRecord(BatchUpdateTypeUsedQuota, 12, 9)
	addNewRecord(BatchUpdateTypeRequestCount, 12, 2)

	batchUpdate()

	batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
	assert.Equal(t, 5, batchUpdateStores[BatchUpdateTypeTokenQuota][10])
	batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()
	batchUpdateLocks[BatchUpdateTypeChannelUsedQuota].Lock()
	assert.Equal(t, 7, batchUpdateStores[BatchUpdateTypeChannelUsedQuota][11])
	batchUpdateLocks[BatchUpdateTypeChannelUsedQuota].Unlock()
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Lock()
	assert.Equal(t, 9, batchUpdateStores[BatchUpdateTypeUsedQuota][12])
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Unlock()
	batchUpdateLocks[BatchUpdateTypeRequestCount].Lock()
	assert.Equal(t, 2, batchUpdateStores[BatchUpdateTypeRequestCount][12])
	batchUpdateLocks[BatchUpdateTypeRequestCount].Unlock()
}
