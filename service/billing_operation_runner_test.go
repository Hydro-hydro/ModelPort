package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingOperationRunnerRefundsFailureTaskOnce(t *testing.T) {
	truncate(t)

	const (
		userID       = 960
		tokenID      = 960
		channelID    = 960
		preConsumed  = 3000
		initialQuota = 10000
		taskID       = "billing-runner-refund-once"
	)
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, "sk-billing-runner-refund", 5000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{"used_quota": preConsumed}).Error)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).Update("used_quota", preConsumed).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceUsage)
	task.TaskID = taskID
	task.Status = model.TaskStatusFailure
	task.FailReason = "upstream failed"
	task.SubmitTime = time.Now().Unix()
	operation := markTaskBillingChargedFixture(t, task)
	require.NoError(t, model.DB.Create(task).Error)
	require.Equal(t, model.BillingOperationApplying, operation.Status)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Refunded)
	assert.Zero(t, getUserQuota(t, userID)-initialQuota)
	assert.Equal(t, 5000+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	assert.Zero(t, getTaskQuota(t, task.ID))

	updated, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, updated.Status)
	assert.True(t, updated.RefundTokenApplied)
	assert.True(t, updated.RefundStatsApplied)
	assert.True(t, updated.RefundLogApplied)

	// A terminal operation is no longer due and cannot move the balances a
	// second time when the reconciliation pass runs again.
	second := RunBillingOperationReconciliationOnce(context.Background())
	assert.Zero(t, second.Claimed)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, 5000+preConsumed, getTokenRemainQuota(t, tokenID))
}

func TestBillingOperationRunnerRefundsPendingTaskBeforeTerminalStatus(t *testing.T) {
	truncate(t)

	const (
		userID        = 968
		tokenID       = 968
		channelID     = 968
		reserved      = 300
		initialRemain = 5_000
		taskID        = "billing-runner-pending-refund-task"
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-billing-runner-pending-refund", initialRemain-reserved)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", reserved).Error)

	task := makeTask(userID, channelID, reserved, tokenID, BillingSourceUsage)
	task.TaskID = taskID
	task.Status = model.TaskStatusInProgress
	task.PrivateData.BillingOperationKey = "request:billing-runner-pending-refund-task"
	task.SubmitTime = time.Now().Unix()
	operation := ensureTaskBillingFixture(t, task)
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Model(&model.BillingOperation{}).
		Where("operation_key = ?", operation.OperationKey).
		Updates(map[string]any{"token_reserved": true, "token_reserved_quota": reserved}).Error)
	updated, err := model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationRefundPending, "request cancelled", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, updated)

	// The task is still in progress, but the durable refund intent must win over
	// its current status so a process restart cannot strand the reservation.
	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Refunded)
	assert.Zero(t, summary.Settled)
	assert.Equal(t, initialRemain, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	assert.Zero(t, getTaskQuota(t, task.ID))

	updatedOperation, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, updatedOperation.Status)
}

func TestRefundTaskQuotaDoesNotRefundSettledOperation(t *testing.T) {
	truncate(t)

	const (
		userID    = 961
		tokenID   = 961
		channelID = 961
	)
	seedUser(t, userID, 7000)
	seedToken(t, tokenID, userID, "sk-billing-runner-settled", 2000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 3000, tokenID, BillingSourceUsage)
	task.TaskID = "billing-runner-settled-no-refund"
	task.Status = model.TaskStatusFailure
	operation := ensureTaskBillingFixture(t, task)
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.MarkBillingOperationFinalUsage(operation.OperationKey, task.Quota))
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(operation.OperationKey, component))
	}
	updated, err := model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationSettled, "", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, updated)

	assert.False(t, RefundTaskQuota(context.Background(), task, "stale failure"))
	assert.Equal(t, 7000, getUserQuota(t, userID))
	assert.Equal(t, 2000, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 3000, getTaskQuota(t, task.ID))
}

func TestBillingOperationRunnerPreservesRefundIntent(t *testing.T) {
	truncate(t)

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:billing-runner-refund-intent",
		RequestID:        "billing-runner-refund-intent",
		PreConsumedQuota: 100,
	})
	require.NoError(t, err)
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(operation.OperationKey, component))
		require.NoError(t, model.MarkBillingOperationRefundComponent(operation.OperationKey, component))
	}
	updated, err := model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationRefundPending, "request failed", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, updated)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Refunded)
	assert.Zero(t, summary.Settled)
	operation, err = model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, operation.Status)
}

