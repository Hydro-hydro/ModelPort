package controller

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	personalRelayUserID  = 1001
	personalRelayTokenID = 1001
	personalRelayChannel = 1001
	personalRelayToken   = "sk-personal-relay"
)

func setupPersonalRelayBillingTest(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Ability{},
		&model.Log{},
		&model.BillingOperation{},
	))

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousBatchUpdateEnabled := common.BatchUpdateEnabled
	previousLogConsumeEnabled := common.LogConsumeEnabled
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousCountToken := constant.CountToken
	previousStreamingTimeout := constant.StreamingTimeout
	previousPreConsumedQuota := common.PreConsumedQuota
	previousRetryTimes := common.RetryTimes

	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.MemoryCacheEnabled = true
	constant.CountToken = false
	constant.StreamingTimeout = 30
	common.PreConsumedQuota = 100
	common.RetryTimes = 0

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.BatchUpdateEnabled = previousBatchUpdateEnabled
		common.LogConsumeEnabled = previousLogConsumeEnabled
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		constant.CountToken = previousCountToken
		constant.StreamingTimeout = previousStreamingTimeout
		common.PreConsumedQuota = previousPreConsumedQuota
		common.RetryTimes = previousRetryTimes
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       personalRelayUserID,
		Username: "personal-relay-user",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:             personalRelayTokenID,
		UserId:         personalRelayUserID,
		Key:            personalRelayToken,
		Name:           "personal-relay-token",
		Status:         common.TokenStatusEnabled,
		RemainQuota:    10_000,
		UsedQuota:      0,
		UnlimitedQuota: false,
	}).Error)
}

func newPersonalRelayContext(t *testing.T, upstreamURL string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions",
		strings.NewReader(body),
	).WithContext(context.Background())
	request.Header.Set("Content-Type", "application/json")

	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = request
	common.SetContextKey(c, constant.ContextKeyUserId, personalRelayUserID)
	common.SetContextKey(c, constant.ContextKeyTokenId, personalRelayTokenID)
	common.SetContextKey(c, constant.ContextKeyTokenKey, personalRelayToken)
	common.SetContextKey(c, constant.ContextKeyTokenUnlimited, false)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-4o")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{
		AcceptUnsetRatioModel: true,
	})
	common.SetContextKey(c, constant.ContextKeyChannelId, personalRelayChannel)
	common.SetContextKey(c, constant.ContextKeyChannelName, "personal-relay-channel")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstreamURL)
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, false)
	return c, response
}

func seedPersonalRelayChannel(t *testing.T, upstreamURL string) {
	t.Helper()
	priority := int64(0)
	weight := uint(0)
	autoBan := 0
	channel := &model.Channel{
		Id:       personalRelayChannel,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "upstream-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "personal-relay-channel",
		BaseURL:  &upstreamURL,
		Models:   "gpt-4o",
		Group:    "default",
		Priority: &priority,
		Weight:   &weight,
		AutoBan:  &autoBan,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-4o",
		ChannelId: personalRelayChannel,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
	model.InitChannelCache()
}

type personalRelayCancelWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
	once   sync.Once
	needle []byte
}

func (w *personalRelayCancelWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if bytes.Contains(data, w.needle) {
		w.once.Do(w.cancel)
	}
	return n, err
}

