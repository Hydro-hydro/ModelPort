package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const billingOperationWorkerLease = 60

// Requests without a task row have no independent lifecycle marker. After
// this recovery window, an operation that still has no actual quota is treated
// as abandoned and refunded; the CAS also checks actual_quota_set so a request
// settling concurrently wins over the timeout path.
const billingOperationRequestRecoveryTimeoutSeconds int64 = 30 * 60

// BillingOperationPollSummary is persisted on the system task row for
// operators. It intentionally reports only durable queue progress.
type BillingOperationPollSummary struct {
	Claimed  int `json:"claimed"`
	Settled  int `json:"settled"`
	Refunded int `json:"refunded"`
	Deferred int `json:"deferred"`
}

// RunBillingOperationReconciliationOnce claims due operations and resumes
// task-bound terminal accounting. Request-bound operations are only closed
// when all their component markers are already present; there is no safe way
// to reconstruct an arbitrary request log after a process restart, so such a
// row remains durable and visible for operator/outbox repair.
func RunBillingOperationReconciliationOnce(ctx context.Context) BillingOperationPollSummary {
	if ctx == nil {
		ctx = context.Background()
	}
	summary := BillingOperationPollSummary{}
	now := common.GetTimestamp()
	workerID := fmt.Sprintf("billing-%s", common.GetRandomString(8))
	operations, err := model.ClaimBillingOperations(workerID, now, billingOperationWorkerLease, 100)
	if err != nil {
		common.SysLog("billing operation claim failed: " + err.Error())
		return summary
	}
	summary.Claimed = len(operations)
	for index, operation := range operations {
		if ctx.Err() != nil {
			for _, pending := range operations[index:] {
				if pending != nil {
					_, _ = model.ReleaseBillingOperationLease(pending.OperationKey, workerID)
				}
			}
			summary.Deferred += len(operations) - index
			break
		}
		result := reconcileBillingOperation(ctx, operation, workerID)
		// Task reconciliation may complete the operation through its task CAS
		// helper. Release the claim in either case; terminal rows no longer need
		// a lease, and retryable rows were already released by the owned defer
		// transition.
		if operation != nil {
			_, _ = model.ReleaseBillingOperationLease(operation.OperationKey, workerID)
		}
		summary.Settled += result.settled
		summary.Refunded += result.refunded
		summary.Deferred += result.deferred
	}
	return summary
}

type billingOperationResult struct {
	settled  int
	refunded int
	deferred int
}

