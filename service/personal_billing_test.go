package service

import (
	"errors"
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

type failingPersonalFunding struct{}

func (*failingPersonalFunding) Source() string       { return BillingSourceUsage }
func (*failingPersonalFunding) PreConsume(int) error { return errors.New("forced funding failure") }
func (*failingPersonalFunding) Settle(int) error     { return nil }
func (*failingPersonalFunding) Refund() error        { return nil }

func TestPersonalBillingPreConsumeFailureDoesNotDoubleRefundToken(t *testing.T) {
	truncate(t)

	const userID, tokenID = 810, 810
	const initialTokenQuota, preConsumedQuota = 1_000, 200
	const tokenKey = "sk-personal-funding-failure"
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     "request:personal-funding-failure",
		RequestID:        "personal-funding-failure",
		UserID:           userID,
		TokenID:          tokenID,
		FundingSource:    BillingSourceUsage,
		PreConsumedQuota: 0,
	})
	require.NoError(t, err)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	session := &BillingSession{
		relayInfo:    relayInfo,
		funding:      &failingPersonalFunding{},
		operationKey: operation.OperationKey,
	}
	apiErr := session.preConsume(newPersonalBillingTestContext(), preConsumedQuota)
	require.NotNil(t, apiErr)
	session.abortPreConsume(apiErr)

	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	updated, err := model.GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, model.BillingOperationRefunded, updated.Status)
	assert.True(t, updated.RefundTokenApplied)
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

	require.Eventually(t, func() bool { return !session.NeedsRefund() }, time.Second, 10*time.Millisecond)
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

func TestPersonalBillingSessionChargesActualUsageAfterZeroPreConsume(t *testing.T) {
	truncate(t)

	const userID, tokenID = 809, 809
	const initialWallet, initialTokenQuota, actualQuota = 0, 1_000, 300
	const tokenKey = "sk-personal-zero-preconsume"

	seedUser(t, userID, initialWallet)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)

	session, apiErr := NewBillingSession(newPersonalBillingTestContext(), relayInfo, 0)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.NoError(t, session.Settle(actualQuota))

	assert.Equal(t, initialWallet, getUserQuota(t, userID))
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
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
}
