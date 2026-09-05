package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreConsumeFailureClosesDurableBillingOperation(t *testing.T) {
	truncate(t)
	const (
		userID    = 974
		tokenID   = 974
		requestID = "preconsume-token-insufficient"
	)
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, "sk-preconsume-insufficient", 0)
	info := personalBillingRelayInfo(userID, tokenID, "sk-preconsume-insufficient")
	ctx := newPersonalBillingTestContext()
	ctx.Set(common.RequestIdKey, requestID)

	session, apiErr := NewBillingSession(ctx, info, 300)
	require.Nil(t, session)
	require.NotNil(t, apiErr)

	operation, err := model.GetBillingOperation(model.BillingOperationKeyForRequest(requestID))
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, operation.Status)
	assert.True(t, operation.RefundTokenApplied)
	assert.True(t, operation.RefundStatsApplied)
	assert.True(t, operation.RefundLogApplied)
	assert.Equal(t, 0, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
}
