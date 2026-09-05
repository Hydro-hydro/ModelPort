package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// midjourneyPollSummary is the result recorded on a midjourney_poll system task
// row, summarizing one polling pass.
type midjourneyPollSummary struct {
	UnfinishedTasks int `json:"unfinished_tasks"`
	ChannelsScanned int `json:"channels_scanned"`
	NullTasksFailed int `json:"null_tasks_failed"`
}

// runMidjourneyTaskUpdateOnce performs one Midjourney polling pass synchronously.
// It honors ctx cancellation (the system-task runner cancels it when the lease
// is lost) and, when report is non-nil, reports progress as (processedChannels,
// totalChannels) so the system task surfaces a percentage.
func runMidjourneyTaskUpdateOnce(ctx context.Context, report func(processed, total int)) midjourneyPollSummary {
	summary := midjourneyPollSummary{}
	if ctx == nil {
		ctx = context.Background()
	}

	tasks := model.GetAllUnFinishTasks()
	if len(tasks) == 0 {
		return summary
	}
	summary.UnfinishedTasks = len(tasks)

	logger.LogInfo(ctx, fmt.Sprintf("检测到未完成的任务数有: %v", len(tasks)))
	taskChannelM := make(map[int][]string)
	taskM := make(map[string]*model.Midjourney)
	nullTasks := make([]*model.Midjourney, 0)
	for _, task := range tasks {
		if task.MjId == "" {
			// A task without an upstream ID cannot be polled. Finalize it one
			// row at a time so a concurrent poller cannot overwrite its state,
			// then refund only if this process won the CAS transition.
			nullTasks = append(nullTasks, task)
			continue
		}
		taskM[task.MjId] = task
		taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], task.MjId)
	}
	for _, task := range nullTasks {
		if ctx.Err() != nil {
			break
		}
		if failMidjourneyTaskWithoutUpstreamID(ctx, task) {
			summary.NullTasksFailed++
		}
	}
	if len(taskChannelM) == 0 {
		return summary
	}

	totalChannels := len(taskChannelM)
	processedChannels := 0
	for channelId, taskIds := range taskChannelM {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedChannels, totalChannels)
		}
		processedChannels++
		summary.ChannelsScanned++
		logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
		if len(taskIds) == 0 {
			continue
		}
		midjourneyChannel, err := model.CacheGetChannel(channelId)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("CacheGetChannel: %v", err))
			// A cache miss/database error is a transient polling failure. Keep
			// every task unfinished so a later leased pass can recover the
			// channel and query the upstream instead of refunding prematurely.
			continue
		}
		baseURL := strings.TrimRight(midjourneyChannel.GetBaseURL(), "/")
		if baseURL == "" {
			// A channel without a usable base URL cannot be queried yet. Keep
			// the task pending so configuration repair or the timeout handler
			// can handle it explicitly instead of panicking the poller.
			logger.LogError(ctx, fmt.Sprintf("Midjourney channel #%d has no base URL", channelId))
			continue
		}
		requestUrl := fmt.Sprintf("%s/mj/task/list-by-condition", baseURL)

		body, err := common.Marshal(map[string]any{
			"ids": taskIds,
		})
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Get Task marshal body error: %v", err))
			continue
		}
		timeout := time.Second * 15
		requestCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(requestCtx, "POST", requestUrl, bytes.NewBuffer(body))
		if err != nil {
			cancel()
			logger.LogError(ctx, fmt.Sprintf("Get Task error: %v", err))
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("mj-api-secret", midjourneyChannel.Key)
		resp, err := service.GetHttpClient().Do(req)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Get Task Do req error: %v", err))
			cancel()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
			resp.Body.Close()
			cancel()
			continue
		}
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Get Mjp Task parse body error: %v", err))
			resp.Body.Close()
			cancel()
			continue
		}
		var responseItems []dto.MidjourneyDto
		err = common.Unmarshal(responseBody, &responseItems)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Get Mjp Task parse body error2: %v, body: %s", err, string(responseBody)))
			resp.Body.Close()
			cancel()
			continue
		}
		resp.Body.Close()
		req.Body.Close()
		cancel()

		for _, responseItem := range responseItems {
			task := taskM[responseItem.MjId]
			if task == nil {
				logger.LogWarn(ctx, fmt.Sprintf("Midjourney task response ignored: unknown mj_id=%s", responseItem.MjId))
				continue
			}

			useTime := (time.Now().UnixNano() / int64(time.Millisecond)) - task.SubmitTime
			// 如果时间超过一小时，且进度不是100%，则认为任务失败
			if useTime > 3600000 && task.Progress != "100%" {
				responseItem.FailReason = "上游任务超时（超过1小时）"
				responseItem.Status = "FAILURE"
			}
			if !checkMjTaskNeedUpdate(task, responseItem) {
				continue
			}
			preStatus := task.Status
			task.Code = 1
			task.Progress = responseItem.Progress
			task.PromptEn = responseItem.PromptEn
			task.State = responseItem.State
			task.SubmitTime = responseItem.SubmitTime
			task.StartTime = responseItem.StartTime
			task.FinishTime = responseItem.FinishTime
			task.ImageUrl = responseItem.ImageUrl
			task.Status = responseItem.Status
			task.FailReason = responseItem.FailReason
			if responseItem.Properties != nil {
				propertiesStr, _ := common.Marshal(responseItem.Properties)
				task.Properties = string(propertiesStr)
			}
			if responseItem.Buttons != nil {
				buttonStr, _ := common.Marshal(responseItem.Buttons)
				task.Buttons = string(buttonStr)
			}
			// 映射 VideoUrl
			task.VideoUrl = responseItem.VideoUrl

			// 映射 VideoUrls - 将数组序列化为 JSON 字符串
			if responseItem.VideoUrls != nil && len(responseItem.VideoUrls) > 0 {
				videoUrlsStr, err := common.Marshal(responseItem.VideoUrls)
				if err != nil {
					logger.LogError(ctx, fmt.Sprintf("序列化 VideoUrls 失败: %v", err))
					task.VideoUrls = "[]" // 失败时设置为空数组
				} else {
					task.VideoUrls = string(videoUrlsStr)
				}
			} else {
				task.VideoUrls = "" // 空值时清空字段
			}

			shouldReturnQuota := false
			if task.Status == "FAILURE" {
				// Some upstreams return a terminal failure without a progress or
				// reason field. Status is authoritative; always close the task and
				// let the CAS winner perform the durable refund.
				task.Progress = "100%"
				shouldReturnQuota = task.Quota != 0
			} else if task.Progress != "100%" && responseItem.FailReason != "" {
				logger.LogInfo(ctx, task.MjId+" 构建失败，"+task.FailReason)
				task.Progress = "100%"
				if task.Quota != 0 {
					shouldReturnQuota = true
				}
			}
			won, err := task.UpdateWithStatus(preStatus)
			if err != nil {
				logger.LogError(ctx, "UpdateMidjourneyTask task error: "+err.Error())
			} else if won && shouldReturnQuota {
				service.RefundMidjourneyQuota(ctx, task, "构图失败")
			}
		}
	}
	if report != nil && (ctx == nil || ctx.Err() == nil) {
		report(totalChannels, totalChannels)
	}
	return summary
}

