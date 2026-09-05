package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))

	context := newPersonalBillingTestContext()
	accounting.Refund(context)
	accounting.Refund(context)

	require.Eventually(t, func() bool {
		var token model.Token
		if err := model.DB.Select("remain_quota", "used_quota").Where("id = ?", tokenID).First(&token).Error; err != nil {
			return false
		}
		return token.RemainQuota == initialTokenQuota &&
			token.UsedQuota == 0
	}, time.Second, 10*time.Millisecond)

	assert.False(t, relayInfo.Billing.NeedsRefund())
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
			assert.Equal(t, initialTokenQuota-test.actualQuota, getTokenRemainQuota(t, test.tokenID))
			assert.Equal(t, test.actualQuota, getTokenUsedQuota(t, test.tokenID))

			// A settled request is terminal; a later, different actual quota must not
			// apply another adjustment.
			require.NoError(t, accounting.Settle(test.actualQuota+50))
			assert.Equal(t, initialTokenQuota-test.actualQuota, getTokenRemainQuota(t, test.tokenID))
			assert.Equal(t, test.actualQuota, getTokenUsedQuota(t, test.tokenID))
			operation, err := model.GetBillingOperation(accounting.(*BillingSession).OperationKey())
			require.NoError(t, err)
			assert.Equal(t, test.actualQuota, operation.ActualQuota,
				"a repeated Settle must keep the first durable actual quota")
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
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))

	// 第二次传入不同的终态也不能再次调整已经结算的请求。
	require.NoError(t, accounting.Settle(300))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	assert.False(t, relayInfo.Billing.NeedsRefund())
}

func TestPersonalBillingTerminalRejectsNegativeActualQuota(t *testing.T) {
	truncate(t)

	const userID, tokenID = 909, 909
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-terminal-negative-usage"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
	require.Error(t, relayInfo.Billing.Settle(-1))
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))

	// Rejecting invalid input must not poison the session's terminal state.
	require.NoError(t, relayInfo.Billing.Settle(0))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
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

	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))

	// 资金来源已完成结算后，失败清理路径不能再退款预扣额度。
	context := newPersonalBillingTestContext()
	accounting.Refund(context)
	accounting.Refund(context)

	assert.False(t, relayInfo.Billing.NeedsRefund())
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
}

func TestPersonalBillingTerminalRefundStopsWhenDurableSettlementWon(t *testing.T) {
	truncate(t)

	const userID, tokenID = 911, 911
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-terminal-refund-settled-race"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
	session := relayInfo.Billing.(*BillingSession)
	operationKey := session.OperationKey()
	updated, err := model.UpdateBillingOperationStatus(operationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationSettled, "", 0)
	require.NoError(t, err)
	require.True(t, updated)

	session.Refund(newPersonalBillingTestContext())
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))
	session.mu.Lock()
	assert.True(t, session.settled)
	assert.False(t, session.refundInFlight)
	session.mu.Unlock()
}

func TestPersonalBillingTerminalRefundHonorsDurableRefundMarkers(t *testing.T) {
	truncate(t)

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:durable-funding-refund-marker",
		RequestID:        "durable-funding-refund-marker",
		PreConsumedQuota: 100,
	})
	require.NoError(t, err)
	for _, component := range []string{model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		require.NoError(t, model.MarkBillingOperationRefundComponent(operation.OperationKey, component))
	}
	_, err = model.UpdateBillingOperationStatus(operation.OperationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved},
		model.BillingOperationRefundPending, "runner refund complete", common.GetTimestamp())
	require.NoError(t, err)

	session := &BillingSession{
		relayInfo:    personalBillingRelayInfo(912, 0, ""),
		operationKey: operation.OperationKey,
	}
	session.Refund(newPersonalBillingTestContext())
	require.Eventually(t, func() bool {
		session.mu.Lock()
		defer session.mu.Unlock()
		return !session.refundInFlight
	}, time.Second, 10*time.Millisecond)

	current, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, current.Status)
}

