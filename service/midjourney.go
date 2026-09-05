package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

func CovertMjpActionToModelName(mjAction string) string {
	modelName := "mj_" + strings.ToLower(mjAction)
	if mjAction == constant.MjActionSwapFace {
		modelName = "swap_face"
	}
	return modelName
}

// PrepareMidjourneyTaskBilling sets the durable refund marker before the task is inserted.
func PrepareMidjourneyTaskBilling(relayInfo *relaycommon.RelayInfo, task *model.Midjourney, quota int, shouldBill bool) (bool, error) {
	if task == nil {
		return false, errors.New("Midjourney task is nil")
	}
	task.Quota = 0
	task.TokenId = 0
	task.BillingChannelId = 0
	task.BillingOperationKey = ""
	if !shouldBill {
		return false, nil
	}
	if relayInfo == nil {
		return false, errors.New("relay info is nil")
	}
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	// Midjourney tasks use the same usage-only funding policy as ordinary
	// relay requests. The marker is kept on RelayInfo for settlement/logging.
	relayInfo.BillingSource = BillingSourceUsage

	task.Quota = quota
	task.BillingChannelId = task.ChannelId
	if relayInfo.ChannelMeta != nil && relayInfo.ChannelId > 0 {
		task.BillingChannelId = relayInfo.ChannelId
	}
	// The request billing session is created before the Midjourney row is
	// inserted. Persist its operation key on the task so a later poller can
	// resume or refund the same durable operation after the request process has
	// exited.
	if provider, ok := relayInfo.Billing.(interface{ OperationKey() string }); ok {
		task.BillingOperationKey = provider.OperationKey()
	}
	return true, nil
}

// SettleMidjourneyTaskBilling settles a persisted Midjourney task and records
// the applied billing stages.
func SettleMidjourneyTaskBilling(relayInfo *relaycommon.RelayInfo, task *model.Midjourney, prepared bool) (bool, error) {
	if !prepared {
		return false, nil
	}
	if relayInfo == nil {
		return false, errors.New("relay info is nil")
	}
	if task == nil || task.Id == 0 {
		return false, errors.New("Midjourney task must be persisted before billing")
	}

	// Midjourney routes reserve token quota before contacting the upstream.
	// Reuse that session here so the successful task does not deduct the token
	// a second time. Direct internal callers without a session use the same
	// atomic helper as the request path.
	if relayInfo.Billing != nil {
		billingErr := relayInfo.Billing.Settle(task.Quota)
		if billingErr != nil {
			task.TokenId = 0
		} else if task.Quota > 0 && !relayInfo.IsPlayground {
			task.TokenId = relayInfo.TokenId
		}
		if updateErr := task.UpdateBillingState(); updateErr != nil {
			return true, errors.Join(billingErr, fmt.Errorf("update Midjourney billing state: %w", updateErr))
		}
		return true, billingErr
	}

	result, billingErr := postConsumeQuotaWithResult(relayInfo, task.Quota, 0, true)
	if !result.FundingApplied {
		task.Quota = 0
		task.TokenId = 0
		task.BillingChannelId = 0
		if updateErr := task.UpdateBillingState(); updateErr != nil {
			return false, errors.Join(billingErr, fmt.Errorf("clear Midjourney billing state: %w", updateErr))
		}
		return false, billingErr
	}

	task.TokenId = 0
	if result.TokenApplied {
		task.TokenId = relayInfo.TokenId
	}
	if updateErr := task.UpdateBillingState(); updateErr != nil {
		return true, errors.Join(billingErr, fmt.Errorf("update Midjourney billing state: %w", updateErr))
	}
	return true, billingErr
}

