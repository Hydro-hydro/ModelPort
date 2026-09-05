package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createReserveTestToken(t *testing.T, remainQuota int) Token {
	t.Helper()
	token := Token{
		UserId:      1,
		Key:         "reserve-token-" + common.GetRandomString(8),
		Name:        "reserve-test",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: remainQuota,
	}
	require.NoError(t, token.Insert())
	return token
}

func getTokenFromDB(t *testing.T, id int) Token {
	t.Helper()
	var token Token
	require.NoError(t, DB.First(&token, id).Error)
	return token
}

func resetBatchUpdateTestState(t *testing.T) {
	t.Helper()
	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		for i := 0; i < BatchUpdateTypeCount; i++ {
			batchUpdateLocks[i].Lock()
			batchUpdateStores[i] = make(map[int]int)
			batchUpdateLocks[i].Unlock()
		}
	})
}

func TestTryReserveTokenQuotaWithoutRedis(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	token := createReserveTestToken(t, 80)
	reserved, err := TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Equal(t, 55, reloaded.RemainQuota)
	assert.Equal(t, 25, reloaded.UsedQuota)

	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 56, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 55, getTokenFromDB(t, token.Id).RemainQuota)
}

func TestTokenQuotaAdjustmentsNeverUnderflow(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	token := createReserveTestToken(t, 10)
	assert.ErrorIs(t, DecreaseTokenQuota(token.Id, token.Key, 11), ErrTokenQuotaInsufficient)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Equal(t, 10, reloaded.RemainQuota)
	assert.Zero(t, reloaded.UsedQuota)

	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 7))
	reloaded = getTokenFromDB(t, token.Id)
	assert.Equal(t, 3, reloaded.RemainQuota)
	assert.Equal(t, 7, reloaded.UsedQuota)

	// A task can have a missing used_quota snapshot. Refund still
	// restores remain_quota, while the accounting counter is clamped at zero.
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Update("used_quota", 0).Error)
	require.NoError(t, IncreaseTokenQuota(token.Id, token.Key, 7))
	reloaded = getTokenFromDB(t, token.Id)
	assert.Equal(t, 10, reloaded.RemainQuota)
	assert.Zero(t, reloaded.UsedQuota)
}

func TestUnlimitedTokenTracksUsageWithoutChangingRemainQuota(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	token := createReserveTestToken(t, 0)
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Update("unlimited_quota", true).Error)
	reserved, err := TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	require.True(t, reserved)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Zero(t, reloaded.RemainQuota)
	assert.Equal(t, 25, reloaded.UsedQuota)

	require.NoError(t, IncreaseTokenQuota(token.Id, token.Key, 10))
	reloaded = getTokenFromDB(t, token.Id)
	assert.Zero(t, reloaded.RemainQuota)
	assert.Equal(t, 15, reloaded.UsedQuota)
}

func TestUnlimitedTokenCacheTracksUsageWithoutChangingRemainQuota(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)

	token := createReserveTestToken(t, 0)
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Update("unlimited_quota", true).Error)
	_, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)

	reserved, err := TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	require.True(t, reserved)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Zero(t, reloaded.RemainQuota)
	assert.Equal(t, 25, reloaded.UsedQuota)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Zero(t, cached.RemainQuota)
	assert.Equal(t, 25, cached.UsedQuota)

	require.NoError(t, IncreaseTokenQuotaImmediate(token.Id, token.Key, 25))
	reloaded = getTokenFromDB(t, token.Id)
	assert.Zero(t, reloaded.RemainQuota)
	assert.Zero(t, reloaded.UsedQuota)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Zero(t, cached.RemainQuota)
	assert.Zero(t, cached.UsedQuota)
}

func TestTokenCacheInitPreservesLiveQuotaAndFenceBlocksStaleSnapshot(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)

	token := createReserveTestToken(t, 100)
	loaded, err := GetTokenByKey(token.Key, true)
	require.NoError(t, err)
	stale := *loaded

	result, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, -70)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)

	// 已存在的哈希只刷新 TTL：数据库快照不得覆盖已被原子预扣的余额。
	code, err := cacheInitToken(stale)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 30, cached.RemainQuota)
	assert.Equal(t, 70, cached.UsedQuota, "a cached charge must increase used_quota")

	result, err = cacheApplyTokenQuotaDelta(token.Id, token.Key, 20)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 50, cached.RemainQuota)
	assert.Equal(t, 50, cached.UsedQuota, "a refund must decrease used_quota")

	// 变更期间：fence 删除缓存并拦截并发读者手中的过期快照。
	require.NoError(t, invalidateTokenCacheForMutation(token.Key))
	code, err = cacheInitToken(stale)
	require.NoError(t, err)
	assert.Zero(t, code, "the pre-mutation snapshot must not be published while fenced")
	_, err = cacheGetTokenByKey(token.Key)
	assert.Error(t, err)

	// fence 过期后可重新从数据库水合。
	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	fresh, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, 100, fresh.RemainQuota)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 100, cached.RemainQuota)
}
