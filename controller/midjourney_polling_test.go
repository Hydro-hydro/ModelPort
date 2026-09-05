package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMidjourneyPollingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMemoryCache := common.MemoryCacheEnabled
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Midjourney{}, &model.Channel{}, &model.User{}, &model.Log{}))
	model.DB = database
	model.LOG_DB = database
	common.MemoryCacheEnabled = false
	service.InitHttpClient()
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.MemoryCacheEnabled = previousMemoryCache
	})
	return database
}

func TestMidjourneyPollingNullIDUsesTerminalCAS(t *testing.T) {
	database := setupMidjourneyPollingTestDB(t)
	task := &model.Midjourney{
		UserId:   1,
		Status:   "",
		Progress: "0%",
		Quota:    0,
	}
	require.NoError(t, database.Create(task).Error)

	summary := runMidjourneyTaskUpdateOnce(context.Background(), nil)

	assert.Equal(t, 1, summary.NullTasksFailed)
	var persisted model.Midjourney
	require.NoError(t, database.First(&persisted, task.Id).Error)
	assert.Equal(t, "FAILURE", persisted.Status)
	assert.Equal(t, "100%", persisted.Progress)
	assert.Contains(t, persisted.FailReason, "上游任务 ID 缺失")

	// The terminal row is no longer returned by the unfinished query, so a
	// second polling pass cannot repeat the transition or billing action.
	second := runMidjourneyTaskUpdateOnce(context.Background(), nil)
	assert.Zero(t, second.UnfinishedTasks)
}

func TestMidjourneyPollingCacheFailureLeavesTaskPending(t *testing.T) {
	database := setupMidjourneyPollingTestDB(t)
	task := &model.Midjourney{
		UserId:    2,
		MjId:      "upstream-cache-miss",
		Status:    "IN_PROGRESS",
		Progress:  "30%",
		ChannelId: 999,
		Quota:     1234,
	}
	require.NoError(t, database.Create(task).Error)

	summary := runMidjourneyTaskUpdateOnce(context.Background(), nil)

	assert.Equal(t, 1, summary.UnfinishedTasks)
	var persisted model.Midjourney
	require.NoError(t, database.First(&persisted, task.Id).Error)
	assert.Equal(t, "IN_PROGRESS", persisted.Status)
	assert.Equal(t, "30%", persisted.Progress)
	assert.Equal(t, 1234, persisted.Quota)
	assert.Empty(t, persisted.FailReason)
}

func TestMidjourneyPollingFailureWithoutProgressRefundsTerminalTask(t *testing.T) {
	database := setupMidjourneyPollingTestDB(t)
	now := time.Now().UnixMilli()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{"id":"upstream-failure-no-progress","status":"FAILURE","submitTime":%d}]`, now)
	}))
	defer server.Close()

	channel := &model.Channel{
		Id:      123,
		Name:    "midjourney-failure",
		Key:     "secret",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &server.URL,
	}
	require.NoError(t, database.Create(channel).Error)
	task := &model.Midjourney{
		UserId:     3,
		MjId:       "upstream-failure-no-progress",
		Status:     "IN_PROGRESS",
		Progress:   "30%",
		ChannelId:  channel.Id,
		SubmitTime: now,
	}
	require.NoError(t, database.Create(task).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var persisted model.Midjourney
	require.NoError(t, database.First(&persisted, task.Id).Error)
	assert.Equal(t, "FAILURE", persisted.Status)
	assert.Equal(t, "100%", persisted.Progress)
}
