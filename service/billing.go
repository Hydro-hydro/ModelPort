package service

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// recordBillingUsageStats persists the usage counters that belong to a
// request-bound billing operation. The durable path bypasses the process-local
// batch queue and marks the component only after both aggregates succeed.
func recordBillingUsageStats(relayInfo *relaycommon.RelayInfo, quota int, billable bool) error {
	if relayInfo == nil {
		return nil
	}
	session, durable := relayInfo.Billing.(*BillingSession)
	if !billable {
		if durable {
			return session.MarkBillingComponent(model.BillingComponentStats)
		}
		return nil
	}
	if durable {
		return model.ApplyBillingOperationStats(
			session.OperationKey(), relayInfo.UserId, relayInfo.ChannelId, quota, true,
		)
	}
	model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
	model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	return nil
}

// recordBillingConsumeLog writes one consume log and completes the durable log
// component only after the log database accepts the row.
func recordBillingConsumeLog(c *gin.Context, relayInfo *relaycommon.RelayInfo, params model.RecordConsumeLogParams) error {
	if relayInfo == nil {
		return nil
	}
	if session, durable := relayInfo.Billing.(*BillingSession); durable {
		if err := prepareBillingConsumeLog(relayInfo, params); err != nil {
			return err
		}
		params.BillingOperationKey = session.OperationKey()
		if err := model.RecordConsumeLogChecked(c, relayInfo.UserId, params); err != nil {
			return err
		}
		if err := session.MarkBillingComponent(model.BillingComponentLog); err != nil {
			return err
		}
		return finalizeRequestBillingOperation(session)
	}
	model.RecordConsumeLog(c, relayInfo.UserId, params)
	return nil
}

// prepareBillingConsumeLog persists the exact log payload before any quota or
// aggregate mutation. If the process exits after settlement, reconciliation
// can replay the independent log database write from this payload.
func prepareBillingConsumeLog(relayInfo *relaycommon.RelayInfo, params model.RecordConsumeLogParams) error {
	if relayInfo == nil {
		return nil
	}
	session, durable := relayInfo.Billing.(*BillingSession)
	if !durable {
		return nil
	}
	params.BillingOperationKey = session.OperationKey()
	return model.SetBillingOperationLogPayload(session.OperationKey(), params)
}

func finalizeRequestBillingOperation(session *BillingSession) error {
	if session == nil || session.OperationKey() == "" {
		return nil
	}
	operation, err := model.GetBillingOperation(session.OperationKey())
	if err != nil {
		return err
	}
	if !operation.TokenApplied || !operation.StatsApplied || !operation.LogApplied {
		return nil
	}
	updated, err := model.UpdateBillingOperationStatus(session.OperationKey(),
		[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying},
		model.BillingOperationSettled, "", common.GetTimestamp())
	if err != nil {
		return err
	}
	if updated {
		return nil
	}
	current, err := model.GetBillingOperation(session.OperationKey())
	if err != nil {
		return err
	}
	if current.Status != model.BillingOperationSettled {
		return fmt.Errorf("billing operation %s was not settled", session.OperationKey())
	}
	return nil
}

// newBillingConsumeLogParams keeps the common fields shared by the text,
// audio, and realtime usage paths in one place.
func newBillingConsumeLogParams(relayInfo *relaycommon.RelayInfo, quota int, modelName, tokenName, content, group string, promptTokens, completionTokens, useTimeSeconds int, other map[string]interface{}) model.RecordConsumeLogParams {
	return model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		ModelName:        modelName,
		TokenName:        tokenName,
		Quota:            quota,
		Content:          content,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   useTimeSeconds,
		IsStream:         relayInfo.IsStream,
		Group:            group,
		Other:            other,
	}
}

// PreConsumeBilling 创建 usage-only BillingSession 并执行 Token 额度预扣。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ensureBillingSessionForUsage lazily creates the usage-only session needed by
// a free-model request that reports a positive final charge (for example, a
// tool or audio surcharge). Free models skip the initial reservation, so the
// session has to be created after the final usage is known. A zero charge keeps
// the old no-session fast path.
func ensureBillingSessionForUsage(c *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo == nil || relayInfo.Billing != nil || actualQuota <= 0 {
		return nil
	}
	if !relayInfo.PriceData.FreeModel {
		return fmt.Errorf("billing session is required before usage settlement")
	}
	if apiErr := PreConsumeBilling(c, 0, relayInfo); apiErr != nil {
		return apiErr
	}
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling executes usage settlement through the request's BillingSession.
// A session is mandatory so no user-balance fallback can be selected.
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo == nil {
		return fmt.Errorf("relay info is required for usage settlement")
	}
	if relayInfo.Billing != nil {
		// 普通请求的终态结算只依赖 UsageAccounting；完整的
		// BillingSettler 能力仍保留给追加预扣和会话状态调用点。
		var accounting relaycommon.UsageAccounting = relayInfo.Billing
		preConsumed := accounting.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := accounting.Settle(actualQuota); err != nil {
			return err
		}

		return nil
	}

	// Free-model requests intentionally skip pre-consumption and therefore do
	// not have a BillingSession. A zero-usage settlement is the corresponding
	// no-op; a positive amount without a session indicates a broken caller and
	// must remain visible as an error instead of silently charging elsewhere.
	if actualQuota == 0 {
		return nil
	}
	return fmt.Errorf("billing session is required for usage settlement")
}