func reconcileBillingOperation(ctx context.Context, operation *model.BillingOperation, workerID string) billingOperationResult {
	if operation == nil {
		return billingOperationResult{deferred: 1}
	}
	owned, err := model.BillingOperationLeaseOwned(operation.OperationKey, workerID, common.GetTimestamp())
	if err != nil || !owned {
		return billingOperationResult{deferred: 1}
	}
	if operation.TaskID == "" {
		return reconcileRequestOperation(operation, workerID)
	}

	task, exists, err := model.GetByTaskId(operation.UserID, operation.TaskID)
	if err != nil || !exists || task == nil {
		if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
			// The submit path binds TaskID before inserting the task row, so an
			// insert failure can leave an orphaned operation. Usage-funded orphan
			// operations have no wallet side effect and can be refunded safely.
			if operation.FundingSource == BillingSourceUsage {
				return reconcileOrphanedTaskOperation(operation, workerID)
			}
			return deferBillingOperation(operation, workerID, "task not found")
		}
		return deferBillingOperation(operation, workerID, err.Error())
	}
	// A request can be cancelled after the task row has been inserted but before
	// the originating process finishes its asynchronous refund. In that window
	// the durable operation is already refund_pending while the task may still be
	// queued/in progress. Refund intent takes precedence over the task status so
	// a restarted worker cannot leave the reservation stranded until a provider
	// eventually reports a terminal state.
	if operation.Status == model.BillingOperationRefundPending {
		if !reconcileRequestRefund(operation, workerID) {
			return deferBillingOperation(operation, workerID, "task refund components pending")
		}
		current, reloadErr := model.GetBillingOperation(operation.OperationKey)
		if reloadErr != nil || current == nil {
			return deferBillingOperation(operation, workerID, "task refund state reload failed")
		}
		updated, transitionErr := model.UpdateBillingOperationStatusOwned(
			operation.OperationKey,
			workerID,
			[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
			model.BillingOperationRefunded,
			"",
			common.GetTimestamp(),
			common.GetTimestamp(),
		)
		if transitionErr != nil {
			return deferBillingOperation(current, workerID, "task refund terminal transition failed: "+transitionErr.Error())
		}
		if updated {
			if !clearRefundedTaskQuota(ctx, task) {
				return billingOperationResult{deferred: 1}
			}
			return billingOperationResult{refunded: 1}
		}
		current, reloadErr = model.GetBillingOperation(operation.OperationKey)
		if reloadErr == nil && current != nil && current.Status == model.BillingOperationRefunded {
			if !clearRefundedTaskQuota(ctx, task) {
				return billingOperationResult{deferred: 1}
			}
			return billingOperationResult{refunded: 1}
		}
		if reloadErr != nil || current == nil {
			return deferBillingOperation(operation, workerID, "task refund terminal reload failed")
		}
		return deferBillingOperation(current, workerID, "task refund terminal transition pending")
	}

	switch task.Status {
	case model.TaskStatusFailure:
		if RefundTaskQuotaAfterTerminalOwned(ctx, task, task.FailReason, workerID) {
			return billingOperationResult{refunded: 1}
		}
		return billingOperationResult{deferred: 1}
	case model.TaskStatusSuccess:
		// A process may crash after the task reaches SUCCESS but before the
		// statistics/log components (or their markers) are durable. Replay the
		// same operation-keyed components before attempting the terminal CAS;
		// otherwise a successful task would remain applying forever and could not
		// be recovered by the worker.
		if operation.Status != model.BillingOperationRefundPending &&
			(!operation.FundingApplied || !operation.TokenApplied || !operation.StatsApplied || !operation.LogApplied) {
			if err := ensureTaskBillingLogPayload(operation, task); err != nil || !reconcileRequestComponents(operation, workerID) {
				if err != nil {
					return deferBillingOperation(operation, workerID, "task billing payload recovery failed: "+err.Error())
				}
				return deferBillingOperation(operation, workerID, "task billing components pending")
			}
			key := operation.OperationKey
			reloaded, reloadErr := model.GetBillingOperation(key)
			if reloadErr != nil || reloaded == nil {
				if reloadErr == nil {
					reloadErr = errors.New("billing operation reload returned nil")
				}
				return deferBillingOperation(operation, workerID, "task billing state reload failed: "+reloadErr.Error())
			}
			operation = reloaded
		}
		// ActualQuotaSet is populated at submit time and therefore does not prove
		// that a successful task's provider usage has been reconciled. The polling
		// path marks FinalUsageApplied only after the final token/stat/log writes;
		// until then a worker must leave the operation applying so it cannot swallow
		// a later usage adjustment.
		if operation.Status != model.BillingOperationRefundPending && !operation.FinalUsageApplied {
			// Immediate tasks have no later polling observation. Their task row
			// already contains the provider's terminal result and submit-time
			// amount, so recover the final-usage marker after a crash between the
			// consume log and finalization.
			if task.PrivateData.ImmediateTask {
				actualQuota := task.Quota
				if operation.ActualQuotaSet {
					actualQuota = operation.ActualQuota
				}
				if err := model.MarkBillingOperationFinalUsageOwned(operation.OperationKey, workerID, actualQuota, common.GetTimestamp()); err != nil {
					return deferBillingOperation(operation, workerID, "immediate task final usage recovery failed: "+err.Error())
				}
				reloaded, reloadErr := model.GetBillingOperation(operation.OperationKey)
				if reloadErr != nil || reloaded == nil {
					if reloadErr == nil {
						reloadErr = errors.New("billing operation reload returned nil")
					}
					return deferBillingOperation(operation, workerID, "immediate task final usage reload failed: "+reloadErr.Error())
				}
				operation = reloaded
			}
			// Internal task runners do not have a request session whose final
			// usage marker can be written by the relay handler. A terminal task
			// row is the final observation for these deterministic task keys.
			if strings.HasPrefix(operation.OperationKey, "task:") &&
				(operation.ActualQuotaSet || operation.FundingSource == BillingSourceWallet) {
				actualQuota := task.Quota
				if operation.ActualQuotaSet {
					actualQuota = operation.ActualQuota
				}
				if err := model.MarkBillingOperationFinalUsageOwned(operation.OperationKey, workerID, actualQuota, common.GetTimestamp()); err != nil {
					return deferBillingOperation(operation, workerID, "task final usage recovery failed: "+err.Error())
				}
				reloaded, reloadErr := model.GetBillingOperation(operation.OperationKey)
				if reloadErr != nil || reloaded == nil {
					if reloadErr == nil {
						reloadErr = errors.New("billing operation reload returned nil")
					}
					return deferBillingOperation(operation, workerID, "task final usage reload failed: "+reloadErr.Error())
				}
				operation = reloaded
			}
			// Request-bound asynchronous tasks normally receive their final
			// usage marker from the polling process. If that process exits after
			// the terminal task row and all component writes are durable but before
			// the marker, the task row is the remaining terminal observation. Use
			// the operation's durable actual quota to close the operation; this
			// prevents a successful task from remaining in applying forever after
			// a restart.
			if strings.HasPrefix(operation.OperationKey, "request:") &&
				operation.ActualQuotaSet && task.Quota == operation.ActualQuota &&
				operation.FundingApplied &&
				operation.TokenApplied && operation.StatsApplied && operation.LogApplied {
				if err := model.MarkBillingOperationFinalUsageOwned(
					operation.OperationKey, workerID, operation.ActualQuota, common.GetTimestamp(),
				); err != nil {
					return deferBillingOperation(operation, workerID, "request task final usage recovery failed: "+err.Error())
				}
				reloaded, reloadErr := model.GetBillingOperation(operation.OperationKey)
				if reloadErr != nil || reloaded == nil {
					if reloadErr == nil {
						reloadErr = errors.New("billing operation reload returned nil")
					}
					return deferBillingOperation(operation, workerID, "request task final usage reload failed: "+reloadErr.Error())
				}
				operation = reloaded
			}
		}
		if operation.Status != model.BillingOperationRefundPending && !operation.FinalUsageApplied {
			return deferBillingOperation(operation, workerID, "task final usage adjustment pending")
		}
		actualQuota := task.Quota
		if operation.ActualQuotaSet {
			actualQuota = operation.ActualQuota
		}
		settled, finalizeErr := finalizeTaskBillingOperationOwned(operation.OperationKey, workerID, actualQuota)
		if finalizeErr != nil {
			return deferBillingOperation(operation, workerID, "task settlement failed: "+finalizeErr.Error())
		}
		if settled {
			return billingOperationResult{settled: 1}
		}
		return deferBillingOperation(operation, workerID, "task billing components pending or lease lost")
	default:
		// Submission reserves the estimated task amount before the task row is
		// inserted. If the synchronous settlement then fails, replay the component
		// writes here, but keep the operation applying while the task is queued or
		// in progress. The task's terminal CAS owns the final settlement/refund;
		// settling this row early would make a later provider-usage adjustment
		// indistinguishable from a duplicate charge.
		if operation.Status != model.BillingOperationRefundPending && operation.ActualQuotaSet {
			if reconcilePendingTaskSubmission(operation, task, workerID) {
				return deferBillingOperation(operation, workerID, "task remains nonterminal after submission accounting")
			}
			return deferBillingOperation(operation, workerID, "pending task billing components")
		}
		return deferBillingOperation(operation, workerID, "task is not terminal")
	}
}

