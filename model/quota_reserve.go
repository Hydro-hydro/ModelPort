package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type cacheQuotaResult int

const (
	cacheQuotaInsufficient cacheQuotaResult = iota
	cacheQuotaOK
	cacheQuotaMiss
)

const tokenQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local remain = tonumber(redis.call('HGET', KEYS[1], 'RemainQuota'))
if remain == nil or remain < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', -tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

const tokenQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local delta = tonumber(ARGV[1])
local remain = tonumber(redis.call('HGET', KEYS[1], 'RemainQuota'))
local used = tonumber(redis.call('HGET', KEYS[1], 'UsedQuota'))
if remain == nil or used == nil then
  return -1
end
if delta < 0 then
  local unlimited = redis.call('HGET', KEYS[1], 'UnlimitedQuota')
  if unlimited ~= 'true' and unlimited ~= '1' and remain < -delta then
    return 0
  end
  -- A negative delta is a charge: increase the used counter by the
  -- requested amount, while the remain-quota guard above prevents overdraft.
  redis.call('HINCRBY', KEYS[1], 'RemainQuota', delta)
  redis.call('HINCRBY', KEYS[1], 'UsedQuota', -delta)
else
  -- A positive delta is a refund. Decrease used_quota only by the amount
  -- that was actually recorded, preventing a duplicate refund from underflow.
  local refundUsed = used
  if refundUsed < 0 then
    refundUsed = 0
  end
  if refundUsed > delta then
    refundUsed = delta
  end
  redis.call('HINCRBY', KEYS[1], 'RemainQuota', delta)
  redis.call('HINCRBY', KEYS[1], 'UsedQuota', -refundUsed)
end
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

func quotaResultFromLua(result int, err error) (cacheQuotaResult, error) {
	if err != nil {
		return cacheQuotaMiss, err
	}
	switch result {
	case 1:
		return cacheQuotaOK, nil
	case 0:
		return cacheQuotaInsufficient, nil
	default:
		return cacheQuotaMiss, nil
	}
}

