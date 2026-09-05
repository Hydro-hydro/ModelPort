package service

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// BillingSession — 统一计费会话
// ---------------------------------------------------------------------------

// BillingSession manages Token quota reservation, settlement, and refund for
// one request. Personal mode never reads or mutates a user balance.
type BillingSession struct {
	relayInfo         *relaycommon.RelayInfo
	operationKey      string
	preConsumedQuota  int  // 实际预扣额度（可为 0）
	tokenConsumed     int  // 令牌额度实际扣减量
	extraReserved     int  // 发送前补充预扣的额度
	tokenSettled      bool // 令牌差额已成功提交
	settlementStarted bool // 已锁定本次结算差额，失败重试不得改变目标
	settlementDelta   int  // actualQuota - preConsumedQuota
	settled           bool // Settle 的 Token、统计和日志组件全部完成
	refunded          bool // 令牌额度及必要账单组件均已成功退款
	refundInFlight    bool // 退款操作正在执行，防止并发重复退款
	tokenRefunded     bool // 令牌额度退款已成功
	tokenRefundMarked bool // 令牌额度退款完成标记已持久化
	statsRefundMarked bool // 用户/渠道统计退款完成标记已持久化
	logRefundMarked   bool // 消费日志退款完成标记已持久化
	mu                sync.Mutex
}

var _ relaycommon.UsageAccounting = (*BillingSession)(nil)
var _ relaycommon.BillingSettler = (*BillingSession)(nil)

// Settle applies the actual usage to the Token quota. Usage statistics and logs
// are committed by their own idempotent billing components.
func (s *BillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if actualQuota < 0 {
		return fmt.Errorf("billing actual quota cannot be negative: %d", actualQuota)
	}
	if s.settled || s.refunded {
		return nil
	}
	if s.refundInFlight {
		return errors.New("billing refund is in progress")
	}
	if !s.settlementStarted {
		s.settlementStarted = true
		s.settlementDelta = actualQuota - s.preConsumedQuota
	} else {
		// The first Settle call fixes the target for this in-memory session. A
		// retry may arrive with a stale or different usage snapshot after one
		// component has already been applied; changing ActualQuota here would
		// make the durable target disagree with the already-applied delta.
		// Keep retries idempotent by reusing the original target.
		actualQuota = s.preConsumedQuota + s.settlementDelta
	}
	if s.operationKey != "" {
		// A durable terminal operation may be observed by a restarted request
		// handler. Treat it as an idempotent completion, but never continue a
		// settlement after a refund has won the CAS race.
		operation, err := model.GetBillingOperation(s.operationKey)
		if err != nil {
			return err
		}
		if operation.TokenApplied {
			s.tokenSettled = true
		}
		switch operation.Status {
		case model.BillingOperationSettled:
			return s.adoptDurableSettled(operation)
		case model.BillingOperationRefunded, model.BillingOperationRefundPending, model.BillingOperationFailed:
			return fmt.Errorf("billing operation %s is already %s", s.operationKey, operation.Status)
		}
		if err := model.UpdateBillingOperationActualQuota(s.operationKey, actualQuota); err != nil {
			// Another worker may have completed the operation between the read
			// above and this update. Do not apply a second quota adjustment.
			if operation, getErr := model.GetBillingOperation(s.operationKey); getErr == nil && operation.Status == model.BillingOperationSettled {
				return s.adoptDurableSettled(operation)
			}
			return err
		}
		updated, err := model.UpdateBillingOperationStatus(s.operationKey,
			[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying},
			model.BillingOperationApplying, "", common.GetTimestamp())
		if err != nil {
			return err
		}
		if !updated {
			// Updating applying -> applying can be reported as unchanged by a
			// database driver. It is safe to continue only when the row is still
			// applying; refund/failed/terminal rows must stop here.
			operation, getErr := model.GetBillingOperation(s.operationKey)
			if getErr != nil {
				return getErr
			}
			if operation.Status == model.BillingOperationSettled {
				return s.adoptDurableSettled(operation)
			}
			if operation.Status != model.BillingOperationApplying {
				return fmt.Errorf("billing operation %s transition rejected in state %s", s.operationKey, operation.Status)
			}
		}
	}
	delta := s.settlementDelta
	if delta == 0 {
		s.tokenSettled = true
		if err := s.markOperationComponent(model.BillingComponentToken); err != nil {
			return err
		}
		// Statistics are written by the request/task accounting path. Keep the
		// in-memory session retryable until that component is durable; otherwise
		// a successful token adjustment could hide a lost usage aggregate.
		if s.operationKey == "" {
			s.settled = true
			return nil
		}
		operation, err := model.GetBillingOperation(s.operationKey)
		if err != nil {
			return err
		}
		if !billingOperationComponentsComplete(operation) {
			return nil
		}
		return s.adoptDurableSettled(operation)
	}
	// 调整令牌额度。令牌更新失败时不能标记 settled；下一次 Settle
	// 会只重试这一项。
	if !s.tokenSettled && !s.relayInfo.IsPlayground && s.relayInfo.TokenId > 0 {
		var tokenErr error
		if s.operationKey != "" {
			tokenErr = model.ApplyBillingOperationToken(s.operationKey, s.relayInfo.TokenId, s.relayInfo.TokenKey, delta, s.relayInfo.TokenUnlimited)
		} else if delta > 0 {
			tokenErr = model.DecreaseTokenQuotaImmediate(s.relayInfo.TokenId, s.relayInfo.TokenKey, delta)
		} else {
			tokenErr = model.IncreaseTokenQuotaImmediate(s.relayInfo.TokenId, s.relayInfo.TokenKey, -delta)
		}
		if tokenErr != nil {
			common.SysLog(fmt.Sprintf("error adjusting token quota after usage settlement (userId=%d, tokenId=%d, delta=%d): %s",
				s.relayInfo.UserId, s.relayInfo.TokenId, delta, tokenErr.Error()))
			return tokenErr
		}
		s.tokenSettled = true
	} else if s.relayInfo.IsPlayground || s.relayInfo.TokenId <= 0 {
		// Usage-funded requests may legitimately have no API token (for
		// example, an internal/playground route). There is no token row to
		// adjust in that case, but the token component still needs a durable
		// no-op marker so the operation can reach its terminal state.
		s.tokenSettled = true
	}
	if err := s.markOperationComponent(model.BillingComponentToken); err != nil {
		return err
	}
	if s.operationKey == "" {
		s.settled = true
		return nil
	}
	operation, err := model.GetBillingOperation(s.operationKey)
	if err != nil {
		return err
	}
	if !billingOperationComponentsComplete(operation) {
		return nil
	}
	return s.adoptDurableSettled(operation)
}

