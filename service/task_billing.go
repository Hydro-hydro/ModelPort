package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// loadTaskBillingOperation loads the operation that was created before the
// task row was submitted. New deployments always create this durable marker;
// a missing marker is an accounting error, never a reason to fall back to
// direct quota/statistics writes.
func loadTaskBillingOperation(task *model.Task) (*model.BillingOperation, bool, error) {
	if task == nil {
		return nil, true, fmt.Errorf("task billing operation requires a task")
	}
	key := strings.TrimSpace(task.PrivateData.BillingOperationKey)
	if key == "" {
		operation, err := model.EnsureTaskBillingOperation(task)
		return operation, true, err
	}
	operation, err := model.GetBillingOperation(key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		operation, err = model.EnsureTaskBillingOperation(task)
	}
	if err != nil {
		return nil, true, err
	}
	if operation == nil {
		return nil, true, errors.New("billing operation is empty")
	}
	if operation.TaskID != "" && operation.TaskID != task.TaskID {
		return nil, true, fmt.Errorf("billing operation %s belongs to task %s", operation.OperationKey, operation.TaskID)
	}
	return operation, true, nil
}

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task) {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		var contents []string
		if otherRatios := info.PriceData.OtherRatios(); len(otherRatios) > 0 {
			for key, ra := range otherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
		}
		if snap := info.TieredBillingSnapshot; snap != nil {
			for key, value := range snap.UsageFacts {
				contents = append(contents, fmt.Sprintf("%s: %v", key, value))
			}
		}
		if len(contents) > 0 {
			logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	if snap := info.TieredBillingSnapshot; snap != nil {
		other["billing_mode"] = "tiered_expr"
		other["expr_b64"] = base64.StdEncoding.EncodeToString([]byte(snap.ExprString))
		other["matched_tier"] = snap.EstimatedTier
		if len(snap.UsageFacts) > 0 {
			other["usage_facts"] = snap.UsageFacts
		}
	}
	appendTaskLogInfo(task, other)
	attachQuotaSaturation(c, info, other)
	logQuota := info.PriceData.Quota
	if task != nil {
		// The submit-time estimate can be adjusted by the adaptor before the task
		// is persisted. The task row is the authoritative amount for its log.
		logQuota = task.Quota
	}
	logParams := model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     logQuota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	}
	operation, _, operationErr := loadTaskBillingOperation(task)
	if operationErr != nil {
		logger.LogWarn(c, fmt.Sprintf("任务账单操作读取失败 task %s: %v", task.TaskID, operationErr))
		return
	}
	logParams.BillingOperationKey = operation.OperationKey
	if err := model.SetBillingOperationLogPayload(operation.OperationKey, logParams); err != nil {
		logger.LogWarn(c, fmt.Sprintf("任务消费日志参数持久化失败 task %s: %v", task.TaskID, err))
		return
	}
	if err := model.RecordConsumeLogChecked(c, info.UserId, logParams); err == nil {
		_ = model.MarkBillingOperationComponent(operation.OperationKey, model.BillingComponentLog)
	} else {
		logger.LogWarn(c, fmt.Sprintf("任务消费日志写入失败 task %s: %v", task.TaskID, err))
	}
	if err := model.ApplyBillingOperationStats(
		operation.OperationKey, info.UserId, info.ChannelId, logQuota, true,
	); err != nil {
		logger.LogWarn(c, fmt.Sprintf("任务用户统计写入失败 task %s: %v", task.TaskID, err))
	}
	// The durable operation is created before the task is inserted. Mark the
	// independent statistics and log components only after their writes have
	// been attempted; a failed marker leaves the operation retryable.
	if task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure {
		return
	}
	if task.Status == model.TaskStatusFailure {
		// The task's terminal failure path owns the refund transition. Keeping
		// the operation applying makes the pending charge recoverable.
		return
	}
	if operation, err := model.GetBillingOperation(operation.OperationKey); err == nil &&
		operation.FundingApplied && operation.TokenApplied && operation.StatsApplied && operation.LogApplied &&
		operation.FinalUsageApplied {
		_, _ = model.UpdateBillingOperationStatus(operation.OperationKey,
			[]model.BillingOperationStatus{model.BillingOperationApplying, model.BillingOperationReserved},
			model.BillingOperationSettled, "", common.GetTimestamp())
	}
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskAdjustFunding 调整任务的资金来源，delta > 0 表示扣费，delta < 0 表示退还。
// 个人版任务使用用量记账，不触碰用户钱包。显式的 wallet 来源仅供
// 非个人模式调用方使用；空来源按 usage 处理。
func taskAdjustFunding(task *model.Task, delta int) error {
	if task == nil || delta == 0 {
		return nil
	}
	source := strings.TrimSpace(task.PrivateData.BillingSource)
	if source == "" {
		source = BillingSourceUsage
	}
	switch source {
	case BillingSourceUsage:
		return nil
	case BillingSourceWallet:
		if delta > 0 {
			return model.DecreaseUserQuota(task.UserId, delta, false)
		}
		return model.IncreaseUserQuota(task.UserId, -delta, false)
	default:
		return fmt.Errorf("unknown task billing source %q", source)
	}
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if priceData := taskBillingContextPriceData(bc); priceData != nil {
			for k, v := range priceData.OtherRatios() {
				other[k] = v
			}
		}
		if snap := bc.TieredSnapshot; snap != nil {
			other["billing_mode"] = "tiered_expr"
			other["expr_b64"] = base64.StdEncoding.EncodeToString([]byte(snap.ExprString))
			other["matched_tier"] = snap.EstimatedTier
			if len(snap.UsageFacts) > 0 {
				other["usage_facts"] = snap.UsageFacts
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	appendTaskLogInfo(task, other)
	return other
}

func appendTaskLogInfo(task *model.Task, other map[string]interface{}) {
	if task == nil || other == nil {
		return
	}
	if task.TaskID != "" {
		other["task_id"] = task.TaskID
	}
	if task.PrivateData.Execution != nil {
		AppendTaskPluginAuditInfo(other, task.PrivateData.Execution.TaskPlugin)
	}
	if task.PrivateData.UpstreamTaskID == "" && task.PrivateData.NodeName == "" {
		return
	}
	rootInfo, ok := other["root_info"].(map[string]interface{})
	if !ok || rootInfo == nil {
		rootInfo = map[string]interface{}{}
		other["root_info"] = rootInfo
	}
	if task.PrivateData.UpstreamTaskID != "" {
		rootInfo["upstream_task_id"] = task.PrivateData.UpstreamTaskID
	}
	if task.PrivateData.NodeName != "" {
		rootInfo["node_name"] = task.PrivateData.NodeName
	}
}

func taskBillingContextPriceData(bc *model.TaskBillingContext) *types.PriceData {
	if bc == nil || len(bc.OtherRatios) == 0 {
		return nil
	}
	priceData := &types.PriceData{}
	if !priceData.ReplaceOtherRatios(bc.OtherRatios) {
		return nil
	}
	return priceData
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，退还资金与令牌额度，并回减用户和渠道用量。
// 返回资金来源是否已成功退还；失败时保留 quota，供显式重试或人工对账。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) bool {
	return refundTaskQuota(ctx, task, reason, false, "")
}

// RefundTaskQuotaAfterTerminal refunds a task after its caller has won the
// task-status terminal CAS. Async polling uses this entry point because task
// submission records the estimated charge up front, so a later upstream
// failure must be able to reverse an already-settled operation. Keeping the
// authorization explicit prevents stale direct callers from refunding a
// settled operation without first winning the task transition.
func RefundTaskQuotaAfterTerminal(ctx context.Context, task *model.Task, reason string) bool {
	return refundTaskQuota(ctx, task, reason, true, "")
}

// RefundTaskQuotaAfterTerminalOwned is used by the durable billing-operation
// worker after it has claimed an operation. Every component and the terminal
// transition are guarded by the same lease, so a worker whose lease expires
// cannot continue refunding after a successor has taken over.
func RefundTaskQuotaAfterTerminalOwned(ctx context.Context, task *model.Task, reason, workerID string) bool {
	if strings.TrimSpace(workerID) == "" {
		return false
	}
	return refundTaskQuota(ctx, task, reason, true, workerID)
}

func refundTaskQuota(ctx context.Context, task *model.Task, reason string, allowSettled bool, workerID string) bool {
	if task == nil {
		return false
	}
	key := operationKey(task)
	unlock := lockTaskRefund(key)
	defer unlock()

	operation, err := model.EnsureTaskBillingOperation(task)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("创建任务退款操作失败 task %s: %s", task.TaskID, err.Error()))
		return false
	}
	key = operation.OperationKey
	if !billingOperationLeaseActive(key, workerID) {
		return false
	}
	// A task is charged at submission and its operation is therefore normally
	// settled before the upstream task reaches a terminal state. A caller may
	// refund a settled operation only after it has won the task's terminal
	// failure CAS; stale pollers are rejected by that CAS before reaching here.
	if operation.Status == model.BillingOperationRefunded {
		return clearRefundedTaskQuota(ctx, task)
	}
	if operation.Status == model.BillingOperationSettled && !allowSettled {
		return false
	}
	wasSettled := operation.Status == model.BillingOperationSettled
	// A request-bound operation may have reserved quota before the task row was
	// created. Before settlement, task.Quota is only the provider's adjusted
	// estimate and may be lower than the durable reservation (including zero).
	// Refund the persisted reservation so an immediate provider failure cannot
	// strand the token quota. Once an operation is settled, the actual quota is
	// the amount that was charged and should be used by any terminal refund
	// path.
	quota := task.Quota
	if operation.Status == model.BillingOperationSettled {
		quota = operation.ActualQuota
	} else if operation.PreConsumedQuota > quota {
		quota = operation.PreConsumedQuota
	}
	if quota == 0 {
		return completeZeroQuotaRefund(key, operation.Status, allowSettled, workerID)
	}
	fromStatuses := []model.BillingOperationStatus{
		model.BillingOperationReserved,
		model.BillingOperationApplying,
		model.BillingOperationRefundPending,
	}
	if allowSettled {
		fromStatuses = append(fromStatuses, model.BillingOperationSettled)
	}
	var transitioned bool
	if workerID != "" && operation.Status != model.BillingOperationRefundPending {
		transitioned, err = model.UpdateBillingOperationStatusOwnedKeepLease(key, workerID,
			fromStatuses, model.BillingOperationRefundPending, reason, common.GetTimestamp(), common.GetTimestamp())
	} else if workerID != "" {
		transitioned = true
	} else {
		transitioned, err = model.UpdateBillingOperationStatus(key,
			fromStatuses,
			model.BillingOperationRefundPending, reason, common.GetTimestamp())
	}
	if err != nil {
		return false
	}
	if !transitioned {
		// Another worker may have won the state transition. Reload before
		// deciding whether this invocation is already complete or must retry.
		current, getErr := model.GetBillingOperation(key)
		if getErr != nil || current == nil {
			return false
		}
		switch current.Status {
		case model.BillingOperationRefunded:
			return clearRefundedTaskQuota(ctx, task)
		case model.BillingOperationSettled:
			return false
		default:
			return false
		}
	}

	operation, err = model.GetBillingOperation(key)
	if err != nil {
		return false
	}

	// 1. 退还管理员钱包额度
	if !operation.RefundFundingApplied {
		if !wasSettled || operation.FundingApplied {
			if !billingOperationLeaseActive(key, workerID) {
				return false
			}
			if err := taskAdjustFunding(task, -quota); err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
				return false
			}
		}
		if err := markRefundComponent(key, model.BillingComponentFunding, workerID); err != nil {
			return false
		}
	}

	// 2. 退还令牌额度
	operation, err = model.GetBillingOperation(key)
	if err != nil {
		return false
	}
	if !operation.RefundTokenApplied {
		// Keep the token mutation and its durable refund marker in one database
		// transaction. A direct quota update followed by a marker write leaves a
		// crash window in which the retry would restore the same reservation a
		// second time.
		refundTokenQuota := 0
		if operation.TokenApplied {
			if operation.ActualQuotaSet {
				refundTokenQuota = operation.ActualQuota
			} else {
				refundTokenQuota = operation.TokenReservedQuota
			}
		} else if operation.TokenReserved {
			refundTokenQuota = operation.TokenReservedQuota
		} else if operation.ActualQuotaSet && operation.PreConsumedQuota > 0 && operation.TokenID > 0 {
			// The request may have committed the token row immediately before
			// the durable reservation marker. The persisted request reservation
			// is still authoritative for recovery in that crash window.
			refundTokenQuota = operation.PreConsumedQuota
		}
		if refundTokenQuota > 0 {
			if !billingOperationLeaseActive(key, workerID) {
				return false
			}
			if operation.TokenID <= 0 {
				return false
			}
			token, tokenErr := model.GetTokenById(operation.TokenID)
			if tokenErr != nil {
				return false
			}
			if workerID != "" {
				tokenErr = model.ApplyBillingOperationRefundTokenOwned(
					key, token.Id, token.Key, refundTokenQuota, token.UnlimitedQuota,
					workerID, common.GetTimestamp(),
				)
			} else {
				tokenErr = model.ApplyBillingOperationRefundToken(
					key, token.Id, token.Key, refundTokenQuota, token.UnlimitedQuota,
				)
			}
			if tokenErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("退还令牌额度失败 task %s: %v", task.TaskID, tokenErr))
				return false
			}
		} else if err := markRefundComponent(key, model.BillingComponentToken, workerID); err != nil {
			return false
		}
	}

	// 3. 回减预扣时累计的用户和渠道用量，请求次数保持不变
	operation, err = model.GetBillingOperation(key)
	if err != nil {
		return false
	}
	if !operation.RefundStatsApplied {
		// A request-bound operation writes usage counters only after settlement.
		// Immediate task failures are refunded before that component exists; do
		// not turn the absence of a charge into a negative usage counter.
		if !billingOperationLeaseActive(key, workerID) {
			return false
		}
		var statsErr error
		if workerID != "" {
			statsErr = model.ApplyBillingOperationRefundStatsOwned(key, task.UserId, task.ChannelId, quota, workerID, common.GetTimestamp())
		} else {
			statsErr = model.ApplyBillingOperationRefundStats(key, task.UserId, task.ChannelId, quota)
		}
		if err := statsErr; err != nil {
			return false
		}
	}

	// 4. 记录日志
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	operation, err = model.GetBillingOperation(key)
	if err != nil {
		return false
	}
	if !operation.RefundLogApplied {
		if !billingOperationLeaseActive(key, workerID) {
			return false
		}
		if err := model.RecordTaskBillingLogCheckedOwned(model.RecordTaskBillingLogParams{
			UserId:              task.UserId,
			LogType:             model.LogTypeRefund,
			Content:             "",
			ChannelId:           task.ChannelId,
			ModelName:           taskModelName(task),
			Quota:               quota,
			TokenId:             task.PrivateData.TokenId,
			Group:               task.Group,
			Other:               other,
			BillingOperationKey: key + ":refund",
		}, workerID); err != nil {
			return false
		}
		if err := markRefundComponent(key, model.BillingComponentLog, workerID); err != nil {
			return false
		}
	}

	operation, err = model.GetBillingOperation(key)
	if err != nil || operation == nil || !operation.RefundFundingApplied || !operation.RefundTokenApplied ||
		!operation.RefundStatsApplied || !operation.RefundLogApplied {
		return false
	}
	// Persist the durable terminal state before clearing task.Quota. If the
	// task update is interrupted, a retry sees refunded and only repeats the
	// harmless marker clear; it never repeats a money movement.
	var refunded bool
	if workerID != "" {
		refunded, err = model.UpdateBillingOperationStatusOwned(key, workerID,
			[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
			model.BillingOperationRefunded, "", common.GetTimestamp(), common.GetTimestamp())
	} else {
		refunded, err = model.UpdateBillingOperationStatus(key,
			[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
			model.BillingOperationRefunded, "", common.GetTimestamp())
	}
	if err != nil {
		return false
	}
	if !refunded {
		operation, err = model.GetBillingOperation(key)
		if err != nil || operation.Status != model.BillingOperationRefunded {
			return false
		}
	}
	return clearRefundedTaskQuota(ctx, task)
}

func billingOperationLeaseActive(operationKey, workerID string) bool {
	if workerID == "" {
		return true
	}
	owned, err := model.BillingOperationLeaseOwned(operationKey, workerID, common.GetTimestamp())
	return err == nil && owned
}

func markRefundComponent(operationKey, component, workerID string) error {
	if workerID == "" {
		return model.MarkBillingOperationRefundComponent(operationKey, component)
	}
	return model.MarkBillingOperationRefundComponentOwned(operationKey, component, workerID, common.GetTimestamp())
}

var taskRefundLocks sync.Map

func lockTaskRefund(key string) func() {
	value, _ := taskRefundLocks.LoadOrStore(key, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func completeZeroQuotaRefund(key string, status model.BillingOperationStatus, allowSettled bool, workerID string) bool {
	operation, err := model.GetBillingOperation(key)
	if err != nil || operation == nil {
		return false
	}
	if operation.Status == model.BillingOperationRefunded {
		return true
	}
	// A zero task marker is safe to close only when there was no durable
	// reservation. If a process cleared task.Quota before completing the
	// refund, retain the operation for reconciliation instead of claiming that
	// money movement succeeded.
	if operation.PreConsumedQuota != 0 || operation.ActualQuota != 0 {
		return false
	}
	for _, component := range []string{model.BillingComponentFunding, model.BillingComponentToken, model.BillingComponentStats, model.BillingComponentLog} {
		if !billingOperationLeaseActive(key, workerID) {
			return false
		}
		if err := markRefundComponent(key, component, workerID); err != nil {
			return false
		}
	}
	fromStatuses := []model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying, model.BillingOperationRefundPending}
	if allowSettled {
		fromStatuses = append(fromStatuses, model.BillingOperationSettled)
	}
	var refunded bool
	if workerID != "" {
		refunded, err = model.UpdateBillingOperationStatusOwned(key, workerID,
			fromStatuses, model.BillingOperationRefunded, "", common.GetTimestamp(), common.GetTimestamp())
	} else {
		refunded, err = model.UpdateBillingOperationStatus(key,
			fromStatuses,
			model.BillingOperationRefunded, "", common.GetTimestamp())
	}
	if err != nil || refunded {
		return err == nil
	}
	operation, err = model.GetBillingOperation(key)
	return err == nil && operation.Status == model.BillingOperationRefunded
}

func clearRefundedTaskQuota(ctx context.Context, task *model.Task) bool {
	if task == nil || task.Quota == 0 {
		return true
	}
	previousQuota := task.Quota
	task.Quota = 0
	if err := task.UpdateQuota(); err != nil {
		task.Quota = previousQuota
		logger.LogError(ctx, fmt.Sprintf("退款完成但清除 task quota 失败 task %s: %s", task.TaskID, err.Error()))
		return false
	}
	return true
}

func operationKey(task *model.Task) string {
	if task.PrivateData.BillingOperationKey != "" {
		return task.PrivateData.BillingOperationKey
	}
	key := model.BillingOperationKeyForTask(task.TaskID)
	if task.ID > 0 {
		return fmt.Sprintf("%s:id:%d", key, task.ID)
	}
	return key
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
// clamps 可选：若计算 actualQuota 时发生额度饱和，将其记入日志 admin_info（仅管理员可见）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) {
	if task == nil || actualQuota < 0 {
		return
	}
	if taskBillingOperationTerminal(task) {
		return
	}
	operation, _, operationErr := loadTaskBillingOperation(task)
	if operationErr != nil {
		logger.LogError(ctx, fmt.Sprintf("加载任务账单操作失败 task %s: %v", task.TaskID, operationErr))
		return
	}
	// The durable operation is the source of truth after a process restart.
	// The task row may still contain the old estimate if the prior attempt
	// committed quota/token changes before its task update was interrupted.
	preConsumedQuota := task.Quota
	if operation.ActualQuotaSet {
		preConsumedQuota = operation.ActualQuota
	}
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		if err := model.ApplyBillingOperationStatsAdjustment(operation.OperationKey, task.UserId, task.ChannelId, actualQuota); err != nil {
			logger.LogError(ctx, fmt.Sprintf("任务统计结算失败 task %s: %s", task.TaskID, err.Error()))
			return
		}
		finalizeTaskBillingOperation(task, actualQuota)
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	// 调整资金来源
	if err := taskAdjustFunding(task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 调整令牌额度. Durable operations update the token row and their marker
	// atomically.
	if operation.TokenID > 0 {
		token, err := model.GetTokenById(operation.TokenID)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("获取任务令牌失败 task %s: %v", task.TaskID, err))
			return
		}
		if err := model.ApplyBillingOperationTokenAdjustment(
			operation.OperationKey, token.Id, token.Key, actualQuota, token.UnlimitedQuota,
		); err != nil {
			logger.LogError(ctx, fmt.Sprintf("任务令牌结算失败 task %s: %v", task.TaskID, err))
			return
		}
	} else if err := model.UpdateBillingOperationActualQuota(operation.OperationKey, actualQuota); err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务实际额度持久化失败 task %s: %v", task.TaskID, err))
		return
	}

	task.Quota = actualQuota
	if task.ID > 0 {
		if err := task.UpdateQuota(); err != nil {
			logger.LogError(ctx, fmt.Sprintf("差额结算回写 quota 失败 task %s: %s", task.TaskID, err.Error()))
			return
		}
	}

	// 提交阶段已经累计过一次请求；结算阶段只调整最终用量。带有持久化
	// 操作的任务把统计额度和幂等标记放在同一主库事务中。
	if err := model.ApplyBillingOperationStatsAdjustment(operation.OperationKey, task.UserId, task.ChannelId, actualQuota); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算统计失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	if err := model.RecordTaskBillingLogChecked(model.RecordTaskBillingLogParams{
		UserId:              task.UserId,
		LogType:             logType,
		Content:             reason,
		ChannelId:           task.ChannelId,
		ModelName:           taskModelName(task),
		Quota:               logQuota,
		TokenId:             task.PrivateData.TokenId,
		Group:               task.Group,
		Other:               other,
		NodeName:            task.PrivateData.NodeName,
		BillingOperationKey: fmt.Sprintf("%s:adjustment:%d", operation.OperationKey, actualQuota),
	}); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算日志失败 task %s: %s", task.TaskID, err.Error()))
		return
	}
	finalizeTaskBillingOperation(task, actualQuota)
}

