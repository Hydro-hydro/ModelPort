package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChargeViolationFeeIsOperationKeyedAndIdempotent(t *testing.T) {
	truncate(t)
	const (
		userID        = 990
		tokenID       = 990
		channelID     = 990
		initialWallet = 20_000
		initialToken  = 10_000
	)
	seedUser(t, userID, initialWallet)
	seedToken(t, tokenID, userID, "sk-violation-fee", initialToken)
	seedChannel(t, channelID)

	settings := model_setting.GetGrokSettings()
	previous := *settings
	settings.ViolationDeductionEnabled = true
	settings.ViolationDeductionAmount = 0.01
	t.Cleanup(func() { *settings = previous })

	ctx := newPersonalBillingTestContext()
	ctx.Set(common.RequestIdKey, "violation-fee-request")
	relayInfo := personalBillingRelayInfo(userID, tokenID, "sk-violation-fee")
	relayInfo.RequestId = "violation-fee-request"
	relayInfo.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channelID}
	relayInfo.PriceData.GroupRatioInfo.GroupRatio = 1
	err := types.NewOpenAIError(errors.New(CSAMViolationMarker), types.ErrorCodeViolationFeeGrokCSAM, http.StatusBadRequest)

	require.True(t, ChargeViolationFeeIfNeeded(ctx, relayInfo, err))
	require.True(t, ChargeViolationFeeIfNeeded(ctx, relayInfo, err))

	feeQuota := calcViolationFeeQuota(settings.ViolationDeductionAmount, 1)
	assert.Equal(t, initialWallet, getUserQuota(t, userID), "violation fee must not mutate wallet quota")
	var user model.User
	require.NoError(t, model.DB.Select("used_quota").First(&user, userID).Error)
	assert.Equal(t, feeQuota, user.UsedQuota)
	assert.Equal(t, initialToken-feeQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, feeQuota, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, int64(feeQuota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countLogs(t), "duplicate violation fee call must reuse one log")

	operation, opErr := model.GetBillingOperation(violationFeeOperationKey(ctx, relayInfo))
	require.NoError(t, opErr)
	assert.Equal(t, model.BillingOperationSettled, operation.Status)
	assert.True(t, operation.FundingApplied)
	assert.True(t, operation.TokenApplied)
	assert.True(t, operation.StatsApplied)
	assert.True(t, operation.LogApplied)
}
