package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setBillingFactoryTestMode(t *testing.T, selfUse, demo bool) {
	t.Helper()

	originalSelfUse := operation_setting.SelfUseModeEnabled
	originalDemo := operation_setting.DemoSiteEnabled
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUse
		operation_setting.DemoSiteEnabled = originalDemo
	})

	operation_setting.SelfUseModeEnabled = selfUse
	operation_setting.DemoSiteEnabled = demo
}

func seedBillingFactorySubscription(t *testing.T, userID, subscriptionID, planID int) {
	t.Helper()

	// The shared service TestMain migrates the subscription instance table for
	// existing billing tests, while this factory test also exercises the real
	// subscription pre-consume path that needs its plan and idempotency tables.
	require.NoError(t, model.DB.AutoMigrate(
		&model.SubscriptionPlan{},
		&model.SubscriptionPreConsumeRecord{},
	))

	plan := &model.SubscriptionPlan{
		Id:            planID,
		Title:         "billing factory test plan",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   5_000,
	}
	require.NoError(t, model.DB.Create(plan).Error)

	seedSubscription(t, subscriptionID, userID, 5_000, 0)
	result := model.DB.Model(&model.UserSubscription{}).
		Where("id = ?", subscriptionID).
		Update("plan_id", planID)
	require.NoError(t, result.Error)
	require.Equal(t, int64(1), result.RowsAffected)
}

func TestBillingFactoryPersonalModeIgnoresActiveSubscription(t *testing.T) {
	truncate(t)
	setBillingFactoryTestMode(t, true, false)

	const userID, tokenID, subscriptionID = 901, 901, 901
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-billing-mode-personal"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedSubscription(t, subscriptionID, userID, 5_000, 0)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	relayInfo.RequestId = "billing-mode-personal"
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceWallet, session.funding.Source())
	assert.Equal(t, BillingSourceWallet, relayInfo.BillingSource)
	assert.Equal(t, initialQuota-preConsumedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, int64(0), getSubscriptionUsed(t, subscriptionID))

	require.NoError(t, session.Settle(preConsumedQuota))
}

func TestBillingFactoryExternalSubscriptionOnlyUsesSubscription(t *testing.T) {
	truncate(t)
	setBillingFactoryTestMode(t, false, false)

	const userID, tokenID, subscriptionID, planID = 902, 902, 902, 1902
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-billing-mode-subscription"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedBillingFactorySubscription(t, userID, subscriptionID, planID)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	relayInfo.RequestId = "billing-mode-subscription"
	relayInfo.UserSetting.BillingPreference = "subscription_only"
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceSubscription, session.funding.Source())
	assert.Equal(t, BillingSourceSubscription, relayInfo.BillingSource)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, int64(preConsumedQuota), getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, subscriptionID, relayInfo.SubscriptionId)
	assert.Equal(t, int64(preConsumedQuota), relayInfo.SubscriptionPreConsumed)

	require.NoError(t, session.Settle(preConsumedQuota))
}

func TestBillingFactoryExternalWalletOnlyUsesWallet(t *testing.T) {
	truncate(t)
	setBillingFactoryTestMode(t, false, false)

	const userID, tokenID, subscriptionID = 903, 903, 903
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-billing-mode-wallet"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedSubscription(t, subscriptionID, userID, 5_000, 0)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	relayInfo.UserSetting.BillingPreference = "wallet_only"
	relayInfo.RequestId = "billing-mode-wallet"
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceWallet, session.funding.Source())
	assert.Equal(t, BillingSourceWallet, relayInfo.BillingSource)
	assert.Equal(t, initialQuota-preConsumedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-preConsumedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumedQuota, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, int64(0), getSubscriptionUsed(t, subscriptionID))

	require.NoError(t, session.Settle(preConsumedQuota))
}

func TestBillingFactoryDemoModeDoesNotUsePersonalStrategy(t *testing.T) {
	truncate(t)
	// Demo mode currently has precedence over self-use mode. Setting both flags
	// verifies that demo mode does not accidentally enter the personal branch.
	setBillingFactoryTestMode(t, true, true)

	const userID, tokenID, subscriptionID, planID = 904, 904, 904, 1904
	const initialQuota, initialTokenQuota, preConsumedQuota = 1_000, 1_000, 200
	const tokenKey = "sk-billing-mode-demo"

	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedBillingFactorySubscription(t, userID, subscriptionID, planID)

	relayInfo := personalBillingRelayInfo(userID, tokenID, tokenKey)
	relayInfo.RequestId = "billing-mode-demo"
	session, apiErr := NewBillingSession(
		newPersonalBillingTestContext(),
		relayInfo,
		preConsumedQuota,
	)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceSubscription, session.funding.Source())
	assert.Equal(t, BillingSourceSubscription, relayInfo.BillingSource)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(preConsumedQuota), getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, int64(preConsumedQuota), relayInfo.SubscriptionPreConsumed)

	require.NoError(t, session.Settle(preConsumedQuota))
}