// RecordMidjourneyTaskConsumption persists the consume log and usage counters
// for a successfully billed Midjourney task through its durable operation.
func RecordMidjourneyTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo, task *model.Midjourney, modelName, tokenName, content, group string, other map[string]interface{}) error {
	if info == nil || task == nil {
		return errors.New("Midjourney billing info and task are required")
	}
	channelID := task.GetBillingChannelId()
	params := model.RecordConsumeLogParams{
		ChannelId: channelID,
		ModelName: modelName,
		TokenName: tokenName,
		Quota:     task.Quota,
		Content:   content,
		TokenId:   task.TokenId,
		Group:     group,
		Other:     other,
	}
	operationKey := strings.TrimSpace(task.BillingOperationKey)
	if operationKey == "" {
		return errors.New("Midjourney task billing operation key is required")
	}

	params.BillingOperationKey = operationKey
	operation, err := model.GetBillingOperation(operationKey)
	if err != nil {
		return err
	}
	if operation.Status == model.BillingOperationRefundPending ||
		operation.Status == model.BillingOperationRefunded ||
		operation.Status == model.BillingOperationFailed {
		return fmt.Errorf("Midjourney billing operation %s is already %s", operationKey, operation.Status)
	}
	if err := model.SetBillingOperationLogPayload(operationKey, params); err != nil {
		return err
	}
	if err := model.ApplyBillingOperationStats(operationKey, info.UserId, channelID, task.Quota, true); err != nil {
		return err
	}
	if err := model.RecordConsumeLogChecked(c, info.UserId, params); err != nil {
		return err
	}
	if err := model.MarkBillingOperationComponent(operationKey, model.BillingComponentLog); err != nil {
		return err
	}
	operation, err = model.GetBillingOperation(operationKey)
	if err != nil {
		return err
	}
	if !operation.FundingApplied || !operation.TokenApplied || !operation.StatsApplied || !operation.LogApplied {
		return nil
	}
	updated, err := model.UpdateBillingOperationStatus(operationKey,
		[]model.BillingOperationStatus{model.BillingOperationReserved, model.BillingOperationApplying},
		model.BillingOperationSettled, "", common.GetTimestamp())
	if err != nil {
		return err
	}
	if updated {
		return nil
	}
	operation, err = model.GetBillingOperation(operationKey)
	if err != nil {
		return err
	}
	if operation.Status != model.BillingOperationSettled {
		return fmt.Errorf("Midjourney billing operation %s was not settled", operationKey)
	}
	return nil
}

// RefundMidjourneyQuota reverses every accounting element recorded for a
// billed Midjourney task. The operation row is the durable idempotency barrier:
// each component is applied at most once and task.Quota is cleared only after
// the operation reaches refunded. This also lets a later poller retry a
// partially completed refund after the originating request has exited.
func RefundMidjourneyQuota(ctx context.Context, task *model.Midjourney, reason string) bool {
	if task == nil {
		return false
	}
	if task.Quota == 0 {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}

	operation, err := ensureMidjourneyBillingOperation(task)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("创建 Midjourney 退款操作失败 task %s: %v", task.MjId, err))
		return false
	}
	key := operation.OperationKey
	unlock := lockTaskRefund(key)
	defer unlock()

	if operation.Status == model.BillingOperationRefunded {
		return clearMidjourneyTaskQuota(task, ctx)
	}
	if operation.Status == model.BillingOperationFailed {
		return false
	}

	quota := task.Quota
	if operation.ActualQuotaSet && operation.ActualQuota > 0 {
		quota = operation.ActualQuota
	}
	if quota <= 0 {
		return false
	}

	if operation.Status != model.BillingOperationRefundPending {
		transitioned, err := model.UpdateBillingOperationStatus(key,
			[]model.BillingOperationStatus{
				model.BillingOperationReserved,
				model.BillingOperationApplying,
				model.BillingOperationSettled,
			}, model.BillingOperationRefundPending, reason, common.GetTimestamp())
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("标记 Midjourney 退款待处理失败 task %s: %v", task.MjId, err))
			return false
		}
		if !transitioned {
			operation, err = model.GetBillingOperation(key)
			if err != nil {
				return false
			}
			if operation.Status == model.BillingOperationRefunded {
				return clearMidjourneyTaskQuota(task, ctx)
			}
			if operation.Status != model.BillingOperationRefundPending {
				return false
			}
		}
	}

	// Midjourney personal billing has no wallet mutation. The funding marker is
	// still required so the durable operation can reach its terminal state.
	operation, err = model.GetBillingOperation(key)
	if err != nil {
		return false
	}
	if !operation.RefundFundingApplied {
		if err := model.MarkBillingOperationRefundComponent(key, model.BillingComponentFunding); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("标记 Midjourney 资金退款完成失败 task %s: %v", task.MjId, err))
			return false
		}
	}

	// ApplyBillingOperationRefundToken performs the token update and its
	// marker in one transaction. The operation row is the only source of truth
	// for the reservation and final charge.
	operation, err = model.GetBillingOperation(key)
	if err != nil {
		return false
	}
	if !operation.RefundTokenApplied {
		tokenID := operation.TokenID
		if tokenID <= 0 {
			tokenID = task.TokenId
		}
		tokenRefundQuota := quota
		if !operation.TokenApplied && operation.TokenReserved && operation.TokenReservedQuota > 0 {
			tokenRefundQuota = operation.TokenReservedQuota
		}
		tokenKey := ""
		if tokenID > 0 {
			tokenKey = resolveTokenKey(ctx, tokenID, task.MjId)
			if tokenKey == "" {
				return false
			}
		}
		if err := model.ApplyBillingOperationRefundToken(key, tokenID, tokenKey, tokenRefundQuota, false); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("退还 Midjourney 令牌额度失败 task %s: %v", task.MjId, err))
			return false
		}
	}

	// ApplyBillingOperationRefundStats is atomic and clamps used_quota at zero.
	operation, err = model.GetBillingOperation(key)
	if err != nil {
		return false
	}
	if !operation.RefundStatsApplied {
		if err := model.ApplyBillingOperationRefundStats(key, task.UserId, task.GetBillingChannelId(), quota); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("回减 Midjourney 统计失败 task %s: %v", task.MjId, err))
			return false
		}
	}

	// Refund logs use the operation key as a unique key in the log database,
	// making retries and process restarts harmless.
	operation, err = model.GetBillingOperation(key)
	if err != nil {
		return false
	}
	if !operation.RefundLogApplied {
		if err := model.RecordTaskBillingLogChecked(model.RecordTaskBillingLogParams{
			UserId:    task.UserId,
			LogType:   model.LogTypeRefund,
			Content:   "",
			ChannelId: task.GetBillingChannelId(),
			ModelName: CovertMjpActionToModelName(task.Action),
			Quota:     quota,
			TokenId:   task.TokenId,
			Other: map[string]interface{}{
				"task_id": task.MjId,
				"reason":  reason,
			},
			BillingOperationKey: key + ":refund",
		}); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("记录 Midjourney 退款日志失败 task %s: %v", task.MjId, err))
			return false
		}
		if err := model.MarkBillingOperationRefundComponent(key, model.BillingComponentLog); err != nil {
			return false
		}
	}

	operation, err = model.GetBillingOperation(key)
	if err != nil || !operation.RefundFundingApplied || !operation.RefundTokenApplied ||
		!operation.RefundStatsApplied || !operation.RefundLogApplied {
		return false
	}
	refunded, err := model.UpdateBillingOperationStatus(key,
		[]model.BillingOperationStatus{model.BillingOperationRefundPending, model.BillingOperationApplying},
		model.BillingOperationRefunded, "", common.GetTimestamp())
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("标记 Midjourney 退款完成失败 task %s: %v", task.MjId, err))
		return false
	}
	if !refunded {
		operation, err = model.GetBillingOperation(key)
		if err != nil || operation.Status != model.BillingOperationRefunded {
			return false
		}
	}
	return clearMidjourneyTaskQuota(task, ctx)
}

