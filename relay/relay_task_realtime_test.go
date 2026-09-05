package relay

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type realtimeTaskTestAdaptor struct {
	adjustCalls int
}

func (a *realtimeTaskTestAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *realtimeTaskTestAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}

func (a *realtimeTaskTestAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (a *realtimeTaskTestAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	a.adjustCalls++
	return 0
}

func setupRealtimeTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Task{}, &model.BillingOperation{}))
	model.DB = database
	t.Cleanup(func() { model.DB = previousDB })
	return database
}

func TestApplyRealtimeTaskResultUsesCASAndFinalizesBillingOnce(t *testing.T) {
	database := setupRealtimeTaskTestDB(t)
	task := &model.Task{
		TaskID:   "realtime-cas",
		Platform: "vertex-ai",
		Status:   model.TaskStatusInProgress,
		Progress: "40%",
		PrivateData: model.TaskPrivateData{
			BillingOperationKey: model.BillingOperationKeyForTask("realtime-cas"),
		},
	}
	_, err := model.EnsureTaskBillingOperation(task)
	require.NoError(t, err)
	require.NoError(t, database.Create(task).Error)
	stale := *task
	adaptor := &realtimeTaskTestAdaptor{}

	updated := applyRealtimeTaskResult(context.Background(), task, adaptor, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess,
		Url:    "https://example.com/video.mp4",
	})

	assert.True(t, updated)
	assert.Equal(t, 1, adaptor.adjustCalls)
	var persisted model.Task
	require.NoError(t, database.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, persisted.Status)
	assert.Equal(t, "100%", persisted.Progress)
	assert.Equal(t, "https://example.com/video.mp4", persisted.PrivateData.ResultURL)
	require.NotZero(t, persisted.FinishTime)

	// A stale realtime reader cannot overwrite the terminal result or invoke
	// the billing adjustment a second time.
	assert.False(t, applyRealtimeTaskResult(context.Background(), &stale, adaptor, &relaycommon.TaskInfo{
		Status: model.TaskStatusFailure,
		Reason: "stale failure",
	}))
	require.NoError(t, database.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, persisted.Status)
	assert.Equal(t, 1, adaptor.adjustCalls)
}

func TestApplyRealtimeTaskResultIgnoresEmptyObservation(t *testing.T) {
	database := setupRealtimeTaskTestDB(t)
	task := &model.Task{
		TaskID:   "realtime-empty",
		Platform: "vertex-ai",
		Status:   model.TaskStatusInProgress,
		Progress: "40%",
	}
	require.NoError(t, database.Create(task).Error)

	assert.False(t, applyRealtimeTaskResult(context.Background(), task, &realtimeTaskTestAdaptor{}, &relaycommon.TaskInfo{}))
	var persisted model.Task
	require.NoError(t, database.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, persisted.Status)
	assert.Equal(t, "40%", persisted.Progress)
}
