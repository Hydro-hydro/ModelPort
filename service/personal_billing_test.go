package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPersonalBillingTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return context
}

func personalBillingRelayInfo(userID, tokenID int, tokenKey string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        tokenKey,
		OriginModelName: "test-model",
	}
}

func TestPersonalBillingSessionUsesUsageFunding(t *testing.T) {
	truncate(t)

	const userID, tokenID = 801, 801
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-usage"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, BillingSourceUsage, session.funding.Source())
	assert.Equal(t, BillingSourceUsage, relayInfo.BillingSource)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))

	require.NoError(t, session.Settle(preConsumedQuota))
}

func TestPersonalBillingSessionSettlesUsageAndTokenDelta(t *testing.T) {
	truncate(t)

	const userID, tokenID = 802, 802
	const initialQuota, initialTokenQuota, preConsumedQuota, actualQuota = 1_000, 1_000, 200, 300
	const tokenKey = "sk-personal-settle"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	require.NoError(t, session.Settle(actualQuota))
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	assert.False(t, session.NeedsRefund())
}

func TestPersonalBillingSessionReturnsUsageAndTokenDelta(t *testing.T) {
	truncate(t)

	const userID, tokenID = 805, 805
	const initialQuota, initialTokenQuota, preConsumedQuota, actualQuota = 1_000, 1_000, 200, 100
	const tokenKey = "sk-personal-settle-refund"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	require.NoError(t, session.Settle(actualQuota))
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	assert.False(t, session.NeedsRefund())
}

func TestPersonalBillingSessionRefundsUsageAndTokenOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID = 803, 803
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-refund"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.True(t, session.NeedsRefund())

	context := newPersonalBillingTestContext()
	session.Refund(context)
	session.Refund(context)

	require.Eventually(t, func() bool {
		var user model.User
		var token model.Token
		if err := model.DB.Select("quota").First(&user, userID).Error; err != nil {
			return false
		}
		if err := model.DB.Select("remain_quota", "used_quota").First(&token, tokenID).Error; err != nil {
			return false
		}
		return user.Quota == initialQuota &&
			token.RemainQuota == initialTokenQuota &&
			token.UsedQuota == 0
	}, time.Second, 10*time.Millisecond)

	assert.False(t, session.NeedsRefund())
}

func TestPersonalBillingSessionAllowsInsufficientWalletBalance(t *testing.T) {
	truncate(t)

	const userID, tokenID = 806, 806
	const initialQuota, initialTokenQuota, preConsumedQuota, actualQuota = 0, 1_000, 200, 300
	const tokenKey = "sk-personal-zero-wallet"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceUsage, relayInfo.BillingSource)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))

	require.NoError(t, session.Settle(actualQuota))
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
}

func TestPostConsumeQuotaDefaultsToUsageWithoutWalletMutation(t *testing.T) {
	truncate(t)

	const userID, tokenID = 807, 807
	const initialQuota, initialTokenQuota, consumedQuota = 0, 1_000, 200
	const tokenKey = "sk-personal-default-usage"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	// An unmarked RelayInfo is the shape used by older direct callers. It must
	// follow the personal usage-only default rather than falling back to a
	// wallet deduction.
	relayInfo := &relaycommon.RelayInfo{
		UserId:   userID,
		TokenId:  tokenID,
		TokenKey: tokenKey,
	}
	require.NoError(t, PostConsumeQuota(relayInfo, consumedQuota, 0, false))
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-consumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, consumedQuota, getTokenUsedQuota(t, tokenID))
}

func TestPersonalBillingSessionRejectsTokenQuotaWithoutWalletDeduction(t *testing.T) {
	truncate(t)

	const userID, tokenID = 804, 804
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 100, 200
	const tokenKey = "sk-personal-token-limit"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)

	require.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodePreConsumeTokenQuotaFailed, apiErr.GetErrorCode())
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
}
