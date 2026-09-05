package service

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/shopspring/decimal"

	"github.com/gin-gonic/gin"
)

const (
	ViolationFeeCodePrefix     = "violation_fee."
	CSAMViolationMarker        = "Failed check: SAFETY_CHECK_TYPE"
	ContentViolatesUsageMarker = "Content violates usage guidelines"
)

func IsViolationFeeCode(code types.ErrorCode) bool {
	return strings.HasPrefix(string(code), ViolationFeeCodePrefix)
}

func HasCSAMViolationMarker(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), CSAMViolationMarker) || strings.Contains(err.Error(), ContentViolatesUsageMarker) {
		return true
	}
	msg := err.ToOpenAIError().Message
	return strings.Contains(msg, CSAMViolationMarker) || strings.Contains(err.Error(), ContentViolatesUsageMarker)
}

func WrapAsViolationFeeGrokCSAM(err *types.NewAPIError) *types.NewAPIError {
	if err == nil {
		return nil
	}
	oai := err.ToOpenAIError()
	oai.Type = string(types.ErrorCodeViolationFeeGrokCSAM)
	oai.Code = string(types.ErrorCodeViolationFeeGrokCSAM)
	return types.WithOpenAIError(oai, err.StatusCode, types.ErrOptionWithSkipRetry())
}

// NormalizeViolationFeeError ensures:
// - if the CSAM marker is present, error.code is set to a stable violation-fee code and skip-retry is enabled.
// - if error.code already has the violation-fee prefix, skip-retry is enabled.
//
// It must be called before retry decision logic.
func NormalizeViolationFeeError(err *types.NewAPIError) *types.NewAPIError {
	if err == nil {
		return nil
	}

	if HasCSAMViolationMarker(err) {
		return WrapAsViolationFeeGrokCSAM(err)
	}

	if IsViolationFeeCode(err.GetErrorCode()) {
		oai := err.ToOpenAIError()
		return types.WithOpenAIError(oai, err.StatusCode, types.ErrOptionWithSkipRetry())
	}

	return err
}

func shouldChargeViolationFee(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.GetErrorCode() == types.ErrorCodeViolationFeeGrokCSAM {
		return true
	}
	// In case some callers didn't normalize, keep a safety net.
	return HasCSAMViolationMarker(err)
}

func calcViolationFeeQuota(amount, groupRatio float64) int {
	quota, _ := calcViolationFeeQuotaChecked(amount, groupRatio)
	return quota
}

func calcViolationFeeQuotaChecked(amount, groupRatio float64) (int, *common.QuotaClamp) {
	if amount <= 0 {
		return 0, nil
	}
	if groupRatio <= 0 {
		return 0, nil
	}
	if math.IsNaN(amount) || math.IsNaN(groupRatio) {
		return common.QuotaFromFloatChecked(math.NaN())
	}
	if math.IsInf(amount, 1) || math.IsInf(groupRatio, 1) {
		return common.QuotaFromFloatChecked(math.Inf(1))
	}
	return common.QuotaFromDecimalChecked(decimal.NewFromFloat(amount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(groupRatio)))
}