func billingOperationComponentsComplete(operation *model.BillingOperation) bool {
	return operation != nil && operation.TokenApplied && operation.StatsApplied && operation.LogApplied
}

func (s *BillingSession) adoptDurableSettled(operation *model.BillingOperation) error {
	if !billingOperationComponentsComplete(operation) {
		return fmt.Errorf("billing operation %s is settled with incomplete components", s.operationKey)
	}
	s.tokenSettled = true
	s.settled = true
	return nil
}

func (s *BillingSession) markOperationComponent(component string) error {
	if s.operationKey == "" {
		return nil
	}
	return model.MarkBillingOperationComponent(s.operationKey, component)
}

func (s *BillingSession) markOperationRefundComponent(component string) error {
	if s.operationKey == "" {
		return nil
	}
	return model.MarkBillingOperationRefundComponent(s.operationKey, component)
}

// MarkBillingComponent records completion of a non-quota component after its
// caller has persisted the corresponding statistic or log. It is deliberately
// best-effort at call sites: a failed mark leaves the operation retryable.
func (s *BillingSession) MarkBillingComponent(component string) error {
	return s.markOperationComponent(component)
}

// Refund 退还所有预扣费，幂等安全。接口保留无返回值以兼容 relaykit；
// 内部同步实现通过错误返回让调用方和日志知道退款是否真正完成。
func (s *BillingSession) Refund(c *gin.Context) {
	if err := s.refundWithError(c); err != nil {
		common.SysLog("billing refund failed: " + err.Error())
	}
}