func reconcileOrphanedTaskOperation(operation *model.BillingOperation, workerID string) billingOperationResult {
	if operation == nil || strings.TrimSpace(workerID) == "" {
		return billingOperationResult{deferred: 1}
	}
	if operation.Status == model.BillingOperationRefunded {
		return billingOperationResult{refunded: 1}
	}
	if operation.Status == model.BillingOperationSettled || operation.Status == model.BillingOperationFailed {
		return deferBillingOperation(operation, workerID, "orphaned task operation is terminal")
	}
	if operation.Status != model.BillingOperationRefundPending {
		updated, err := model.UpdateBillingOperationStatusOwnedKeepLease(operation.OperationKey, workerID,
			[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying},
			model.BillingOperationRefundPending, "task insert did not persist", common.GetTimestamp(), common.GetTimestamp())
		if err != nil || !updated {
			return deferBillingOperation(operation, workerID, "orphaned task refund transition pending")
		}
	}
	current, err := model.GetBillingOperation(operation.OperationKey)
	if err != nil || current == nil || current.Status != model.BillingOperationRefundPending {
		return deferBillingOperation(operation, workerID, "orphaned task refund reload failed")
	}
	if !reconcileRequestRefund(current, workerID) {
		return deferBillingOperation(current, workerID, "orphaned task refund components pending")
	}
	updated, err := model.UpdateBillingOperationStatusOwned(operation.OperationKey, workerID,
		[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
		model.BillingOperationRefunded, "", common.GetTimestamp(), common.GetTimestamp())
	if err != nil {
		return deferBillingOperation(current, workerID, "orphaned task refund terminal transition failed: "+err.Error())
	}
	if updated {
		return billingOperationResult{refunded: 1}
	}
	current, err = model.GetBillingOperation(operation.OperationKey)
	if err == nil && current != nil && current.Status == model.BillingOperationRefunded {
		return billingOperationResult{refunded: 1}
	}
	if err != nil || current == nil {
		return billingOperationResult{deferred: 1}
	}
	return deferBillingOperation(current, workerID, "orphaned task refund terminal transition pending")
}

// reconcilePendingTaskSubmission completes the charge created at task
// submission while the upstream task is still running. A submission can fail
// after its task row is durable but before all billing components are applied;
// in that window the operation already carries the authoritative actual quota,
// so it is safe to replay the same component idempotency keys.
func reconcilePendingTaskSubmission(operation *model.BillingOperation, task *model.Task, workerID string) bool {
	if operation == nil || task == nil || strings.TrimSpace(workerID) == "" || !operation.ActualQuotaSet {
		return false
	}
	if err := ensureTaskBillingLogPayload(operation, task); err != nil {
		return false
	}
	if !reconcileRequestComponents(operation, workerID) {
		return false
	}
	current, err := model.GetBillingOperation(operation.OperationKey)
	if err != nil || current == nil || current.Status == model.BillingOperationRefundPending {
		return false
	}
	return current.FundingApplied && current.TokenApplied && current.StatsApplied && current.LogApplied
}

// ensureTaskBillingLogPayload supplies a recoverable log payload for the crash
// window between task insertion and LogTaskConsumption. The normal path stores
// a richer payload; this fallback is intentionally derived only from persisted
// task/billing context and keeps the durable operation replayable after restart.
func ensureTaskBillingLogPayload(operation *model.BillingOperation, task *model.Task) error {
	if operation == nil || task == nil {
		return errors.New("task billing operation and task are required")
	}
	if operation.LogPayloadSet {
		return nil
	}
	quota := operation.ActualQuota
	if !operation.ActualQuotaSet {
		quota = task.Quota
	}
	other := taskBillingOther(task)
	other["billing_recovery"] = true
	if task.PrivateData.Execution != nil && task.PrivateData.Execution.RequestPath != "" {
		other["request_path"] = task.PrivateData.Execution.RequestPath
	}
	modelName := taskModelName(task)
	if modelName == "" {
		modelName = task.Properties.OriginModelName
	}
	params := model.RecordConsumeLogParams{
		ChannelId:           task.ChannelId,
		ModelName:           modelName,
		Quota:               quota,
		Content:             fmt.Sprintf("操作 %s", task.Action),
		TokenId:             task.PrivateData.TokenId,
		Group:               task.Group,
		Other:               other,
		BillingOperationKey: operation.OperationKey,
	}
	return model.SetBillingOperationLogPayload(operation.OperationKey, params)
}

// finalizeTaskBillingOperationOwned closes a successful task operation only
// while the reconciliation worker still owns its active lease. Actual quota
// persistence and the terminal status transition are separate guarded CAS
// operations so a worker that loses its lease between them cannot settle a
// row claimed by its successor.
func finalizeTaskBillingOperationOwned(operationKey, workerID string, actualQuota int) (bool, error) {
	if operationKey == "" {
		return false, errors.New("task billing operation key is required")
	}
	if actualQuota < 0 {
		return false, errors.New("task billing actual quota cannot be negative")
	}
	now := common.GetTimestamp()
	owned, err := model.BillingOperationLeaseOwned(operationKey, workerID, now)
	if err != nil {
		return false, err
	}
	if !owned {
		return false, nil
	}
	updated, err := model.UpdateBillingOperationActualQuotaOwned(operationKey, workerID, actualQuota, now)
	if err != nil {
		return false, err
	}
	if !updated {
		// A concurrent worker may have completed the operation after this worker
		// read the lease. Treat an already-settled row as idempotent success; all
		// other states remain for the current owner/retry worker to reconcile.
		operation, getErr := model.GetBillingOperation(operationKey)
		if getErr != nil {
			return false, getErr
		}
		return operation.Status == model.BillingOperationSettled, nil
	}
	operation, err := model.GetBillingOperation(operationKey)
	if err != nil {
		return false, err
	}
	if !operation.FundingApplied || !operation.TokenApplied || !operation.StatsApplied || !operation.LogApplied {
		return false, nil
	}
	if err := model.MarkBillingOperationFinalUsageOwned(operationKey, workerID, actualQuota, now); err != nil {
		return false, err
	}
	// Refresh the lease timestamp immediately before the terminal CAS. The
	// initial ownership check may have happened seconds earlier while the
	// component read was in progress; using that stale timestamp could let an
	// expired worker settle after a successor has reclaimed the row.
	statusNow := common.GetTimestamp()
	settled, err := model.UpdateBillingOperationStatusOwned(operationKey, workerID,
		[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying},
		model.BillingOperationSettled, "", statusNow, statusNow)
	if err != nil {
		return false, err
	}
	if settled {
		return true, nil
	}
	operation, err = model.GetBillingOperation(operationKey)
	if err != nil {
		return false, err
	}
	return operation.Status == model.BillingOperationSettled, nil
}

func reconcileRequestOperation(operation *model.BillingOperation, workerID string) billingOperationResult {
	// A previous attempt may have completed every refund component but crashed
	// before the terminal status CAS. Close that durable intent first; otherwise
	// the no-actual-quota recovery timeout below would keep an already-complete
	// refund pending indefinitely.
	if operation.Status == model.BillingOperationRefundPending &&
		operation.RefundFundingApplied && operation.RefundTokenApplied &&
		operation.RefundStatsApplied && operation.RefundLogApplied {
		updated, err := model.UpdateBillingOperationStatusOwned(operation.OperationKey, workerID,
			[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
			model.BillingOperationRefunded, "", common.GetTimestamp(), common.GetTimestamp())
		if err != nil {
			return deferBillingOperation(operation, workerID, "refund terminal transition failed: "+err.Error())
		}
		if updated {
			return billingOperationResult{refunded: 1}
		}
		current, getErr := model.GetBillingOperation(operation.OperationKey)
		if getErr == nil && current != nil && current.Status == model.BillingOperationRefunded {
			return billingOperationResult{refunded: 1}
		}
		if getErr != nil {
			return deferBillingOperation(operation, workerID, "refund terminal reload failed: "+getErr.Error())
		}
	}
	if operation.ActualQuotaSet == false && operation.TaskID == "" {
		cutoff := common.GetTimestamp() - billingOperationRequestRecoveryTimeoutSeconds
		if operation.CreatedAt > cutoff {
			return deferBillingOperation(operation, workerID, "request billing operation is still active")
		}
		transitioned, err := model.ExpireUnfinishedRequestBillingOperationOwned(
			operation.OperationKey, workerID, cutoff, common.GetTimestamp(),
			"request recovery timeout",
		)
		if err != nil {
			return deferBillingOperation(operation, workerID, "request timeout transition failed: "+err.Error())
		}
		if !transitioned {
			current, getErr := model.GetBillingOperation(operation.OperationKey)
			if getErr != nil {
				return deferBillingOperation(operation, workerID, "request timeout reload failed: "+getErr.Error())
			}
			if current.ActualQuotaSet {
				return reconcileRequestOperation(current, workerID)
			}
			return deferBillingOperation(current, workerID, "request billing operation timeout CAS lost")
		}
		current, err := model.GetBillingOperation(operation.OperationKey)
		if err != nil {
			return deferBillingOperation(operation, workerID, "request timeout state reload failed: "+err.Error())
		}
		if !reconcileRequestRefund(current, workerID) {
			return deferBillingOperation(current, workerID, "request timeout refund components pending")
		}
		updated, err := model.UpdateBillingOperationStatusOwned(operation.OperationKey, workerID,
			[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
			model.BillingOperationRefunded, "", common.GetTimestamp(), common.GetTimestamp())
		if err != nil || !updated {
			return deferBillingOperation(current, workerID, "request timeout refund terminal transition pending")
		}
		return billingOperationResult{refunded: 1}
	}
	if operation.Status == model.BillingOperationRefundPending {
		if !reconcileRequestRefund(operation, workerID) {
			return deferBillingOperation(operation, workerID, "request refund components pending")
		}
		operation, err := model.GetBillingOperation(operation.OperationKey)
		if err != nil {
			return deferBillingOperation(operation, workerID, "request refund reload failed: "+err.Error())
		}
		if updated, err := model.UpdateBillingOperationStatusOwned(operation.OperationKey, workerID,
			[]model.BillingOperationStatus{model.BillingOperationApplying, model.BillingOperationRefundPending},
			model.BillingOperationRefunded, "", common.GetTimestamp(), common.GetTimestamp()); err == nil && updated {
			return billingOperationResult{refunded: 1}
		}
		return deferBillingOperation(operation, workerID, "request refund terminal transition pending")
	}
	if !reconcileRequestComponents(operation, workerID) {
		return deferBillingOperation(operation, workerID, "request billing components pending")
	}
	operation, err := model.GetBillingOperation(operation.OperationKey)
	if err != nil {
		return deferBillingOperation(operation, workerID, "request billing reload failed: "+err.Error())
	}
	complete := operation.FundingApplied && operation.TokenApplied && operation.StatsApplied && operation.LogApplied
	refundComplete := operation.RefundFundingApplied && operation.RefundTokenApplied && operation.RefundStatsApplied && operation.RefundLogApplied
	if operation.Status == model.BillingOperationRefundPending && refundComplete {
		return billingOperationResult{refunded: 1}
	}
	if operation.Status != model.BillingOperationRefundPending && complete {
		if updated, err := model.UpdateBillingOperationStatusOwned(operation.OperationKey, workerID,
			[]model.BillingOperationStatus{model.BillingOperationApplying, model.BillingOperationReserved},
			model.BillingOperationSettled, "", common.GetTimestamp(), common.GetTimestamp()); err == nil && updated {
			return billingOperationResult{settled: 1}
		}
		if current, err := model.GetBillingOperation(operation.OperationKey); err == nil && current.Status == model.BillingOperationSettled {
			return billingOperationResult{settled: 1}
		}
	}
	return deferBillingOperation(operation, workerID, "billing components pending")
}

func reconcileRequestComponents(operation *model.BillingOperation, workerID string) bool {
	if operation == nil || workerID == "" {
		return false
	}
	key := operation.OperationKey
	if !operation.FundingApplied {
		// Wallet operations cannot be safely reconstructed here: the durable
		// operation records only component completion, not the non-idempotent
		// wallet reservation owner. Leave them pending for the owning request
		// path instead of marking a missing wallet mutation as a no-op.
		if operation.FundingSource == BillingSourceWallet ||
			(operation.FundingSource != "" && operation.FundingSource != BillingSourceUsage) {
			return false
		}
		// Personal mode has no wallet mutation; the marker is the durable no-op.
		if err := model.MarkBillingOperationComponentOwned(key, model.BillingComponentFunding, workerID, common.GetTimestamp()); err != nil {
			return false
		}
	}
	if !operation.TokenApplied {
		if !operation.ActualQuotaSet {
			return false
		}
		if operation.TokenID <= 0 {
			// Playground operations have no token row to mutate. The
			// component marker is still required before the durable operation can
			// be settled.
			if err := model.MarkBillingOperationComponentOwned(key, model.BillingComponentToken, workerID, common.GetTimestamp()); err != nil {
				return false
			}
		} else {
			token, err := model.GetTokenById(operation.TokenID)
			if err != nil {
				return false
			}
			delta := operation.ActualQuota - operation.PreConsumedQuota
			if err := model.ApplyBillingOperationTokenOwned(
				key, token.Id, token.Key, delta, token.UnlimitedQuota, workerID, common.GetTimestamp(),
			); err != nil {
				return false
			}
		}
	}
	if !operation.StatsApplied {
		if !operation.ActualQuotaSet {
			return false
		}
		if err := model.ApplyBillingOperationStatsOwned(key, operation.UserID, operation.ChannelID, operation.ActualQuota, true, workerID, common.GetTimestamp()); err != nil {
			return false
		}
	}
	if !operation.LogApplied {
		params, err := model.GetBillingOperationLogPayload(key)
		if err != nil {
			return false
		}
		// The operation key is intentionally omitted from the serialized log
		// payload to keep it out of the user-facing parameter JSON. Restore it
		// before the idempotent log write so a retry cannot create a duplicate.
		params.BillingOperationKey = key
		if err := model.RecordConsumeLogCheckedOwned(nil, operation.UserID, params, workerID); err != nil {
			return false
		}
		if err := model.MarkBillingOperationComponentOwned(key, model.BillingComponentLog, workerID, common.GetTimestamp()); err != nil {
			return false
		}
	}
	return true
}

func reconcileRequestRefund(operation *model.BillingOperation, workerID string) bool {
	if operation == nil || workerID == "" {
		return false
	}
	key := operation.OperationKey
	if !operation.RefundFundingApplied {
		// A wallet refund is a non-idempotent mutation. The reconciliation
		// worker cannot tell whether the original request already restored the
		// wallet, so it must leave the operation pending for the owning request
		// path instead of recording a potentially false completion marker.
		if operation.FundingSource == BillingSourceWallet ||
			(operation.FundingSource != "" && operation.FundingSource != BillingSourceUsage) {
			return false
		}
		if err := model.MarkBillingOperationRefundComponentOwned(key, model.BillingComponentFunding, workerID, common.GetTimestamp()); err != nil {
			return false
		}
	}
	if !operation.RefundTokenApplied {
		// Before settlement, reverse the reservation. After settlement, reverse
		// the final actual charge. Both mutations and their marker are atomic and
		// idempotent, so retries cannot double-restore token quota.
		refundQuota := 0
		if operation.TokenApplied {
			if operation.ActualQuotaSet {
				refundQuota = operation.ActualQuota
			} else {
				refundQuota = operation.TokenReservedQuota
			}
		} else if operation.TokenReserved {
			refundQuota = operation.TokenReservedQuota
		}
		if refundQuota > 0 {
			// A positive reservation without a token identity cannot be safely
			// replayed. Keep the refund pending rather than silently marking the
			// token component complete and losing the quota adjustment.
			if operation.TokenID <= 0 {
				return false
			}
			token, err := model.GetTokenById(operation.TokenID)
			if err != nil {
				return false
			}
			if err := model.ApplyBillingOperationRefundTokenOwned(
				key, token.Id, token.Key, refundQuota, token.UnlimitedQuota,
				workerID, common.GetTimestamp(),
			); err != nil {
				return false
			}
		}
		if err := model.MarkBillingOperationRefundComponentOwned(key, model.BillingComponentToken, workerID, common.GetTimestamp()); err != nil {
			return false
		}
	}
	if !operation.RefundStatsApplied {
		var err error
		if operation.StatsApplied {
			statsQuota := operation.StatsQuota
			if !operation.StatsQuotaSet {
				if operation.ActualQuotaSet {
					statsQuota = operation.ActualQuota
				} else {
					statsQuota = operation.PreConsumedQuota
				}
			}
			err = model.ApplyBillingOperationRefundStatsOwned(key, operation.UserID, operation.ChannelID, statsQuota, workerID, common.GetTimestamp())
		} else {
			err = model.MarkBillingOperationRefundComponentOwned(key, model.BillingComponentStats, workerID, common.GetTimestamp())
		}
		if err != nil {
			return false
		}
	}
	if !operation.RefundLogApplied {
		if err := model.MarkBillingOperationRefundComponentOwned(key, model.BillingComponentLog, workerID, common.GetTimestamp()); err != nil {
			return false
		}
	}
	return true
}

func deferBillingOperation(operation *model.BillingOperation, workerID, reason string) billingOperationResult {
	if operation == nil || strings.TrimSpace(workerID) == "" {
		return billingOperationResult{deferred: 1}
	}
	// The caller's operation snapshot may predate an unowned request-path refund
	// that won the state CAS while this worker was applying components. Reloading
	// and matching the exact current status prevents this retry write from
	// reverting a newly-created refund_pending intent back to applying.
	current, err := model.GetBillingOperation(operation.OperationKey)
	if err == nil && current != nil {
		operation = current
	}
	attempt := operation.AttemptCount
	if attempt < 1 {
		attempt = 1
	}
	backoff := time.Duration(1<<minInt(attempt, 6)) * time.Second
	if _, err := model.UpdateBillingOperationStatusOwned(operation.OperationKey, workerID,
		[]model.BillingOperationStatus{operation.Status},
		operation.Status, reason, common.GetTimestamp()+int64(backoff.Seconds()), common.GetTimestamp()); err != nil {
		common.SysLog("billing operation retry state failed: " + err.Error())
	}
	return billingOperationResult{deferred: 1}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type billingOperationSystemTaskHandler struct{}

func (billingOperationSystemTaskHandler) Type() string { return model.SystemTaskTypeBillingOperation }

func (billingOperationSystemTaskHandler) Enabled() bool {
	return model.HasDueBillingOperations(common.GetTimestamp())
}

func (billingOperationSystemTaskHandler) Interval() time.Duration { return 15 * time.Second }

func (billingOperationSystemTaskHandler) NewPayload() any { return nil }

func (billingOperationSystemTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := RunBillingOperationReconciliationOnce(ctx)
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, summary, ""); err != nil {
		common.SysLog("billing operation system task finish failed: " + err.Error())
	}
}

func init() {
	RegisterSystemTaskHandler(billingOperationSystemTaskHandler{})
}
