package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyBillingOperationStatsConcurrentIsIdempotent(t *testing.T) {
	if err := ensureBillingOperationTable(); err != nil {
		t.Fatal(err)
	}
	user := &User{Username: "billing_stats_concurrent"}
	channel := &Channel{Key: "billing_stats_concurrent", Name: "billing stats concurrent"}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(channel).Error)
	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey: "request:stats-concurrent",
		UserID:       user.Id,
		ChannelID:    channel.Id,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = DB.Delete(&BillingOperation{}, "operation_key = ?", operation.OperationKey).Error
		_ = DB.Delete(&Channel{}, channel.Id).Error
		_ = DB.Delete(&User{}, user.Id).Error
	})

	const workers = 8
	errs := make(chan error, workers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer waitGroup.Done()
			errs <- ApplyBillingOperationStats(operation.OperationKey, user.Id, channel.Id, 125, true)
		}()
	}
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var gotUser User
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	require.Equal(t, 125, gotUser.UsedQuota)
	require.Equal(t, 1, gotUser.RequestCount)
	var gotChannel Channel
	require.NoError(t, DB.First(&gotChannel, channel.Id).Error)
	require.Equal(t, int64(125), gotChannel.UsedQuota)
	gotOperation, err := GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	require.True(t, gotOperation.StatsApplied)
	require.Equal(t, 125, gotOperation.StatsQuota)
}

func TestUpdateBillingOperationChannelIDAllowsRetryBeforeAccounting(t *testing.T) {
	if err := ensureBillingOperationTable(); err != nil {
		t.Fatal(err)
	}
	const operationKey = "request:channel-retry-binding"
	require.NoError(t, DB.Where("operation_key = ?", operationKey).Delete(&BillingOperation{}).Error)
	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey: operationKey,
		RequestID:    "channel-retry-binding",
		ChannelID:    101,
		Status:       BillingOperationApplying,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = DB.Delete(&BillingOperation{}, "operation_key = ?", operationKey).Error
	})

	// A channel failure before usage accounting must not prevent the request
	// retry from binding the operation to the replacement channel.
	require.NoError(t, UpdateBillingOperationChannelID(operation.OperationKey, 202))
	updated, err := GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, 202, updated.ChannelID)

	// Once stats are durable, a channel switch would make the aggregates and
	// operation row disagree, so preserve the original binding.
	require.NoError(t, DB.Model(&BillingOperation{}).Where("operation_key = ?", operationKey).
		Updates(map[string]any{"stats_applied": true}).Error)
	assert.Error(t, UpdateBillingOperationChannelID(operationKey, 303))
}

func TestApplyBillingOperationTokenAdjustmentUsesLastAppliedQuota(t *testing.T) {
	if err := ensureBillingOperationTable(); err != nil {
		t.Fatal(err)
	}
	const operationKey = "request:token-adjustment-last-applied"
	require.NoError(t, DB.Where("operation_key = ?", operationKey).Delete(&BillingOperation{}).Error)
	token := &Token{
		UserId:      1,
		Key:         "billing-token-adjustment-last-applied",
		Name:        "billing token adjustment",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 900,
		UsedQuota:   100,
	}
	require.NoError(t, DB.Create(token).Error)
	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey:     operationKey,
		TokenID:          token.Id,
		PreConsumedQuota: 100,
		Status:           BillingOperationApplying,
	})
	require.NoError(t, err)
	require.NoError(t, DB.Model(&BillingOperation{}).Where("operation_key = ?", operationKey).Updates(map[string]any{
		"token_reserved":       true,
		"token_reserved_quota": 100,
		"token_applied":        true,
		"actual_quota":         100,
		"actual_quota_set":     true,
	}).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&BillingOperation{}, "operation_key = ?", operationKey).Error
		_ = DB.Delete(&Token{}, token.Id).Error
	})

	// The first post-submit observation charges only the increase from the
	// already applied 100 quota, not another 160 on top of the reservation.
	require.NoError(t, ApplyBillingOperationTokenAdjustment(operation.OperationKey, token.Id, token.Key, 160, false))
	var gotToken Token
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	assert.Equal(t, 840, gotToken.RemainQuota)
	assert.Equal(t, 160, gotToken.UsedQuota)
	gotOperation, err := GetBillingOperation(operationKey)
	require.NoError(t, err)
	assert.Equal(t, 160, gotOperation.ActualQuota)
	assert.Equal(t, 60, gotOperation.TokenDelta)

	// Replaying the same target is a no-op.
	require.NoError(t, ApplyBillingOperationTokenAdjustment(operationKey, token.Id, token.Key, 160, false))
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	assert.Equal(t, 840, gotToken.RemainQuota)
	assert.Equal(t, 160, gotToken.UsedQuota)

	// A later lower target refunds only the difference from the latest target.
	require.NoError(t, ApplyBillingOperationTokenAdjustment(operationKey, token.Id, token.Key, 80, false))
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	assert.Equal(t, 920, gotToken.RemainQuota)
	assert.Equal(t, 80, gotToken.UsedQuota)
	gotOperation, err = GetBillingOperation(operationKey)
	require.NoError(t, err)
	assert.Equal(t, 80, gotOperation.ActualQuota)
	assert.Equal(t, -20, gotOperation.TokenDelta)
}