func taskBillingOperationTerminal(task *model.Task) bool {
	if task == nil || task.PrivateData.BillingOperationKey == "" {
		return false
	}
	operation, err := model.GetBillingOperation(task.PrivateData.BillingOperationKey)
	if err != nil || operation == nil {
		return false
	}
	return operation.Status == model.BillingOperationSettled || operation.Status == model.BillingOperationRefunded
}

// finalizeTaskBillingOperation closes a task operation only after its terminal
// usage has been persisted. Submission accounting intentionally leaves async
// task operations in applying so a later failure can still refund them.
func finalizeTaskBillingOperation(task *model.Task, actualQuota int) {
	if task == nil || task.PrivateData.BillingOperationKey == "" {
		return
	}
	key := task.PrivateData.BillingOperationKey
	if err := model.MarkBillingOperationFinalUsage(key, actualQuota); err != nil {
		common.SysLog(fmt.Sprintf("failed to persist task final usage task=%s: %v", task.TaskID, err))
		return
	}
	operation, err := model.GetBillingOperation(key)
	if err != nil || operation == nil || !operation.FundingApplied || !operation.TokenApplied ||
		!operation.StatsApplied || !operation.LogApplied || !operation.FinalUsageApplied {
		return
	}
	if _, err := model.UpdateBillingOperationStatus(key,
		[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying},
		model.BillingOperationSettled, "", common.GetTimestamp()); err != nil {
		common.SysLog(fmt.Sprintf("failed to settle task billing operation task=%s: %v", task.TaskID, err))
	}
}