func cacheTryReserveTokenQuota(id int, key string, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaReserveScript,
		[]string{getTokenCacheKey(key)}, amount, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyTokenQuotaDelta(id int, key string, delta int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaDeltaScript,
		[]string{getTokenCacheKey(key)}, delta, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func persistTokenQuotaDelta(id int, delta int) error {
	// Token access limits are part of the billing/authorization boundary. They
	// must be durable before the caller reports success, even when the optional
	// aggregate batch updater is enabled.
	return persistTokenQuotaDeltaImmediate(id, delta)
}

// persistTokenQuotaDeltaImmediate is used by request/task billing paths. A
// quota change is part of the durable billing operation and must not wait in
// the process-local batch accumulator, which can be lost on shutdown.
func persistTokenQuotaDeltaImmediate(id int, delta int) error {
	query := DB.Model(&Token{}).Where("id = ?", id)
	if delta < 0 {
		amount := -delta
		query = query.Where("unlimited_quota = ? OR remain_quota >= ?", true, amount)
	}
	updates := map[string]interface{}{
		"remain_quota":  gorm.Expr("remain_quota + ?", delta),
		"used_quota":    gorm.Expr("used_quota - ?", delta),
		"accessed_time": common.GetTimestamp(),
	}
	if delta > 0 {
		// Refund the requested remain quota even when an old task snapshot has
		// no matching used_quota entry, but never let the accounting counter go
		// below zero.
		updates["used_quota"] = gorm.Expr("CASE WHEN used_quota >= ? THEN used_quota - ? ELSE 0 END", delta, delta)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		var token Token
		if err := DB.Unscoped().Select("id", "deleted_at").First(&token, id).Error; err != nil {
			return err
		}
		if token.DeletedAt.Valid {
			return gorm.ErrRecordNotFound
		}
		return ErrTokenQuotaInsufficient
	}
	return nil
}

func reserveTokenQuotaDB(id int, quota int) (bool, error) {
	result := DB.Model(&Token{}).
		Where("id = ? AND remain_quota >= ?", id, quota).
		Updates(map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// TryReserveTokenQuota atomically checks and deducts a token quota. Unlimited
// tokens skip the remaining-quota check but still update remain/used accounting.
func TryReserveTokenQuota(id int, key string, quota int, unlimited bool) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return true, nil
	}
	if unlimited {
		return true, DecreaseTokenQuotaImmediate(id, key, quota)
	}
	if !common.RedisEnabled {
		return reserveTokenQuotaDB(id, quota)
	}

	result, err := cacheTryReserveTokenQuota(id, key, int64(quota))
	if err == nil && result == cacheQuotaMiss {
		if _, hydrateErr := GetTokenByKey(key, true); hydrateErr == nil {
			result, err = cacheTryReserveTokenQuota(id, key, int64(quota))
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if err != nil {
			common.SysLog("token quota cache reserve unavailable, falling back to database: " + err.Error())
		}
		return reserveTokenQuotaDB(id, quota)
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err = persistTokenQuotaDeltaImmediate(id, -quota); err != nil {
		compensated, compensateErr := cacheApplyTokenQuotaDelta(id, key, int64(quota))
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(fmt.Sprintf("failed to compensate reserved token quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}

// IncreaseTokenQuotaImmediate applies a refund synchronously. It mirrors
// IncreaseTokenQuota's Redis fallback but deliberately bypasses the optional
// process-local batch queue so a billing operation can be retried after a
// process restart without losing the adjustment.
func IncreaseTokenQuotaImmediate(id int, key string, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return nil
	}
	if common.RedisEnabled {
		result, cacheErr := cacheApplyTokenQuotaDelta(id, key, int64(quota))
		if cacheErr == nil && result == cacheQuotaMiss {
			if _, hydrateErr := GetTokenByKey(key, true); hydrateErr == nil {
				result, cacheErr = cacheApplyTokenQuotaDelta(id, key, int64(quota))
			}
		}
		if cacheErr == nil && result == cacheQuotaInsufficient {
			return ErrTokenQuotaInsufficient
		}
		if cacheErr == nil && result == cacheQuotaOK {
			if err := persistTokenQuotaDeltaImmediate(id, quota); err == nil {
				return nil
			} else {
				if compensated, compensateErr := cacheApplyTokenQuotaDelta(id, key, -int64(quota)); compensateErr != nil || compensated != cacheQuotaOK {
					common.SysError(fmt.Sprintf("failed to compensate immediate token refund: result=%d error=%v", compensated, compensateErr))
				}
				return err
			}
		}
		if cacheErr != nil {
			common.SysLog("token quota cache refund unavailable, falling back to database: " + cacheErr.Error())
		}
	}
	return increaseTokenQuota(id, quota)
}

// DecreaseTokenQuotaImmediate applies a charge synchronously, bypassing the
// process-local batch queue. It is the counterpart used by billing sessions
// and asynchronous task settlement.
func DecreaseTokenQuotaImmediate(id int, key string, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return nil
	}
	if common.RedisEnabled {
		result, cacheErr := cacheApplyTokenQuotaDelta(id, key, -int64(quota))
		if cacheErr == nil && result == cacheQuotaMiss {
			if _, hydrateErr := GetTokenByKey(key, true); hydrateErr == nil {
				result, cacheErr = cacheApplyTokenQuotaDelta(id, key, -int64(quota))
			}
		}
		if cacheErr == nil && result == cacheQuotaInsufficient {
			return ErrTokenQuotaInsufficient
		}
		if cacheErr == nil && result == cacheQuotaOK {
			if err := persistTokenQuotaDeltaImmediate(id, -quota); err == nil {
				return nil
			} else {
				if compensated, compensateErr := cacheApplyTokenQuotaDelta(id, key, int64(quota)); compensateErr != nil || compensated != cacheQuotaOK {
					common.SysError(fmt.Sprintf("failed to compensate immediate token charge: result=%d error=%v", compensated, compensateErr))
				}
				return err
			}
		}
		if cacheErr != nil {
			common.SysLog("token quota cache charge unavailable, falling back to database: " + cacheErr.Error())
		}
	}
	return decreaseTokenQuota(id, quota)
}
