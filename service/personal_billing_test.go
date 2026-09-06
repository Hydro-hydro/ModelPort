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

func TestPersonalBillingSessionUsesTokenQuota(t *testing.T) {
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
	assert.Equal(t, BillingSourceUsage, relayInfo.BillingSource)
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))

	require.NoError(t, session.Settle(preConsumedQuota))
}

func TestFreeModelUsageSessionIsCreatedOnlyForPositiveSurcharge(t *testing.T) {
	truncate(t)

	const userID, tokenID = 807, 807
	const initialTokenQuota = 1_000
	const tokenKey = "sk-personal-free-model-surcharge"
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	relayInfo.PriceData.FreeModel = true
	ctx := newPersonalBillingTestContext()

	// A genuinely free response does not need a durable billing operation.
	require.NoError(t, ensureBillingSessionForUsage(ctx, relayInfo, 0))
	assert.Nil(t, relayInfo.Billing)

	// A positive tool/audio surcharge must enter the same usage-only settlement
	// path as a normally pre-consumed request, starting from zero reservation.
	require.NoError(t, ensureBillingSessionForUsage(ctx, relayInfo, 1))
	require.NotNil(t, relayInfo.Billing)
	require.NoError(t, relayInfo.Billing.Settle(1))

	assert.Equal(t, initialTokenQuota-1, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 1, getTokenUsedQuota(t, tokenID))
	operation, err := model.GetBillingOperation(relayInfo.Billing.(*BillingSession).OperationKey())
	require.NoError(t, err)
	assert.True(t, operation.TokenApplied)
}

func TestPersonalBillingSessionCanReserveAfterZeroInitialEstimate(t *testing.T) {
	truncate(t)

	const userID, tokenID = 806, 806
	const initialTokenQuota, reservedQuota = 1_000, 200
	const tokenKey = "sk-personal-zero-estimate-reserve"
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session, apiErr := NewBillingSession(newPersonalBillingTestContext(), relayInfo, 0)
	require.Nil(t, apiErr)
	require.NoError(t, session.Reserve(reservedQuota))

	assert.Equal(t, initialTokenQuota-reservedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, reservedQuota, getTokenUsedQuota(t, tokenID))
	operation, err := model.GetBillingOperation(session.OperationKey())
	require.NoError(t, err)
	assert.True(t, operation.TokenReserved)
	assert.False(t, operation.TokenReservationSkipped)
	assert.Equal(t, reservedQuota, operation.TokenReservedQuota)
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
		var token model.Token
		if err := model.DB.Select("remain_quota", "used_quota").First(&token, tokenID).Error; err != nil {
			return false
		}
		return token.RemainQuota == initialTokenQuota &&
			token.UsedQuota == 0
	}, time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool { return !session.NeedsRefund() }, time.Second, 10*time.Millisecond)
}

func TestPersonalBillingSessionIgnoresUserBalance(t *testing.T) {
	truncate(t)

	const userID, tokenID = 806, 806
	const initialQuota, initialTokenQuota, preConsumedQuota, actualQuota = 0, 1_000, 200, 300
	const tokenKey = "sk-personal-zero-user-balance"

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
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))

	require.NoError(t, session.Settle(actualQuota))
	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
}

func TestPersonalBillingSessionChargesActualUsageAfterZeroPreConsume(t *testing.T) {
	truncate(t)

	const userID, tokenID = 809, 809
	const initialTokenQuota, actualQuota = 1_000, 300
	const tokenKey = "sk-personal-zero-preconsume"

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)

	session, apiErr := NewBillingSession(newPersonalBillingTestContext(), relayInfo, 0)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.NoError(t, session.Settle(actualQuota))

	assert.Equal(t, initialTokenQuota-actualQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
}

func TestPersonalBillingSessionRejectsTokenQuotaWithoutUserBalance(t *testing.T) {
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
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
}

func TestPersonalBillingSessionRollsBackTokenWhenReservationCannotPersist(t *testing.T) {
	truncate(t)

	const userID, tokenID = 809, 809
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-personal-reservation-persist-failure"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	require.NoError(t, model.DB.Exec(`
		CREATE TRIGGER fail_billing_operation_reservation
		BEFORE UPDATE OF pre_consumed_quota ON billing_operations
		BEGIN
			SELECT RAISE(ABORT, 'forced billing reservation persistence failure');
		END;
	`).Error)
	t.Cleanup(func() { model.DB.Exec("DROP TRIGGER IF EXISTS fail_billing_operation_reservation") })

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session, apiErr := NewBillingSession(newPersonalBillingTestContext(), relayInfo, preConsumedQuota)
	require.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
}
