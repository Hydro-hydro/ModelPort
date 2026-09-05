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
	if !operation.FundingApplied || !operation.TokenApplied || !operation.StatsApplied || !operation.LogApplied {
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

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
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

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
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

		if actualQuota != 0 {
			checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		return PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
	}
	return nil
}