func failMidjourneyTaskWithoutUpstreamID(ctx context.Context, task *model.Midjourney) bool {
	if task == nil || task.Id == 0 {
		return false
	}
	oldStatus := task.Status
	task.Status = "FAILURE"
	task.Progress = "100%"
	task.FinishTime = time.Now().UnixNano() / int64(time.Millisecond)
	task.FailReason = "上游任务 ID 缺失，无法继续轮询"
	won, err := task.UpdateWithStatus(oldStatus)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("failed to finalize Midjourney task %d without upstream id: %v", task.Id, err))
		return false
	}
	if !won {
		logger.LogInfo(ctx, fmt.Sprintf("Midjourney task %d already transitioned, skip", task.Id))
		return false
	}
	if task.Quota > 0 && !service.RefundMidjourneyQuota(ctx, task, task.FailReason) {
		logger.LogWarn(ctx, fmt.Sprintf("Midjourney task %d waits for refund after missing upstream id", task.Id))
	}
	return true
}

func checkMjTaskNeedUpdate(oldTask *model.Midjourney, newTask dto.MidjourneyDto) bool {
	if oldTask.Code != 1 {
		return true
	}
	if oldTask.Progress != newTask.Progress {
		return true
	}
	if oldTask.PromptEn != newTask.PromptEn {
		return true
	}
	if oldTask.State != newTask.State {
		return true
	}
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.ImageUrl != newTask.ImageUrl {
		return true
	}
	if oldTask.Status != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.Progress != "100%" && newTask.FailReason != "" {
		return true
	}
	// 检查 VideoUrl 是否需要更新
	if oldTask.VideoUrl != newTask.VideoUrl {
		return true
	}
	// 检查 VideoUrls 是否需要更新
	if newTask.VideoUrls != nil && len(newTask.VideoUrls) > 0 {
		newVideoUrlsStr, _ := common.Marshal(newTask.VideoUrls)
		if oldTask.VideoUrls != string(newVideoUrlsStr) {
			return true
		}
	} else if oldTask.VideoUrls != "" {
		// 如果新数据没有 VideoUrls 但旧数据有，需要更新（清空）
		return true
	}

	return false
}

func GetAllMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	// 解析其他查询参数
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllTasks(queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetUserMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	queryParams := model.TaskQueryParams{
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllUserTask(userId, queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