func TestApplyBillingOperationStatsAdjustmentConcurrentTargetsFinalQuota(t *testing.T) {
	if err := ensureBillingOperationTable(); err != nil {
		t.Fatal(err)
	}
	user := &User{Username: "billing_stats_adjustment", UsedQuota: 100}
	channel := &Channel{Key: "billing_stats_adjustment", Name: "billing stats adjustment", UsedQuota: 100}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(channel).Error)
	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey: "request:stats-adjustment",
		UserID:       user.Id,
		ChannelID:    channel.Id,
	})
	require.NoError(t, err)
	require.NoError(t, DB.Model(&BillingOperation{}).Where("operation_key = ?", operation.OperationKey).
		Updates(map[string]any{"stats_applied": true, "stats_quota": 100, "stats_quota_set": true}).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&BillingOperation{}, "operation_key = ?", operation.OperationKey).Error
		_ = DB.Delete(&Channel{}, channel.Id).Error
		_ = DB.Delete(&User{}, user.Id).Error
	})

	targets := []int{75, 250, 175, 125}
	errs := make(chan error, len(targets))
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(targets))
	for _, target := range targets {
		go func(target int) {
			defer waitGroup.Done()
			errs <- ApplyBillingOperationStatsAdjustment(operation.OperationKey, user.Id, channel.Id, target)
		}(target)
	}
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	gotOperation, err := GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	require.Contains(t, targets, gotOperation.StatsQuota)
	var gotUser User
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	var gotChannel Channel
	require.NoError(t, DB.First(&gotChannel, channel.Id).Error)
	require.Equal(t, gotOperation.StatsQuota, gotUser.UsedQuota)
	require.Equal(t, int64(gotOperation.StatsQuota), gotChannel.UsedQuota)
}

func TestApplyBillingOperationRefundStatsDoesNotInferLegacyFromEmptyRequestID(t *testing.T) {
	if err := ensureBillingOperationTable(); err != nil {
		t.Fatal(err)
	}
	user := &User{Username: "billing_stats_empty_request"}
	channel := &Channel{Key: "billing_stats_empty_request", Name: "billing stats empty request"}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(channel).Error)
	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey:     "request:stats-empty-request-id",
		UserID:           user.Id,
		ChannelID:        channel.Id,
		PreConsumedQuota: 100,
	})
	require.NoError(t, err)
	require.NoError(t, DB.Model(&BillingOperation{}).
		Where("operation_key = ?", operation.OperationKey).
		Updates(map[string]any{"status": BillingOperationRefundPending}).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&BillingOperation{}, "operation_key = ?", operation.OperationKey).Error
		_ = DB.Delete(&Channel{}, channel.Id).Error
		_ = DB.Delete(&User{}, user.Id).Error
	})

	require.NoError(t, ApplyBillingOperationRefundStats(operation.OperationKey, user.Id, channel.Id, 100))
	var gotUser User
	var gotChannel Channel
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	require.NoError(t, DB.First(&gotChannel, channel.Id).Error)
	require.Zero(t, gotUser.UsedQuota)
	require.Zero(t, gotChannel.UsedQuota)
	updated, err := GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	require.True(t, updated.RefundStatsApplied)
}