// ensureMidjourneyBillingOperation returns the operation associated with a
// task. New requests persist the request session key; internal task runners use
// a deterministic task key so every retry addresses the same operation.
func ensureMidjourneyBillingOperation(task *model.Midjourney) (*model.BillingOperation, error) {
	if task == nil {
		return nil, errors.New("Midjourney task is nil")
	}
	key := strings.TrimSpace(task.BillingOperationKey)
	if key == "" {
		switch {
		case task.Id > 0:
			key = fmt.Sprintf("midjourney:id:%d", task.Id)
		case task.MjId != "":
			key = "midjourney:mj:" + task.MjId
		default:
			return nil, errors.New("Midjourney task has no stable billing key")
		}
		task.BillingOperationKey = key
	}

	operation, err := model.EnsureBillingOperation(model.BillingOperationAttrs{
		OperationKey:     key,
		UserID:           task.UserId,
		TokenID:          task.TokenId,
		ChannelID:        task.GetBillingChannelId(),
		FundingSource:    BillingSourceUsage,
		PreConsumedQuota: task.Quota,
		ActualQuota:      task.Quota,
		ActualQuotaSet:   true,
	})
	if err != nil {
		return nil, err
	}
	// Historical Midjourney rows were persisted after the request had already
	// charged token quota and usage aggregates, but they did not carry a durable
	// operation marker. Reconstruct those component markers once for the
	// deterministic midjourney:* operation so a retry can reverse the original
	// charge exactly once. Request-bound operations are owned by BillingSession
	// and must not be inferred here.
	if strings.HasPrefix(key, "midjourney:") && operation.RequestID == "" {
		updates := map[string]any{"updated_at": common.GetTimestamp()}
		changed := false
		if task.Quota > 0 && !operation.StatsApplied {
			updates["stats_applied"] = true
			updates["stats_quota"] = task.Quota
			updates["stats_quota_set"] = true
			changed = true
		}
		if task.TokenId > 0 && task.Quota > 0 && !operation.TokenReserved && !operation.TokenApplied {
			updates["token_id"] = task.TokenId
			updates["token_reserved"] = true
			updates["token_reserved_quota"] = task.Quota
			changed = true
		}
		if changed {
			if err := model.DB.Model(&model.BillingOperation{}).
				Where("operation_key = ?", key).Updates(updates).Error; err != nil {
				return nil, err
			}
			operation, err = model.GetBillingOperation(key)
			if err != nil {
				return nil, err
			}
		}
	}
	if operation.UserID != 0 && operation.UserID != task.UserId {
		return nil, fmt.Errorf("Midjourney billing operation %s belongs to user %d", key, operation.UserID)
	}
	if operation.TokenID == 0 && task.TokenId > 0 {
		if err := model.DB.Model(&model.BillingOperation{}).
			Where("operation_key = ? AND token_id = ?", key, 0).
			Updates(map[string]any{"token_id": task.TokenId, "updated_at": common.GetTimestamp()}).Error; err != nil {
			return nil, err
		}
		operation.TokenID = task.TokenId
	}
	if task.Id > 0 {
		if err := task.UpdateBillingState(); err != nil {
			return nil, err
		}
	}
	return operation, nil
}