func TestBillingOperationRunnerReplaysPreparedRequestLogAfterSettlementCrash(t *testing.T) {
	truncate(t)

	const (
		userID      = 979
		tokenID     = 979
		channelID   = 979
		preConsumed = 200
		actualQuota = 300
		tokenKey    = "sk-billing-runner-prepared-log"
		requestID   = "billing-runner-prepared-log"
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, tokenKey, 1_000)
	seedChannel(t, channelID)

	ctx := newPersonalBillingTestContext()
	ctx.Set(common.RequestIdKey, requestID)
	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	relayInfo.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channelID}
	require.Nil(t, PreConsumeBilling(ctx, preConsumed, relayInfo))
	session, ok := relayInfo.Billing.(*BillingSession)
	require.True(t, ok)

	logParams := model.RecordConsumeLogParams{
		ChannelId: channelID,
		ModelName: "prepared-log-model",
		Quota:     actualQuota,
		Content:   "prepared before settlement",
		TokenId:   tokenID,
		Group:     "default",
	}
	require.NoError(t, prepareBillingConsumeLog(relayInfo, logParams))

	// These are the normal pre-log side effects. The simulated crash happens
	// before recordBillingConsumeLog can write to LOG_DB.
	require.NoError(t, recordBillingUsageStats(relayInfo, actualQuota, true))
	require.NoError(t, session.Settle(actualQuota))
	operation, err := model.GetBillingOperation(session.OperationKey())
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationApplying, operation.Status)
	assert.True(t, operation.LogPayloadSet)
	assert.False(t, operation.LogApplied)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Settled)

	operation, err = model.GetBillingOperation(session.OperationKey())
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationSettled, operation.Status)
	assert.True(t, operation.LogApplied)
	assert.Equal(t, actualQuota, operation.StatsQuota)

	var logs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).
		Where("billing_operation_key = ?", session.OperationKey()).Count(&logs).Error)
	assert.Equal(t, int64(1), logs)
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, 1_000-actualQuota, getTokenRemainQuota(t, tokenID))
}

func TestBillingOperationRunnerRecoversImmediateTaskFinalUsageMarker(t *testing.T) {
	truncate(t)

	const (
		userID    = 980
		tokenID   = 980
		channelID = 980
		quota     = 240
		taskID    = "billing-runner-immediate-success"
		operation = "request:billing-runner-immediate-success"
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-billing-runner-immediate", 10_000-quota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, quota, tokenID, BillingSourceUsage)
	task.TaskID = taskID
	task.Status = model.TaskStatusSuccess
	task.SubmitTime = time.Now().Unix()
	task.FinishTime = task.SubmitTime
	task.PrivateData.BillingOperationKey = operation
	task.PrivateData.ImmediateTask = true
	created := ensureTaskBillingFixture(t, task)
	require.NoError(t, model.DB.Create(task).Error)
	require.Equal(t, model.BillingOperationReserved, created.Status)
	assert.False(t, created.FinalUsageApplied)
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(created.OperationKey, component))
	}

	// Simulate a process exit after the consume log and all component markers,
	// but before FinalizeTaskBillingOperation persisted the final-usage boundary.
	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Settled)
	assert.Zero(t, summary.Deferred)

	updated, err := model.GetBillingOperation(operation)
	require.NoError(t, err)
	assert.True(t, updated.FinalUsageApplied)
	assert.Equal(t, quota, updated.ActualQuota)
	assert.Equal(t, model.BillingOperationSettled, updated.Status)
	reloadedTask, found, err := model.GetByTaskId(userID, taskID)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, reloadedTask.PrivateData.ImmediateTask)
}