func (w *personalRelayCancelWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func getPersonalRelayQuota(t *testing.T) (int, int) {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").First(&token, personalRelayTokenID).Error)
	return 0, token.RemainQuota
}

func TestPersonalRelayFailureRefundsPreConsumedQuotaExactlyOnce(t *testing.T) {
	setupPersonalRelayBillingTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream failed","type":"upstream_error"}}`))
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	Relay(c, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Eventually(t, func() bool {
		userQuota, tokenQuota := getPersonalRelayQuota(t)
		return userQuota == 0 && tokenQuota == 10_000 && getTokenUsedQuotaForPersonalRelay(t) == 0
	}, time.Second, 10*time.Millisecond)
	userQuota, tokenQuota := getPersonalRelayQuota(t)
	assert.Zero(t, userQuota)
	assert.Equal(t, 10_000, tokenQuota)
	assert.Equal(t, 0, getTokenUsedQuotaForPersonalRelay(t))
}

func TestPersonalRelaySuccessSettlesOnlyOnce(t *testing.T) {
	setupPersonalRelayBillingTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-personal","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	Relay(c, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusOK, response.Code)
	userQuota, tokenQuota := getPersonalRelayQuota(t)
	assert.Zero(t, userQuota)
	assert.Less(t, tokenQuota, 10_000)
	assert.Greater(t, getTokenUsedQuotaForPersonalRelay(t), 0)
	assert.Equal(t, 10_000-tokenQuota, getTokenUsedQuotaForPersonalRelay(t))
	assertPersonalRelayUsageRecorded(t)
}

func TestPersonalRelayResponseDecodeFailureRefundsPreConsumedQuota(t *testing.T) {
	setupPersonalRelayBillingTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "not-json")
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	Relay(c, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Eventually(t, func() bool {
		userQuota, tokenQuota := getPersonalRelayQuota(t)
		return userQuota == 0 && tokenQuota == 10_000 && getTokenUsedQuotaForPersonalRelay(t) == 0
	}, time.Second, 10*time.Millisecond)
}

func TestPersonalRelayRetrySuccessSettlesOnlyFinalAttempt(t *testing.T) {
	setupPersonalRelayBillingTest(t)
	common.RetryTimes = 1

	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"error":{"message":"temporary upstream failure","type":"upstream_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-retry","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	Relay(c, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, int32(2), requests.Load())
	userQuota, tokenQuota := getPersonalRelayQuota(t)
	assert.Zero(t, userQuota)
	assert.Greater(t, getTokenUsedQuotaForPersonalRelay(t), 0)
	assert.Equal(t, 10_000-tokenQuota, getTokenUsedQuotaForPersonalRelay(t))
	assertPersonalRelayUsageRecorded(t)
}

func TestPersonalRelayRetryExhaustionRefundsOnce(t *testing.T) {
	setupPersonalRelayBillingTest(t)
	common.RetryTimes = 1

	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream failed","type":"upstream_error"}}`)
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	Relay(c, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusBadGateway, response.Code)
	assert.Equal(t, int32(2), requests.Load())
	require.Eventually(t, func() bool {
		userQuota, tokenQuota := getPersonalRelayQuota(t)
		return userQuota == 0 && tokenQuota == 10_000 && getTokenUsedQuotaForPersonalRelay(t) == 0
	}, time.Second, 10*time.Millisecond)
}

func TestPersonalRelayZeroUsageSettlesReservationToZero(t *testing.T) {
	setupPersonalRelayBillingTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-zero","object":"chat.completion","model":"gpt-4o","choices":[]}`)
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	Relay(c, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusOK, response.Code)
	userQuota, tokenQuota := getPersonalRelayQuota(t)
	assert.Zero(t, userQuota)
	assert.Equal(t, 10_000, tokenQuota)
	assert.Equal(t, 0, getTokenUsedQuotaForPersonalRelay(t))
}

func TestPersonalRelayTruncatedStreamSettlesReturnedUsageOnce(t *testing.T) {
	setupPersonalRelayBillingTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-truncated\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-truncated\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	Relay(c, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusOK, response.Code)
	userQuota, tokenQuota := getPersonalRelayQuota(t)
	assert.Zero(t, userQuota)
	assert.Greater(t, getTokenUsedQuotaForPersonalRelay(t), 0)
	assert.Equal(t, 10_000-tokenQuota, getTokenUsedQuotaForPersonalRelay(t))
	assertPersonalRelayUsageRecorded(t)
	assert.Contains(t, response.Body.String(), "chatcmpl-truncated")
}

func TestPersonalRelayStreamingSuccessSettlesOnce(t *testing.T) {
	setupPersonalRelayBillingTest(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-stream\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-stream\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	Relay(c, types.RelayFormatOpenAI)

	require.Equal(t, http.StatusOK, response.Code)
	userQuota, tokenQuota := getPersonalRelayQuota(t)
	assert.Zero(t, userQuota)
	assert.Greater(t, getTokenUsedQuotaForPersonalRelay(t), 0)
	assert.Equal(t, 10_000-tokenQuota, getTokenUsedQuotaForPersonalRelay(t))
	assertPersonalRelayUsageRecorded(t)
	assert.Contains(t, response.Body.String(), "chatcmpl-stream")
}

func TestPersonalRelayStreamingTimeoutSettlesZeroUsageOnce(t *testing.T) {
	setupPersonalRelayBillingTest(t)
	constant.StreamingTimeout = 1

	upstreamStarted := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		close(upstreamStarted)
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)

	done := make(chan struct{})
	go func() {
		Relay(c, types.RelayFormatOpenAI)
		close(done)
	}()

	select {
	case <-upstreamStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not start")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not return after stream timeout")
	}

	assert.Equal(t, http.StatusOK, response.Code)
	userQuota, tokenQuota := getPersonalRelayQuota(t)
	assert.Zero(t, userQuota)
	assert.Equal(t, 10_000, tokenQuota)
	assert.Equal(t, 0, getTokenUsedQuotaForPersonalRelay(t))
}

func TestPersonalRelayStreamingClientCancellationKeepsAccountingConsistent(t *testing.T) {
	setupPersonalRelayBillingTest(t)

	upstreamStarted := make(chan struct{})
	upstreamStopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		close(upstreamStarted)
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-cancel\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		// A second event makes the handler flush the first event to the client.
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-cancel\",\"object\":\"chat.completion.chunk\",\"created\":1710000000,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		close(upstreamStopped)
	}))
	defer upstream.Close()
	seedPersonalRelayChannel(t, upstream.URL)

	body := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	c, response := newPersonalRelayContext(t, upstream.URL, body)
	requestContext, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(requestContext)
	c.Writer = &personalRelayCancelWriter{
		ResponseWriter: c.Writer,
		cancel:         cancel,
		needle:         []byte("partial"),
	}

	done := make(chan struct{})
	go func() {
		Relay(c, types.RelayFormatOpenAI)
		close(done)
	}()

	select {
	case <-upstreamStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not start")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not return after client cancellation")
	}
	select {
	case <-upstreamStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}

	assert.Equal(t, http.StatusOK, response.Code)
	userQuota, tokenQuota := getPersonalRelayQuota(t)
	assert.Zero(t, userQuota)
	assert.Greater(t, getTokenUsedQuotaForPersonalRelay(t), 0)
	assert.Equal(t, 10_000-tokenQuota, getTokenUsedQuotaForPersonalRelay(t))
	assertPersonalRelayUsageRecorded(t)
}

func getTokenUsedQuotaForPersonalRelay(t *testing.T) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").First(&token, personalRelayTokenID).Error)
	return token.UsedQuota
}

func assertPersonalRelayUsageRecorded(t *testing.T) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, personalRelayUserID).Error)
	assert.Greater(t, user.UsedQuota, 0)
	assert.Greater(t, user.RequestCount, 0)
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", personalRelayUserID, model.LogTypeConsume).Order("id DESC").First(&log).Error)
	assert.Greater(t, log.Quota, 0)
	assert.NotEmpty(t, log.ModelName)
}