func clearMidjourneyTaskQuota(task *model.Midjourney, ctx context.Context) bool {
	if task == nil || task.Quota == 0 {
		return true
	}
	previousQuota := task.Quota
	task.Quota = 0
	if err := task.UpdateBillingState(); err != nil {
		task.Quota = previousQuota
		logger.LogError(ctx, fmt.Sprintf("Midjourney 退款完成但清除 quota 失败 task %s: %v", task.MjId, err))
		return false
	}
	return true
}

func GetMjRequestModel(relayMode int, midjRequest *dto.MidjourneyRequest) (string, *dto.MidjourneyResponse, bool) {
	action := ""
	if relayMode == relayconstant.RelayModeMidjourneyAction {
		// plus request
		err := CoverPlusActionToNormalAction(midjRequest)
		if err != nil {
			return "", err, false
		}
		action = midjRequest.Action
	} else {
		switch relayMode {
		case relayconstant.RelayModeMidjourneyImagine:
			action = constant.MjActionImagine
		case relayconstant.RelayModeMidjourneyVideo:
			action = constant.MjActionVideo
		case relayconstant.RelayModeMidjourneyEdits:
			action = constant.MjActionEdits
		case relayconstant.RelayModeMidjourneyDescribe:
			action = constant.MjActionDescribe
		case relayconstant.RelayModeMidjourneyBlend:
			action = constant.MjActionBlend
		case relayconstant.RelayModeMidjourneyShorten:
			action = constant.MjActionShorten
		case relayconstant.RelayModeMidjourneyChange:
			action = midjRequest.Action
		case relayconstant.RelayModeMidjourneyModal:
			action = constant.MjActionModal
		case relayconstant.RelayModeSwapFace:
			action = constant.MjActionSwapFace
		case relayconstant.RelayModeMidjourneyUpload:
			action = constant.MjActionUpload
		case relayconstant.RelayModeMidjourneySimpleChange:
			params := ConvertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return "", MidjourneyErrorWrapper(constant.MjRequestError, "invalid_request"), false
			}
			action = params.Action
		case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition, relayconstant.RelayModeMidjourneyNotify:
			return "", nil, true
		default:
			return "", MidjourneyErrorWrapper(constant.MjRequestError, "unknown_relay_action"), false
		}
	}
	modelName := CovertMjpActionToModelName(action)
	return modelName, nil, true
}

func CoverPlusActionToNormalAction(midjRequest *dto.MidjourneyRequest) *dto.MidjourneyResponse {
	// "customId": "MJ::JOB::upsample::2::3dbbd469-36af-4a0f-8f02-df6c579e7011"
	customId := midjRequest.CustomId
	if customId == "" {
		return MidjourneyErrorWrapper(constant.MjRequestError, "custom_id_is_required")
	}
	splits := strings.Split(customId, "::")
	var action string
	if splits[1] == "JOB" {
		action = splits[2]
	} else {
		action = splits[1]
	}

	if action == "" {
		return MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action")
	}
	if strings.Contains(action, "upsample") {
		index, err := strconv.Atoi(splits[3])
		if err != nil {
			return MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
		}
		midjRequest.Index = index
		midjRequest.Action = constant.MjActionUpscale
	} else if strings.Contains(action, "variation") {
		midjRequest.Index = 1
		if action == "variation" {
			index, err := strconv.Atoi(splits[3])
			if err != nil {
				return MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
			}
			midjRequest.Index = index
			midjRequest.Action = constant.MjActionVariation
		} else if action == "low_variation" {
			midjRequest.Action = constant.MjActionLowVariation
		} else if action == "high_variation" {
			midjRequest.Action = constant.MjActionHighVariation
		}
	} else if strings.Contains(action, "pan") {
		midjRequest.Action = constant.MjActionPan
		midjRequest.Index = 1
	} else if strings.Contains(action, "reroll") {
		midjRequest.Action = constant.MjActionReRoll
		midjRequest.Index = 1
	} else if action == "Outpaint" {
		midjRequest.Action = constant.MjActionZoom
		midjRequest.Index = 1
	} else if action == "CustomZoom" {
		midjRequest.Action = constant.MjActionCustomZoom
		midjRequest.Index = 1
	} else if action == "Inpaint" {
		midjRequest.Action = constant.MjActionInPaint
		midjRequest.Index = 1
	} else {
		return MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action:"+customId)
	}
	return nil
}

