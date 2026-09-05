package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMidjourneySessionConsumptionAndRefundUseDurableOperation(t *testing.T) {
	truncate(t)
	ctx := newPersonalBillingTestContext()
	ctx.Set(common.RequestIdKey, "midjourney-durable-request")

	const (
		userID       = 992
		tokenID      = 992
		channelID    = 992
		chargedQuota = 3000
	)
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-midjourney-session", 5000)
	seedChannel(t, channelID)
	info := &relaycommon.RelayInfo{
		UserId:      userID,
		TokenId:     tokenID,
		TokenKey:    "sk-midjourney-session",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	session, apiErr := NewBillingSession(ctx, info, chargedQuota)
	require.Nil(t, apiErr)
	info.Billing = session

	task := &model.Midjourney{
		UserId:    userID,
		MjId:      "mj-session-durable",
		Action:    "IMAGINE",
		ChannelId: channelID,
		Progress:  "0%",
	}
	prepared, err := PrepareMidjourneyTaskBilling(info, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)
	require.Equal(t, session.OperationKey(), task.BillingOperationKey)
	require.NoError(t, task.Insert())
	billed, err := SettleMidjourneyTaskBilling(info, task, prepared)
	require.NoError(t, err)
	require.True(t, billed)

	require.NoError(t, RecordMidjourneyTaskConsumption(ctx, info, task, "mj_imagine", "test-token", "consume", "default", map[string]interface{}{}))
	operation, err := model.GetBillingOperation(task.BillingOperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationSettled, operation.Status)
	assert.True(t, operation.TokenApplied)
	assert.True(t, operation.StatsApplied)
	assert.True(t, operation.LogApplied)
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, chargedQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, 2000, getTokenRemainQuota(t, tokenID))

	assert.True(t, RefundMidjourneyQuota(ctx, task, "upstream failure"))
	assert.True(t, RefundMidjourneyQuota(ctx, task, "duplicate poll"))
	assert.Zero(t, task.Quota)
	assert.Equal(t, 5000, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount = getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(2), countLogs(t))
}

func TestRefundMidjourneyQuotaRequiresExistingOperation(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const (
		userID       = 990
		tokenID      = 990
		channelID    = 990
		chargedQuota = 3000
	)
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-midjourney-durable-refund", 2000)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, chargedQuota, 1)

	task := &model.Midjourney{
		UserId:    userID,
		MjId:      "mj-durable-refund",
		Action:    "IMAGINE",
		ChannelId: channelID,
		Quota:     chargedQuota,
		TokenId:   tokenID,
		Progress:  "0%",
	}
	require.NoError(t, task.Insert())

	assert.False(t, RefundMidjourneyQuota(ctx, task, "upstream failure"))
	assert.Empty(t, task.BillingOperationKey)
	usedQuota, _ := getUserUsageAccounting(t, userID)
	assert.Equal(t, chargedQuota, usedQuota)
	assert.Equal(t, int64(chargedQuota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, 2000, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, chargedQuota, getTokenUsedQuota(t, tokenID))
	assert.Zero(t, countLogs(t))
	var operationCount int64
	require.NoError(t, model.DB.Model(&model.BillingOperation{}).Count(&operationCount).Error)
	assert.Zero(t, operationCount)
}