func (s *BillingSession) refundWithError(c *gin.Context) error {
	s.mu.Lock()
	if s.settled || s.refunded || s.refundInFlight || !s.needsRefundLocked() {
		s.mu.Unlock()
		return nil
	}
	s.refundInFlight = true
	s.mu.Unlock()
	if s.operationKey != "" {
		transitioned, err := model.UpdateBillingOperationStatus(s.operationKey,
			[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying, model.BillingOperationRefundPending},
			model.BillingOperationRefundPending, "", common.GetTimestamp())
		if err != nil {
			common.SysLog("failed to mark billing operation refund pending: " + err.Error())
			s.mu.Lock()
			s.refundInFlight = false
			s.mu.Unlock()
			return err
		}
		if !transitioned {
			operation, getErr := model.GetBillingOperation(s.operationKey)
			if getErr != nil {
				common.SysLog("failed to reload billing operation before refund: " + getErr.Error())
				s.mu.Lock()
				s.refundInFlight = false
				s.mu.Unlock()
				return getErr
			}
			s.mu.Lock()
			switch operation.Status {
			case model.BillingOperationRefunded:
				s.refunded = true
			case model.BillingOperationSettled:
				s.settled = true
			case model.BillingOperationRefundPending:
				// The durable refund intent is already visible. Some database
				// drivers report a same-value CAS (refund_pending ->
				// refund_pending) as zero affected rows; that must not prevent
				// this retry from applying the still-pending components.
			default:
				// A retry is safe only after the durable refund intent is visible.
				// Reserved/applying means the CAS result was ambiguous, so leave
				// the side effects untouched and let the outbox retry it.
				s.refundInFlight = false
				s.mu.Unlock()
				return fmt.Errorf("billing operation %s refund transition was not won", s.operationKey)
			}
			if operation.Status != model.BillingOperationRefundPending {
				s.refundInFlight = false
				s.mu.Unlock()
				return nil
			}
			s.mu.Unlock()
			// Continue below with the existing durable refund intent.
		}
	}

	logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣 Token 额度（token_quota=%s）",
		s.relayInfo.UserId,
		logger.FormatQuota(s.tokenConsumed),
	))

	// Keep the in-memory terminal state and its durable component markers
	// atomic from callers' perspective. The quota update is intentionally
	// performed while this lock is held so NeedsRefund cannot observe a
	// restored quota before the session has recorded the matching marker.
	s.mu.Lock()
	defer func() {
		s.refundInFlight = false
		s.mu.Unlock()
	}()

	var tokenErr error
	refundTokenQuota := s.tokenConsumed
	var tokenOperation *model.BillingOperation
	if s.operationKey != "" {
		tokenOperation, tokenErr = model.GetBillingOperation(s.operationKey)
		if tokenErr == nil && tokenOperation.RefundTokenApplied {
			// The token mutation and its durable marker are committed together.
			// Restore the in-memory terminal flags before evaluating the rest of
			// the refund so a restarted/retried session cannot remain pending.
			s.tokenRefunded = true
			s.tokenRefundMarked = true
		}
		if tokenErr == nil && tokenOperation.TokenReservedQuota > refundTokenQuota {
			refundTokenQuota = tokenOperation.TokenReservedQuota
		}
	}
	tokenRefunded := s.tokenRefunded || (tokenOperation != nil && tokenOperation.RefundTokenApplied) || refundTokenQuota <= 0 || s.relayInfo.IsPlayground
	if tokenErr == nil && !tokenRefunded {
		if s.operationKey != "" {
			tokenErr = model.ApplyBillingOperationRefundToken(
				s.operationKey, s.relayInfo.TokenId, s.relayInfo.TokenKey,
				refundTokenQuota, s.relayInfo.TokenUnlimited,
			)
		} else {
			tokenErr = model.IncreaseTokenQuotaImmediate(s.relayInfo.TokenId, s.relayInfo.TokenKey, refundTokenQuota)
		}
		if tokenErr == nil {
			s.tokenRefunded = true
		} else {
			common.SysLog("error refunding token quota: " + tokenErr.Error())
		}
	}
	// A marker failure must be retried independently from the already successful
	// token adjustment.
	tokenRefunded = s.tokenRefunded || (tokenOperation != nil && tokenOperation.RefundTokenApplied) || s.tokenConsumed <= 0 || s.relayInfo.IsPlayground
	if tokenErr == nil && tokenRefunded {
		if err := s.markOperationRefundComponent(model.BillingComponentToken); err != nil {
			tokenErr = err
		} else {
			s.tokenRefundMarked = true
		}
	}

	var statsErr error
	if s.operationKey == "" {
		s.statsRefundMarked = true
	} else if !s.statsRefundMarked {
		operation, getErr := model.GetBillingOperation(s.operationKey)
		if getErr != nil {
			statsErr = getErr
		} else if operation.StatsApplied {
			statsErr = model.ApplyBillingOperationRefundStats(
				s.operationKey, s.relayInfo.UserId, s.relayInfo.ChannelId, operation.StatsQuota,
			)
		} else {
			statsErr = model.MarkBillingOperationRefundComponent(s.operationKey, model.BillingComponentStats)
		}
		if statsErr == nil {
			s.statsRefundMarked = true
		}
	}
	var logErr error
	if s.operationKey == "" {
		s.logRefundMarked = true
	} else if !s.logRefundMarked {
		logErr = model.MarkBillingOperationRefundComponent(s.operationKey, model.BillingComponentLog)
		if logErr == nil {
			s.logRefundMarked = true
		}
	}

	var refundErr error
	if tokenErr != nil {
		refundErr = errors.Join(refundErr, tokenErr)
	}
	if statsErr != nil {
		refundErr = errors.Join(refundErr, statsErr)
	}
	if logErr != nil {
		refundErr = errors.Join(refundErr, logErr)
	}
	if refundErr == nil &&
		(s.tokenRefunded || s.tokenConsumed <= 0 || s.relayInfo.IsPlayground) && s.tokenRefundMarked &&
		s.statsRefundMarked && s.logRefundMarked {
		if s.operationKey == "" {
			s.refunded = true
		} else {
			updated, err := model.UpdateBillingOperationStatus(s.operationKey,
				[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
				model.BillingOperationRefunded, "", common.GetTimestamp())
			if err != nil {
				common.SysLog("failed to mark billing operation refunded: " + err.Error())
				refundErr = errors.Join(refundErr, err)
			} else if updated {
				s.refunded = true
			} else {
				operation, getErr := model.GetBillingOperation(s.operationKey)
				if getErr != nil {
					refundErr = errors.Join(refundErr, getErr)
				} else if operation.Status == model.BillingOperationRefunded {
					s.refunded = true
				} else {
					refundErr = errors.Join(refundErr, fmt.Errorf(
						"billing operation %s refund terminal transition rejected in state %s",
						s.operationKey, operation.Status,
					))
				}
			}
		}
	}
	return refundErr
}

