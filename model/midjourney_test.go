package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnfinishedMidjourneyQueriesUseTerminalStatusInsteadOfProgress(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Midjourney{}))
	require.NoError(t, DB.Exec("DELETE FROM midjourneys").Error)
	t.Cleanup(func() { _ = DB.Exec("DELETE FROM midjourneys").Error })

	require.NoError(t, DB.Create(&Midjourney{
		MjId:     "mj-progress-complete-but-running",
		Status:   "IN_PROGRESS",
		Progress: "100%",
	}).Error)
	require.NoError(t, DB.Create(&Midjourney{
		MjId:     "mj-progress-incomplete-success",
		Status:   "SUCCESS",
		Progress: "50%",
	}).Error)
	require.NoError(t, DB.Create(&Midjourney{
		MjId:     "mj-progress-incomplete-failure",
		Status:   "FAILURE",
		Progress: "10%",
	}).Error)

	unfinished := GetAllUnFinishTasks()
	require.Len(t, unfinished, 1)
	assert.Equal(t, "mj-progress-complete-but-running", unfinished[0].MjId)
	assert.True(t, HasUnfinishedMidjourneyTasks())

	require.NoError(t, DB.Model(&Midjourney{}).
		Where("status NOT IN ?", []string{"FAILURE", "SUCCESS"}).
		Update("status", "SUCCESS").Error)
	assert.False(t, HasUnfinishedMidjourneyTasks())
}