func TestBillingOperationRunnerRecoversRequestTaskFinalUsageMarker(t *testing.T) {
	truncate(t)

	const (
		userID    = 981
		tokenID   = 981
		channelID = 981
		quota     = 240
		taskID    = "billing-runner-request-success"
		operation = "request:billing-runner-request-success"
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-billing-runner-request", 10_000-quota)
	seedChannel(t, channelID)

	// This represents an asynchronous task whose process exited after the
	// terminal task row and all billing components were durable, but before the
	// request operation's final-usage marker was written.
	task := makeTask(userID, channelID, quota, tokenID, BillingSourceUsage)
	task.TaskID = taskID
	task.Status = model.TaskStatusSuccess
	task.SubmitTime = time.Now().Unix()
	task.FinishTime = task.SubmitTime
	task.PrivateData.BillingOperationKey = operation
	_, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     operation,
		RequestID:        "billing-runner-request-success",
		UserID:           userID,
		TokenID:          tokenID,
		ChannelID:        channelID,
		PreConsumedQuota: quota,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingOperationActualQuota(operation, quota))
	created, err := model.EnsureTaskBillingOperation(task)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(task).Error)
	require.Equal(t, model.BillingOperationReserved, created.Status)
	require.True(t, created.ActualQuotaSet)
	require.Equal(t, quota, created.ActualQuota)
	require.False(t, created.FinalUsageApplied)
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(created.OperationKey, component))
	}

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Settled)
	assert.Zero(t, summary.Deferred)

	updated, err := model.GetBillingOperation(operation)
	require.NoError(t, err)
	assert.True(t, updated.FinalUsageApplied)
	assert.Equal(t, quota, updated.ActualQuota)
	assert.Equal(t, model.BillingOperationSettled, updated.Status)
}

func TestBillingOperationRunnerDefersUsageRefundUntilComponentsReady(t *testing.T) {
	truncate(t)

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:billing-runner-usage-refund",
		RequestID:        "billing-runner-usage-refund",
		PreConsumedQuota: 100,
		UserID:           975,
	})
	require.NoError(t, err)
	updated, err := model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationRefundPending, "request failed", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, updated)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Deferred)
	assert.Zero(t, summary.Refunded)

	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefundPending, current.Status)
}

func TestBillingOperationRunnerRefundsStatsUsingActualQuotaWhenStatsQuotaMissing(t *testing.T) {
	truncate(t)

	const (
		userID    = 976
		channelID = 976
		quota     = 300
	)
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("used_quota", quota).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).Update("used_quota", quota).Error)

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:billing-runner-stats-fallback",
		RequestID:        "billing-runner-stats-fallback",
		UserID:           userID,
		ChannelID:        channelID,
		PreConsumedQuota: quota,
		ActualQuota:      quota,
		ActualQuotaSet:   true,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.BillingOperation{}).
		Where("operation_key = ?", operation.OperationKey).
		Updates(map[string]any{"stats_applied": true, "stats_quota": 0, "stats_quota_set": false}).Error)
	updated, err := model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationRefundPending, "request failed", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, updated)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Refunded)
	usedQuota, _ := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, current.Status)
	assert.True(t, current.RefundStatsApplied)
}

func TestBillingOperationRunnerRefundsAppliedTokenWithoutReservation(t *testing.T) {
	truncate(t)

	const (
		userID    = 977
		tokenID   = 977
		channelID = 977
		actual    = 200
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-billing-runner-post-charge", 5_000)
	seedChannel(t, channelID)

	// A zero-precharge operation can still acquire actual usage during final
	// settlement (for example when the initial estimate was zero). In that case
	// TokenApplied is true while TokenReserved is false, and a later refund must
	// reverse the applied charge rather than treating it as a no-op.
	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:billing-runner-post-charge",
		RequestID:        "billing-runner-post-charge",
		UserID:           userID,
		TokenID:          tokenID,
		ChannelID:        channelID,
		PreConsumedQuota: 0,
	})
	require.NoError(t, err)
	require.NoError(t, model.ApplyBillingOperationTokenAdjustment(
		operation.OperationKey, tokenID, "sk-billing-runner-post-charge", actual, false,
	))
	require.NoError(t, model.ApplyBillingOperationStats(
		operation.OperationKey, userID, channelID, actual, true,
	))
	require.NoError(t, model.MarkBillingOperationComponent(operation.OperationKey, model.BillingComponentLog))
	updated, err := model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationSettled, "", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, updated)
	updated, err = model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationSettled},
		model.BillingOperationRefundPending, "request failed", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, updated)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Refunded)
	assert.Equal(t, 5_000, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, current.Status)
	assert.True(t, current.RefundTokenApplied)
}

func TestBillingOperationRunnerDefersPositiveTokenRefundWithoutTokenID(t *testing.T) {
	truncate(t)

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:billing-runner-token-refund-missing-id",
		RequestID:        "billing-runner-token-refund-missing-id",
		PreConsumedQuota: 200,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.BillingOperation{}).
		Where("operation_key = ?", operation.OperationKey).
		Updates(map[string]any{"token_reserved": true, "token_reserved_quota": 200}).Error)
	updated, err := model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationRefundPending, "request failed", common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, updated)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Deferred)
	assert.Zero(t, summary.Refunded)

	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefundPending, current.Status)
	assert.False(t, current.RefundTokenApplied)
}