// NeedsRefund 返回是否存在需要退还的预扣状态。
func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded || s.tokenSettled {
		// tokenSettled 时令牌差额已提交结算，不能再退预扣额度
		return false
	}
	if s.tokenConsumed > 0 && (!s.tokenRefunded || !s.tokenRefundMarked) {
		return true
	}
	if s.operationKey != "" && (!s.tokenRefundMarked || !s.statsRefundMarked || !s.logRefundMarked) {
		return true
	}
	return false
}

// GetPreConsumedQuota 返回实际预扣的额度。
func (s *BillingSession) GetPreConsumedQuota() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preConsumedQuota
}

// OperationKey returns the durable outbox key associated with this request.
// Task submission persists the same key so polling can resume accounting after
// the originating process has exited.
func (s *BillingSession) OperationKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.operationKey
}

func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.settled || s.refunded || targetQuota <= s.preConsumedQuota {
		return nil
	}

	delta := targetQuota - s.preConsumedQuota
	if delta <= 0 {
		return nil
	}

	var tokenErr error
	tokenReserved := false
	if s.operationKey != "" && !s.relayInfo.IsPlayground && s.relayInfo.TokenId > 0 {
		tokenErr = model.ReserveBillingOperationToken(s.operationKey, s.relayInfo.TokenId, s.relayInfo.TokenKey, targetQuota, s.relayInfo.TokenUnlimited)
		tokenReserved = tokenErr == nil
	} else if s.operationKey != "" {
		tokenErr = model.MarkBillingOperationTokenReservationSkipped(s.operationKey)
	} else {
		tokenErr = s.reserveToken(delta)
		tokenReserved = tokenErr == nil && !s.relayInfo.IsPlayground && s.relayInfo.TokenId > 0
	}
	if tokenErr != nil {
		return tokenErr
	}

	s.preConsumedQuota += delta
	if tokenReserved {
		s.tokenConsumed += delta
	}
	s.extraReserved += delta
	s.syncRelayInfo()
	return nil
}