// ChargeViolationFeeIfNeeded charges an additional fee after the normal flow finishes (including refund).
// It uses Grok fee settings as the fee policy.
func ChargeViolationFeeIfNeeded(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) bool {
	if ctx == nil || relayInfo == nil || apiErr == nil {
		return false
	}
	// Violation fees are usage records in personal mode as well; they use the
	// same Token/statistics/log components as the normal request path.
	relayInfo.BillingSource = BillingSourceUsage
	//if relayInfo.IsPlayground {
	//	return false
	//}
	if !shouldChargeViolationFee(apiErr) {
		return false
	}

	settings := model_setting.GetGrokSettings()
	if settings == nil || !settings.ViolationDeductionEnabled {
		return false
	}

	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	feeQuota, clamp := calcViolationFeeQuotaChecked(settings.ViolationDeductionAmount, groupRatio)
	noteQuotaClamp(relayInfo, clamp)
	if feeQuota <= 0 {
		return false
	}

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	tokenName := ctx.GetString("token_name")
	oai := apiErr.ToOpenAIError()

	other := map[string]any{
		"violation_fee":        true,
		"violation_fee_code":   string(types.ErrorCodeViolationFeeGrokCSAM),
		"fee_quota":            feeQuota,
		"base_amount":          settings.ViolationDeductionAmount,
		"group_ratio":          groupRatio,
		"status_code":          apiErr.StatusCode,
		"upstream_error_type":  oai.Type,
		"upstream_error_code":  fmt.Sprintf("%v", oai.Code),
		"violation_fee_marker": CSAMViolationMarker,
	}

	params := model.RecordConsumeLogParams{
		ChannelId:      relayInfo.ChannelId,
		ModelName:      relayInfo.OriginModelName,
		TokenName:      tokenName,
		Quota:          feeQuota,
		Content:        "Violation fee charged",
		TokenId:        relayInfo.TokenId,
		UseTimeSeconds: int(useTimeSeconds),
		IsStream:       relayInfo.IsStream,
		Group:          relayInfo.UsingGroup,
		Other:          other,
	}
	operationKey := violationFeeOperationKey(ctx, relayInfo)
	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     operationKey,
		RequestID:        relayInfo.RequestId,
		UserID:           relayInfo.UserId,
		TokenID:          relayInfo.TokenId,
		ChannelID:        relayInfo.ChannelId,
		PreConsumedQuota: 0,
		ActualQuota:      feeQuota,
		ActualQuotaSet:   true,
	})
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("failed to create violation fee billing operation: %s", err.Error()))
		return false
	}
	if operation.Status == model.BillingOperationSettled {
		return true
	}
	if operation.Status == model.BillingOperationRefunded || operation.Status == model.BillingOperationRefundPending || operation.Status == model.BillingOperationFailed {
		logger.LogError(ctx, fmt.Sprintf("violation fee operation %s is already %s", operationKey, operation.Status))
		return false
	}
	if err := model.SetBillingOperationLogPayload(operationKey, params); err != nil {
		logger.LogError(ctx, fmt.Sprintf("failed to prepare violation fee log: %s", err.Error()))
		return false
	}

	operation, err = model.GetBillingOperation(operationKey)
	if err != nil {
		return false
	}
	if !operation.TokenApplied {
		if relayInfo.IsPlayground || relayInfo.TokenId <= 0 {
			err = model.MarkBillingOperationComponent(operationKey, model.BillingComponentToken)
		} else {
			var token *model.Token
			token, err = model.GetTokenById(relayInfo.TokenId)
			if err == nil {
				err = model.ApplyBillingOperationTokenAdjustment(
					operationKey, token.Id, token.Key, feeQuota, token.UnlimitedQuota,
				)
			}
		}
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("failed to apply violation fee token quota: %s", err.Error()))
			return false
		}
	}
	operation, err = model.GetBillingOperation(operationKey)
	if err != nil {
		return false
	}
	if !operation.StatsApplied {
		if err := model.ApplyBillingOperationStats(operationKey, relayInfo.UserId, relayInfo.ChannelId, feeQuota, true); err != nil {
			logger.LogError(ctx, fmt.Sprintf("failed to apply violation fee usage stats: %s", err.Error()))
			return false
		}
	}
	operation, err = model.GetBillingOperation(operationKey)
	if err != nil {
		return false
	}
	if !operation.LogApplied {
		params.BillingOperationKey = operationKey
		if err := model.RecordConsumeLogChecked(ctx, relayInfo.UserId, params); err != nil {
			logger.LogError(ctx, fmt.Sprintf("failed to record violation fee log: %s", err.Error()))
			return false
		}
		if err := model.MarkBillingOperationComponent(operationKey, model.BillingComponentLog); err != nil {
			logger.LogError(ctx, fmt.Sprintf("failed to mark violation fee log: %s", err.Error()))
			return false
		}
	}
	updated, err := model.UpdateBillingOperationStatus(operationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying},
		model.BillingOperationSettled, "", common.GetTimestamp())
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("failed to settle violation fee operation: %s", err.Error()))
		return false
	}
	if updated {
		return true
	}
	operation, err = model.GetBillingOperation(operationKey)
	return err == nil && operation != nil && operation.Status == model.BillingOperationSettled
}

func violationFeeOperationKey(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) string {
	requestID := ""
	if relayInfo != nil {
		requestID = strings.TrimSpace(relayInfo.RequestId)
	}
	if requestID == "" && ctx != nil {
		requestID = strings.TrimSpace(ctx.GetString(common.RequestIdKey))
	}
	if requestID == "" {
		requestID = common.NewRequestId()
		if relayInfo != nil {
			relayInfo.RequestId = requestID
		}
	}
	return model.BillingOperationKeyForRequest(requestID + ":violation_fee")
}