func TestFinalizeTaskBillingOperationOwnedRequiresLease(t *testing.T) {
	truncate(t)

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "task:owned-finalize",
		TaskID:           "owned-finalize",
		PreConsumedQuota: 100,
		ActualQuota:      100,
	})
	require.NoError(t, err)
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(operation.OperationKey, component))
	}

	now := common.GetTimestamp()
	claimed, err := model.ClaimBillingOperations("worker-a", now, 60, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	settled, err := finalizeTaskBillingOperationOwned(operation.OperationKey, "worker-b", 200)
	require.NoError(t, err)
	assert.False(t, settled)

	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationApplying, current.Status)
	assert.Equal(t, 100, current.ActualQuota)

	settled, err = finalizeTaskBillingOperationOwned(operation.OperationKey, "worker-a", 200)
	require.NoError(t, err)
	assert.True(t, settled)

	current, err = model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationSettled, current.Status)
	assert.Equal(t, 200, current.ActualQuota)
}

func TestBillingOperationRunnerPreservesExplicitZeroActualQuota(t *testing.T) {
	truncate(t)
	const (
		userID    = 973
		tokenID   = 973
		channelID = 973
		taskID    = "billing-runner-zero-actual"
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-runner-zero-actual", 5_000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 300, tokenID, BillingSourceUsage)
	task.TaskID = taskID
	task.Status = model.TaskStatusSuccess
	task.SubmitTime = time.Now().Unix()
	operation := ensureTaskBillingFixture(t, task)
	require.NoError(t, model.DB.Create(task).Error)
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(operation.OperationKey, component))
	}
	require.NoError(t, model.UpdateBillingOperationActualQuota(operation.OperationKey, 0))

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Settled)
	updated, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.True(t, updated.ActualQuotaSet)
	assert.Zero(t, updated.ActualQuota)
	assert.Equal(t, model.BillingOperationSettled, updated.Status)
}

func TestBillingOperationRunnerSettlesSuccessfulTaskWithOwnedWorker(t *testing.T) {
	truncate(t)

	const (
		userID    = 962
		tokenID   = 962
		channelID = 962
		taskID    = "billing-runner-owned-success"
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-billing-runner-owned-success", 5_000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 300, tokenID, BillingSourceUsage)
	task.TaskID = taskID
	task.Status = model.TaskStatusSuccess
	task.SubmitTime = time.Now().Unix()
	operation := ensureTaskBillingFixture(t, task)
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.UpdateBillingOperationActualQuota(operation.OperationKey, task.Quota))
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(operation.OperationKey, component))
	}

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Settled)

	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationSettled, current.Status)
	assert.Equal(t, 300, current.ActualQuota)
}

func TestBillingOperationRunnerWaitsForFinalTaskUsage(t *testing.T) {
	truncate(t)

	const (
		userID     = 975
		tokenID    = 975
		channelID  = 975
		taskID     = "billing-runner-waits-for-final-usage"
		preQuota   = 300
		finalQuota = 450
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-runner-final-usage", 5_000-preQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preQuota, tokenID, BillingSourceUsage)
	task.TaskID = taskID
	task.SubmitTime = time.Now().Unix()
	operation := ensureTaskBillingFixture(t, task)
	require.NoError(t, model.DB.Create(task).Error)
	require.False(t, operation.FinalUsageApplied)
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationComponent(operation.OperationKey, component))
	}
	task.Status = model.TaskStatusSuccess
	won, err := task.UpdateWithStatus(model.TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Zero(t, summary.Settled)
	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationApplying, current.Status)

	require.NoError(t, model.MarkBillingOperationFinalUsage(operation.OperationKey, finalQuota))
	require.NoError(t, model.DB.Model(&model.BillingOperation{}).
		Where("operation_key = ?", operation.OperationKey).
		Update("next_retry_at", common.GetTimestamp()).Error)
	summary = RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Settled)
	current, err = model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationSettled, current.Status)
	assert.Equal(t, finalQuota, current.ActualQuota)
}