func TestPersonalBillingTerminalSettlementRetriesTokenFailure(t *testing.T) {
	truncate(t)

	const userID, tokenID = 907, 907
	const initialQuota, initialTokenQuota, preConsumedQuota, actualQuota = 1_000, 1_000, 200, 300
	const tokenKey = "sk-personal-terminal-settle-retry"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
	require.NoError(t, model.DB.Exec(`
		CREATE TRIGGER fail_token_settlement_once
		BEFORE UPDATE ON tokens
		WHEN OLD.id = 907
		BEGIN
			SELECT RAISE(ABORT, 'forced token settlement failure');
		END;
	`).Error)
	t.Cleanup(func() { model.DB.Exec("DROP TRIGGER IF EXISTS fail_token_settlement_once") })

	settlementErr := relayInfo.Billing.Settle(actualQuota)
	require.Error(t, settlementErr)
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))
	assert.False(t, relayInfo.Billing.(*BillingSession).settled)

	require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS fail_token_settlement_once").Error)
	require.NoError(t, relayInfo.Billing.Settle(actualQuota))
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	assert.False(t, relayInfo.Billing.NeedsRefund())

	// A successful session is terminal even if a later caller supplies a
	// different usage value.
	require.NoError(t, relayInfo.Billing.Settle(actualQuota+100))
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
}

func TestPersonalBillingTerminalRefundRetriesFailedTokenAndRemainsIdempotent(t *testing.T) {
	truncate(t)

	const userID, tokenID = 908, 908
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-terminal-refund-retry"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
	require.NoError(t, model.DB.Exec(`
		CREATE TRIGGER fail_token_refund_once
		BEFORE UPDATE ON tokens
		WHEN OLD.id = 908
		BEGIN
			SELECT RAISE(ABORT, 'forced token refund failure');
		END;
	`).Error)
	t.Cleanup(func() { model.DB.Exec("DROP TRIGGER IF EXISTS fail_token_refund_once") })

	relayInfo.Billing.Refund(newPersonalBillingTestContext())
	require.Eventually(t, func() bool {
		session := relayInfo.Billing.(*BillingSession)
		session.mu.Lock()
		defer session.mu.Unlock()
		return !session.refundInFlight
	}, time.Second, 10*time.Millisecond)
	assert.True(t, relayInfo.Billing.NeedsRefund())
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))

	require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS fail_token_refund_once").Error)
	relayInfo.Billing.Refund(newPersonalBillingTestContext())
	require.Eventually(t, func() bool {
		return !relayInfo.Billing.NeedsRefund()
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))

	// Duplicate cleanup calls must not add the reservation twice.
	relayInfo.Billing.Refund(newPersonalBillingTestContext())
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
}

func TestPersonalBillingTerminalRetriesRefundMarkerWithoutRepeatingTokenRefund(t *testing.T) {
	truncate(t)

	const userID, tokenID = 910, 910
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-terminal-refund-marker-retry"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	require.Nil(t, PreConsumeBilling(newPersonalBillingTestContext(), preConsumedQuota, relayInfo))
	require.NoError(t, model.DB.Exec(`
		CREATE TRIGGER fail_billing_refund_token_marker
		BEFORE UPDATE OF refund_token_applied ON billing_operations
		WHEN NEW.refund_token_applied = 1
		BEGIN
			SELECT RAISE(ABORT, 'forced refund marker failure');
		END;
	`).Error)
	t.Cleanup(func() { model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_refund_token_marker") })

	relayInfo.Billing.Refund(newPersonalBillingTestContext())
	require.Eventually(t, func() bool {
		session := relayInfo.Billing.(*BillingSession)
		session.mu.Lock()
		defer session.mu.Unlock()
		return !session.refundInFlight
	}, time.Second, 10*time.Millisecond)
	assert.True(t, relayInfo.Billing.NeedsRefund())
	// Token refund and its durable marker are one transaction. If the marker
	// write fails, the token update rolls back and the operation remains
	// retryable.
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))

	require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_refund_token_marker").Error)
	relayInfo.Billing.Refund(newPersonalBillingTestContext())
	require.Eventually(t, func() bool { return !relayInfo.Billing.NeedsRefund() }, time.Second, 10*time.Millisecond)
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
}