func ConvertSimpleChangeParams(content string) *dto.MidjourneyRequest {
	split := strings.Split(content, " ")
	if len(split) != 2 {
		return nil
	}

	action := strings.ToLower(split[1])
	changeParams := &dto.MidjourneyRequest{}
	changeParams.TaskId = split[0]

	if action[0] == 'u' {
		changeParams.Action = "UPSCALE"
	} else if action[0] == 'v' {
		changeParams.Action = "VARIATION"
	} else if action == "r" {
		changeParams.Action = "REROLL"
		return changeParams
	} else {
		return nil
	}

	index, err := strconv.Atoi(action[1:2])
	if err != nil || index < 1 || index > 4 {
		return nil
	}
	changeParams.Index = index
	return changeParams
}

func DoMidjourneyHttpRequest(c *gin.Context, timeout time.Duration, fullRequestURL string) (*dto.MidjourneyResponseWithStatusCode, []byte, error) {
	var nullBytes []byte
	//var requestBody io.Reader
	//requestBody = c.Request.Body
	// read request body to json, delete accountFilter and notifyHook
	var mapResult map[string]interface{}
	// if get request, no need to read request body
	if c.Request.Method != "GET" {
		err := json.NewDecoder(c.Request.Body).Decode(&mapResult)
		if err != nil {
			return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_request_body_failed", http.StatusInternalServerError), nullBytes, err
		}
		if !setting.MjAccountFilterEnabled {
			delete(mapResult, "accountFilter")
		}
		if !setting.MjNotifyEnabled {
			delete(mapResult, "notifyHook")
		}
		//req, err := http.NewRequest(c.Request.Method, fullRequestURL, requestBody)
		// make new request with mapResult
	}
	if setting.MjModeClearEnabled {
		if prompt, ok := mapResult["prompt"].(string); ok {
			prompt = strings.Replace(prompt, "--fast", "", -1)
			prompt = strings.Replace(prompt, "--relax", "", -1)
			prompt = strings.Replace(prompt, "--turbo", "", -1)

			mapResult["prompt"] = prompt
		}
	}
	reqBody, err := json.Marshal(mapResult)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "marshal_request_body_failed", http.StatusInternalServerError), nullBytes, err
	}
	req, err := http.NewRequest(c.Request.Method, fullRequestURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "create_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	// 使用带有超时的 context 创建新的请求
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Header.Set("Accept", c.Request.Header.Get("Accept"))
	auth := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if auth != "" {
		auth = strings.TrimPrefix(auth, "Bearer ")
		req.Header.Set("mj-api-secret", auth)
	}
	defer cancel()
	resp, err := GetHttpClient().Do(req)
	if err != nil {
		common.SysLog("do request failed: " + err.Error())
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "do_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	statusCode := resp.StatusCode
	//if statusCode != 200  {
	//	return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "bad_response_status_code", statusCode), nullBytes, nil
	//}
	err = req.Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	err = c.Request.Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	var midjResponse dto.MidjourneyResponse
	var midjourneyUploadsResponse dto.MidjourneyUploadResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_response_body_failed", statusCode), nullBytes, err
	}
	CloseResponseBodyGracefully(resp)
	logger.LogDebug(c, "midjourney response body: %s", responseBody)
	if len(responseBody) == 0 {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "empty_response_body", statusCode), responseBody, nil
	} else {
		err = json.Unmarshal(responseBody, &midjResponse)
		if err != nil {
			err2 := json.Unmarshal(responseBody, &midjourneyUploadsResponse)
			if err2 != nil {
				return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "unmarshal_response_body_failed", statusCode), responseBody, err
			}
		}
	}
	//for k, v := range resp.Header {
	//	c.Writer.Header().Set(k, v[0])
	//}
	return &dto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   midjResponse,
	}, responseBody, nil
}