func TestBillingOperationRunnerSettlesPendingTaskAfterSubmissionSettlementFailure(t *testing.T) {
	truncate(t)

	const (
		userID    = 974
		tokenID   = 974
		channelID = 974
		taskID    = "billing-runner-pending-submission"
		quota     = 300
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-runner-pending-submission", 5_000-quota)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).
		Update("used_quota", quota).Error)

	// This is the durable state left when the task row was inserted and the
	// synchronous BillingSession.Settle failed: the task is still pending, the
	// final amount is known, and no consume-log payload was persisted yet.
	task := makeTask(userID, channelID, quota, tokenID, BillingSourceUsage)
	task.TaskID = taskID
	task.Status = model.TaskStatusInProgress
	task.SubmitTime = time.Now().Unix()
	task.PrivateData.BillingOperationKey = "request:pending-submission"
	operation := ensureTaskBillingFixture(t, task)
	require.NoError(t, model.DB.Create(task).Error)
	require.Equal(t, model.BillingOperationReserved, operation.Status)
	require.True(t, operation.ActualQuotaSet)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Zero(t, summary.Settled)
	assert.Zero(t, summary.Refunded)

	updated, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationApplying, updated.Status)
	assert.True(t, updated.TokenApplied)
	assert.True(t, updated.StatsApplied)
	assert.True(t, updated.LogApplied)
	assert.True(t, updated.LogPayloadSet)
	assert.Equal(t, quota, updated.StatsQuota)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, quota, user.UsedQuota)
	assert.Equal(t, int64(quota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, 5_000-quota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, quota, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, quota, getTaskQuota(t, task.ID))

	// Submit-time accounting is intentionally non-terminal for an async task.
	// A later provider usage observation must still apply only the final delta
	// and close the operation exactly once.
	task.Status = model.TaskStatusSuccess
	won, err := task.UpdateWithStatus(model.TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)
	RecalculateTaskQuota(context.Background(), task, quota+150, "provider final usage")
	updated, err = model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationSettled, updated.Status)
	assert.Equal(t, 5_000-quota-150, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, quota+150, getTokenUsedQuota(t, tokenID))
	var finalUser model.User
	require.NoError(t, model.DB.First(&finalUser, userID).Error)
	assert.Equal(t, quota+150, finalUser.UsedQuota)
	assert.Equal(t, int64(quota+150), getChannelUsedQuota(t, channelID))

	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).
		Where("billing_operation_key = ?", operation.OperationKey).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount)
}

func TestBillingOperationRunnerRestoresOperationKeyWhenReplayingLog(t *testing.T) {
	truncate(t)

	const (
		userID    = 963
		channelID = 963
		quota     = 250
	)
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)
	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:runner-log-replay",
		RequestID:        "runner-log-replay",
		UserID:           userID,
		ChannelID:        channelID,
		PreConsumedQuota: 0,
		ActualQuota:      quota,
		ActualQuotaSet:   true,
	})
	require.NoError(t, err)
	params := model.RecordConsumeLogParams{
		ChannelId: channelID,
		ModelName: "runner-log-model",
		Quota:     quota,
		Content:   "replayed",
		Group:     "default",
	}
	require.NoError(t, model.SetBillingOperationLogPayload(operation.OperationKey, params))

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Settled)

	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).
		Where("billing_operation_key = ?", operation.OperationKey).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("billing_operation_key = ?", operation.OperationKey).First(&log).Error)
	require.NotNil(t, log.BillingOperationKey)
	assert.Equal(t, operation.OperationKey, *log.BillingOperationKey)

	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.True(t, current.LogApplied)
	assert.True(t, current.StatsApplied)
	assert.Equal(t, model.BillingOperationSettled, current.Status)
}

func TestBillingOperationRunnerRefundsStaleUnfinishedRequest(t *testing.T) {
	truncate(t)

	const (
		userID    = 964
		tokenID   = 964
		channelID = 964
		reserved  = 400
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-stale-request", 5_000-reserved)
	seedChannel(t, channelID)
	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:stale-unfinished",
		RequestID:        "stale-unfinished",
		UserID:           userID,
		TokenID:          tokenID,
		ChannelID:        channelID,
		PreConsumedQuota: reserved,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.BillingOperation{}).
		Where("operation_key = ?", operation.OperationKey).
		Updates(map[string]any{"token_reserved": true, "token_reserved_quota": reserved, "created_at": common.GetTimestamp() - billingOperationRequestRecoveryTimeoutSeconds - 1}).Error)

	summary := RunBillingOperationReconciliationOnce(context.Background())
	assert.Equal(t, 1, summary.Claimed)
	assert.Equal(t, 1, summary.Refunded)
	assert.Equal(t, 5_000, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, current.Status)
}