// ---------------------------------------------------------------------------
// PreConsume — 统一预扣 Token 访问额度
// ---------------------------------------------------------------------------

// preConsume executes the Token quota reservation.
func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := quota

	if effectiveQuota > 0 {
		logger.LogInfo(c, fmt.Sprintf("用户 %d 需要预扣 Token 额度 %s", s.relayInfo.UserId, logger.FormatQuota(effectiveQuota)))
	}

	// ---- 1) 预扣令牌额度 ----
	tokenReserved := false
	if effectiveQuota > 0 {
		var tokenErr error
		if s.operationKey != "" && !s.relayInfo.IsPlayground && s.relayInfo.TokenId > 0 {
			tokenErr = model.ReserveBillingOperationToken(s.operationKey, s.relayInfo.TokenId, s.relayInfo.TokenKey, effectiveQuota, s.relayInfo.TokenUnlimited)
			tokenReserved = tokenErr == nil
		} else if s.operationKey != "" {
			tokenErr = model.MarkBillingOperationTokenReservationSkipped(s.operationKey)
		} else {
			tokenErr = PreConsumeTokenQuota(s.relayInfo, effectiveQuota)
			tokenReserved = tokenErr == nil && !s.relayInfo.IsPlayground && s.relayInfo.TokenId > 0
		}
		if tokenErr != nil {
			return types.NewErrorWithStatusCode(tokenErr, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if tokenReserved {
			s.tokenConsumed = effectiveQuota
		}
	} else if s.operationKey != "" {
		if err := model.MarkBillingOperationTokenReservationSkipped(s.operationKey); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
	}

	s.preConsumedQuota = effectiveQuota
	if s.operationKey != "" {
		if err := model.UpdateBillingOperationReservedQuota(s.operationKey, effectiveQuota); err != nil {
			// The durable operation already owns any token reservation made above.
			// Leave that reservation in place and let abortPreConsume issue the
			// operation-keyed refund exactly once. Rolling it back here would leave
			// TokenReservedQuota set and make the abort path refund it a second time.
			s.preConsumedQuota = 0
			s.syncRelayInfo()
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
	}

	// ---- 同步 RelayInfo 兼容字段 ----
	s.syncRelayInfo()

	return nil
}

// rollbackToken undoes a synchronous token reservation. It only mutates the
// in-memory counter after the database update succeeds, so a failed rollback
// remains visible to the caller and can be retried by the normal refund path.
func (s *BillingSession) rollbackToken(amount int) error {
	if amount <= 0 || s.relayInfo.IsPlayground || s.tokenConsumed <= 0 {
		return nil
	}
	if amount > s.tokenConsumed {
		amount = s.tokenConsumed
	}
	if err := model.IncreaseTokenQuotaImmediate(s.relayInfo.TokenId, s.relayInfo.TokenKey, amount); err != nil {
		return err
	}
	s.tokenConsumed -= amount
	if s.tokenConsumed < 0 {
		s.tokenConsumed = 0
	}
	return nil
}

// abortPreConsume records a failed pre-consume without manufacturing a
// charge. If the synchronous rollback already succeeded, the operation can be
// closed as a zero-quota refund. If rollback failed, retain the original
// reservation and leave a refund_pending operation for reconciliation.
func (s *BillingSession) abortPreConsume(apiErr *types.NewAPIError) {
	if s.operationKey == "" {
		return
	}
	operation, err := model.GetBillingOperation(s.operationKey)
	if err != nil {
		common.SysLog("failed to load billing operation after pre-consume error: " + err.Error())
		return
	}
	if operation.Status == model.BillingOperationReserved || operation.Status == model.BillingOperationApplying {
		transitioned, transitionErr := model.UpdateBillingOperationStatus(s.operationKey,
			[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying},
			model.BillingOperationRefundPending, apiErr.Error(), common.GetTimestamp())
		if transitionErr != nil {
			common.SysLog("failed to preserve pre-consume refund operation: " + transitionErr.Error())
			return
		}
		if !transitioned {
			current, getErr := model.GetBillingOperation(s.operationKey)
			if getErr != nil {
				common.SysLog("failed to reload billing operation after refund transition rejection: " + getErr.Error())
				return
			}
			if current.Status != model.BillingOperationRefundPending {
				common.SysLog(fmt.Sprintf("pre-consume refund transition rejected in state %s", current.Status))
				return
			}
			operation = current
		}
	}
	// Statistics and log components were never applied before a successful
	// pre-consume. Mark those no-op refunds immediately. A token reservation that
	// survived the synchronous rollback is retried here; if it still fails, the
	// durable operation remains pending for the worker.
	for _, component := range []string{model.BillingComponentStats, model.BillingComponentLog} {
		if err := model.MarkBillingOperationRefundComponent(s.operationKey, component); err != nil {
			common.SysLog("failed to mark pre-consume refund component: " + err.Error())
			return
		}
	}
	refundTokenQuota := s.tokenConsumed
	if operation.TokenReservedQuota > refundTokenQuota {
		refundTokenQuota = operation.TokenReservedQuota
	}
	if refundTokenQuota == 0 || s.relayInfo.IsPlayground {
		if err := model.MarkBillingOperationRefundComponent(s.operationKey, model.BillingComponentToken); err != nil {
			common.SysLog("failed to mark zero token pre-consume refund: " + err.Error())
			return
		}
	} else if err := model.ApplyBillingOperationRefundToken(
		s.operationKey, s.relayInfo.TokenId, s.relayInfo.TokenKey,
		refundTokenQuota, s.relayInfo.TokenUnlimited,
	); err == nil {
		s.tokenConsumed = 0
	} else {
		common.SysLog("failed to refund token after pre-consume error: " + err.Error())
		return
	}
	if operation, err = model.GetBillingOperation(s.operationKey); err == nil &&
		operation.RefundTokenApplied && operation.RefundStatsApplied && operation.RefundLogApplied {
		updated, transitionErr := model.UpdateBillingOperationStatus(s.operationKey,
			[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
			model.BillingOperationRefunded, apiErr.Error(), common.GetTimestamp())
		if transitionErr != nil {
			common.SysLog("failed to finalize pre-consume refund operation: " + transitionErr.Error())
			return
		}
		if !updated {
			current, getErr := model.GetBillingOperation(s.operationKey)
			if getErr != nil || current.Status != model.BillingOperationRefunded {
				common.SysLog(fmt.Sprintf("pre-consume refund terminal transition rejected: operation=%s error=%v", s.operationKey, getErr))
			}
		}
	}
}

func (s *BillingSession) reserveToken(delta int) error {
	if delta <= 0 || s.relayInfo.IsPlayground {
		return nil
	}
	if err := PreConsumeTokenQuota(s.relayInfo, delta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return nil
}

// syncRelayInfo 将 BillingSession 的用量状态同步到 RelayInfo 的兼容字段上。
func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	// Keep the diagnostic marker on RelayInfo for log consumers; it is not a
	// separate billing component or a user balance source.
	info.BillingSource = relaycommon.BillingSourceUsage
}

// NewBillingSession 创建个人版用量计费会话。
func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if relayInfo.UserId <= 0 {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("invalid user id: %d", relayInfo.UserId),
			types.ErrorCodeModelPriceError, http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}

	session := &BillingSession{
		relayInfo: relayInfo,
	}
	requestID := ""
	if c != nil {
		requestID = c.GetString(common.RequestIdKey)
	}
	channelID := 0
	if relayInfo.ChannelMeta != nil {
		channelID = relayInfo.ChannelMeta.ChannelId
	}
	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     model.BillingOperationKeyForRequest(requestID),
		RequestID:        requestID,
		UserID:           relayInfo.UserId,
		TokenID:          relayInfo.TokenId,
		ChannelID:        channelID,
		PreConsumedQuota: preConsumedQuota,
	})
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	session.operationKey = operation.OperationKey
	if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
		// The operation is created before token pre-consumption so a failure can be
		// recovered even though the request never reaches the
		// normal relay defer. Any token reservation that could not be rolled back
		// remains visible to the billing worker as refund_pending.
		session.abortPreConsume(apiErr)
		return nil, apiErr
	}
	return session, nil
}
