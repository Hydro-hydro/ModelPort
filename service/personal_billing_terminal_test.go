package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersonalBillingTerminalRefundsPreConsumedQuotaExactlyOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID = 901, 901
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-terminal-refund"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
	require.NotNil(t, relayInfo.Billing)
	var accounting relaycommon.UsageAccounting = relayInfo.Billing
	assert.Equal(t, initialQuota-preConsumedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))

	context := newPersonalBillingTestContext()
	accounting.Refund(context)
	accounting.Refund(context)

	require.Eventually(t, func() bool {
		var user model.User
		var token model.Token
		if err := model.DB.Select("quota").Where("id = ?", userID).First(&user).Error; err != nil {
			return false
		}
		if err := model.DB.Select("remain_quota", "used_quota").Where("id = ?", tokenID).First(&token).Error; err != nil {
			return false
		}
		return user.Quota == initialQuota &&
			token.RemainQuota == initialTokenQuota &&
			token.UsedQuota == 0
	}, time.Second, 10*time.Millisecond)

	assert.False(t, relayInfo.Billing.NeedsRefund())
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
}

func TestPersonalBillingTerminalSettlementIsIdempotentAndKeepsQuotasInSync(t *testing.T) {
	tests := []struct {
		name        string
		userID      int
		tokenID     int
		actualQuota int
	}{
		{name: "actual usage below reservation", userID: 902, tokenID: 902, actualQuota: 100},
		{name: "actual usage equals reservation", userID: 903, tokenID: 903, actualQuota: 200},
		{name: "actual usage above reservation", userID: 904, tokenID: 904, actualQuota: 300},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)

			const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
			tokenKey := "sk-personal-terminal-settle-" + test.name

			seedUser(t, test.userID, initialQuota)
			seedToken(t, test.tokenID, test.userID, tokenKey, initialTokenQuota)

			relayInfo := personalBillingRelayInfo(test.userID, test.tokenID, tokenKey)
			require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
			require.NotNil(t, relayInfo.Billing)
			var accounting relaycommon.UsageAccounting = relayInfo.Billing

			require.NoError(t, accounting.Settle(test.actualQuota))
			assert.Equal(t, initialQuota-test.actualQuota, getUserQuota(t, test.userID))
			assert.Equal(t, initialTokenQuota-test.actualQuota, getTokenRemainQuota(t, test.tokenID))
			assert.Equal(t, test.actualQuota, getTokenUsedQuota(t, test.tokenID))

			// A settled request is terminal; a later, different actual quota must not
			// apply another adjustment.
			require.NoError(t, accounting.Settle(test.actualQuota+50))
			assert.Equal(t, initialQuota-test.actualQuota, getUserQuota(t, test.userID))
			assert.Equal(t, initialTokenQuota-test.actualQuota, getTokenRemainQuota(t, test.tokenID))
			assert.Equal(t, test.actualQuota, getTokenUsedQuota(t, test.tokenID))
			assert.False(t, relayInfo.Billing.NeedsRefund())
		})
	}
}

func TestPersonalBillingTerminalZeroUsageSettlesOnlyOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID = 905, 905
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-terminal-zero-usage"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
	require.NotNil(t, relayInfo.Billing)
	var accounting relaycommon.UsageAccounting = relayInfo.Billing

	// 当前服务约定中，未取得最终 Usage 时以 actualQuota=0 结算。
	require.NoError(t, accounting.Settle(0))
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))

	// 第二次传入不同的终态也不能再次调整已经结算的请求。
	require.NoError(t, accounting.Settle(300))
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	assert.False(t, relayInfo.Billing.NeedsRefund())
}

func TestPersonalBillingTerminalCommittedFundingIsNotRefunded(t *testing.T) {
	truncate(t)

	const userID, tokenID = 906, 906
	const initialQuota, initialTokenQuota, preConsumedQuota, actualQuota = 1_000, 1_000, 200, 300
	const tokenKey = "sk-personal-terminal-committed-funding"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
	require.NotNil(t, relayInfo.Billing)
	var accounting relaycommon.UsageAccounting = relayInfo.Billing
	require.NoError(t, accounting.Settle(actualQuota))

	assert.Equal(t, initialQuota-actualQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))

	// 资金来源已完成结算后，失败清理路径不能再退款预扣额度。
	context := newPersonalBillingTestContext()
	accounting.Refund(context)
	accounting.Refund(context)

	assert.False(t, relayInfo.Billing.NeedsRefund())
	assert.Equal(t, initialQuota-actualQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
}
