package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefundTaskQuotaAfterTerminalReversesSettledOperationOnce(t *testing.T) {
	truncate(t)

	const (
		userID       = 970
		tokenID      = 970
		channelID    = 970
		initialUser  = 10_000
		charged      = 3_000
		initialToken = 5_000
	)
	seedUser(t, userID, initialUser-charged)
	seedToken(t, tokenID, userID, "sk-terminal-refund", initialToken-charged)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("used_quota", charged).Error)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", charged).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).Update("used_quota", charged).Error)

	task := makeTask(userID, channelID, charged, tokenID, BillingSourceWallet)
	task.TaskID = "settled-task-terminal-refund"
	task.Status = model.TaskStatusFailure
	task.FailReason = "upstream failed after submission"
	task.SubmitTime = time.Now().Unix()
	requestKey := "request:settled-task-terminal-refund"
	task.PrivateData.BillingOperationKey = requestKey
	require.NoError(t, model.DB.Create(task).Error)

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     requestKey,
		RequestID:        "settled-task-terminal-refund",
		TaskID:           task.TaskID,
		UserID:           userID,
		TokenID:          tokenID,
		ChannelID:        channelID,
		PreConsumedQuota: charged,
		ActualQuota:      charged,
	})
	require.NoError(t, err)
	for _, component := range []string{model.BillingComponentFunding, model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(operation.OperationKey, component))
	}
	settled, err := model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationSettled, "", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, settled)

	assert.True(t, RefundTaskQuotaAfterTerminal(context.Background(), task, task.FailReason))
	assert.Equal(t, initialUser, getUserQuota(t, userID))
	assert.Equal(t, initialToken, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	assert.Zero(t, getTaskQuota(t, task.ID))

	updated, err := model.GetBillingOperation(requestKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, updated.Status)

	assert.True(t, RefundTaskQuotaAfterTerminal(context.Background(), task, task.FailReason))
	assert.Equal(t, initialUser, getUserQuota(t, userID))
	assert.Equal(t, initialToken, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestRefundTaskQuotaAfterTerminalOwnedRejectsExpiredWorker(t *testing.T) {
	truncate(t)

	const (
		userID       = 972
		tokenID      = 972
		channelID    = 972
		initialUser  = 10_000
		charged      = 300
		initialToken = 5_000
	)
	seedUser(t, userID, initialUser-charged)
	seedToken(t, tokenID, userID, "sk-owned-refund", initialToken-charged)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("used_quota", charged).Error)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", charged).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).Update("used_quota", charged).Error)

	task := makeTask(userID, channelID, charged, tokenID, BillingSourceWallet)
	task.TaskID = "owned-refund-expired-worker"
	task.Status = model.TaskStatusFailure
	task.FailReason = "upstream failed"
	task.SubmitTime = time.Now().Unix()
	require.NoError(t, model.DB.Create(task).Error)
	operation, err := model.EnsureTaskBillingOperation(task)
	require.NoError(t, err)

	now := common.GetTimestamp()
	claimed, err := model.ClaimBillingOperations("worker-a", now, 60, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, model.DB.Model(&model.BillingOperation{}).
		Where("operation_key = ?", operation.OperationKey).
		Update("lease_until", now-1).Error)

	assert.False(t, RefundTaskQuotaAfterTerminalOwned(context.Background(), task, task.FailReason, "worker-a"))
	assert.Equal(t, initialUser-charged, getUserQuota(t, userID))
	assert.Equal(t, initialToken-charged, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, charged, getTaskQuota(t, task.ID))

	claimed, err = model.ClaimBillingOperations("worker-b", common.GetTimestamp(), 60, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.True(t, RefundTaskQuotaAfterTerminalOwned(context.Background(), task, task.FailReason, "worker-b"))
	assert.Equal(t, initialUser, getUserQuota(t, userID))
	assert.Equal(t, initialToken, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTaskQuota(t, task.ID))
}