// MarkTaskBillingFinalUsage records the terminal usage boundary without
// attempting the operation status transition. Immediate task submission calls
// this before the independent consume-log write, leaving the operation
// recoverable if the process exits in that gap.
func MarkTaskBillingFinalUsage(task *model.Task, actualQuota int) {
	if task == nil || task.PrivateData.BillingOperationKey == "" || actualQuota < 0 {
		return
	}
	if err := model.MarkBillingOperationFinalUsage(task.PrivateData.BillingOperationKey, actualQuota); err != nil {
		common.SysLog(fmt.Sprintf("failed to persist task final usage task=%s: %v", task.TaskID, err))
	}
}

// FinalizeTaskBillingOperation closes a task operation after the synchronous
// submission path has persisted its final charge and consume log. Immediate
// task results do not pass through the polling adaptor, but they still need
// the same final-usage marker before the durable worker can settle the row.
func FinalizeTaskBillingOperation(task *model.Task, actualQuota int) {
	finalizeTaskBillingOperation(task, actualQuota)
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) bool {
	if totalTokens <= 0 {
		return false
	}

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return false
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return false
	}

	groupRatio := ratio_setting.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if priceData := taskBillingContextPriceData(task.PrivateData.BillingContext); priceData != nil {
		otherMultiplier = priceData.OtherRatioMultiplier()
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier（饱和转换，防止溢出成负数）
	actualQuota, clamp := common.QuotaFromFloatChecked(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	RecalculateTaskQuota(ctx, task, actualQuota, reason, clamp)
	return true
}
