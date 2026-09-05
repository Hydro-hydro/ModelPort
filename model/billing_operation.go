package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// BillingOperationStatus is the durable state of a usage accounting operation.
// The state is deliberately independent from the request/task state: a task may
// be terminal while its accounting operation is still waiting for retry.
type BillingOperationStatus string

const (
	BillingOperationReserved      BillingOperationStatus = "reserved"
	BillingOperationApplying      BillingOperationStatus = "applying"
	BillingOperationSettled       BillingOperationStatus = "settled"
	BillingOperationRefundPending BillingOperationStatus = "refund_pending"
	BillingOperationRefunded      BillingOperationStatus = "refunded"
	BillingOperationFailed        BillingOperationStatus = "failed"
)

const (
	BillingComponentFunding = "funding"
	BillingComponentToken   = "token"
	BillingComponentStats   = "stats"
	BillingComponentLog     = "log"
)

// ErrBillingOperationQuotaConflict means that a retry attempted to replace a
// quota target that was already durably selected by another attempt.
var ErrBillingOperationQuotaConflict = errors.New("billing operation quota target conflict")

// BillingOperation is the main-database outbox for quota/usage accounting.
// Each component is applied at most once by its operation key. The flags are
// intentionally denormalized so a retry can resume without a cross-database
// transaction.
type BillingOperation struct {
	ID               int64  `json:"id" gorm:"primaryKey"`
	OperationKey     string `json:"operation_key" gorm:"type:varchar(191);uniqueIndex;not null"`
	RequestID        string `json:"request_id" gorm:"type:varchar(191);index"`
	TaskID           string `json:"task_id" gorm:"type:varchar(191);index"`
	UserID           int    `json:"user_id" gorm:"index"`
	TokenID          int    `json:"token_id" gorm:"index"`
	ChannelID        int    `json:"channel_id" gorm:"index"`
	FundingSource    string `json:"funding_source" gorm:"type:varchar(16)"`
	PreConsumedQuota int    `json:"pre_consumed_quota"`
	// PreConsumedQuotaSet distinguishes an intentional zero reservation from a
	// row that has not recorded its first reservation yet.
	PreConsumedQuotaSet bool `json:"pre_consumed_quota_set"`
	ActualQuota         int  `json:"actual_quota"`
	ActualQuotaSet      bool `json:"actual_quota_set"`
	// FinalUsageApplied is the durable boundary for asynchronous task
	// settlement. ActualQuota is also populated during submit-time reservation,
	// so ActualQuotaSet alone cannot prove that provider final usage was seen.
	// Workers must wait for this marker before closing a successful task.
	FinalUsageApplied  bool `json:"final_usage_applied"`
	TokenReserved      bool `json:"token_reserved"`
	TokenReservedQuota int  `json:"token_reserved_quota"`
	// TokenReservationSkipped distinguishes a deliberate no-op (playground or
	// a request without a token) from a missing reservation. It prevents a
	// recovery worker from charging a token that was never pre-consumed.
	TokenReservationSkipped bool                   `json:"token_reservation_skipped"`
	TokenDelta              int                    `json:"token_delta"`
	TokenDeltaSet           bool                   `json:"token_delta_set"`
	Status                  BillingOperationStatus `json:"status" gorm:"type:varchar(32);index"`
	FundingApplied          bool                   `json:"funding_applied"`
	TokenApplied            bool                   `json:"token_applied"`
	StatsApplied            bool                   `json:"stats_applied"`
	StatsQuota              int                    `json:"stats_quota"`
	StatsQuotaSet           bool                   `json:"stats_quota_set"`
	LogApplied              bool                   `json:"log_applied"`
	LogPayload              string                 `json:"log_payload" gorm:"type:text"`
	LogPayloadSet           bool                   `json:"log_payload_set"`
	RefundFundingApplied    bool                   `json:"refund_funding_applied"`
	RefundTokenApplied      bool                   `json:"refund_token_applied"`
	RefundStatsApplied      bool                   `json:"refund_stats_applied"`
	RefundLogApplied        bool                   `json:"refund_log_applied"`
	AttemptCount            int                    `json:"attempt_count"`
	NextRetryAt             int64                  `json:"next_retry_at" gorm:"index"`
	LockedBy                string                 `json:"locked_by" gorm:"type:varchar(128);index"`
	LeaseUntil              int64                  `json:"lease_until" gorm:"index"`
	LastError               string                 `json:"last_error" gorm:"type:text"`
	CreatedAt               int64                  `json:"created_at" gorm:"index"`
	UpdatedAt               int64                  `json:"updated_at" gorm:"index"`
}

// SetBillingOperationLogPayload stores the exact consume-log parameters before
// writing to the independent log database. The payload is immutable after the
// first successful set, so a retry can reconstruct the same log row without
// duplicating or changing its billing meaning.
func SetBillingOperationLogPayload(operationKey string, params RecordConsumeLogParams) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	payload, err := common.Marshal(params)
	if err != nil {
		return err
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND log_payload_set = ?", operationKey, false).
		Updates(map[string]any{"log_payload": string(payload), "log_payload_set": true, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var operation BillingOperation
	if err := DB.Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
		return err
	}
	if operation.LogPayloadSet && operation.LogPayload == string(payload) {
		return nil
	}
	return errors.New("billing operation log payload was already set")
}

func GetBillingOperationLogPayload(operationKey string) (RecordConsumeLogParams, error) {
	var params RecordConsumeLogParams
	operation, err := GetBillingOperation(operationKey)
	if err != nil {
		return params, err
	}
	if !operation.LogPayloadSet || operation.LogPayload == "" {
		return params, errors.New("billing operation log payload is not available")
	}
	if err := common.Unmarshal([]byte(operation.LogPayload), &params); err != nil {
		return params, err
	}
	// BillingOperationKey is intentionally omitted from the serialized payload
	// because it is transport metadata, but replay must restore it before
	// RecordConsumeLogChecked so the log database unique key remains effective.
	params.BillingOperationKey = operationKey
	return params, nil
}

// BillingOperationAttrs contains immutable values used when an operation is
// first created. Later calls with the same key never overwrite these values.
type BillingOperationAttrs struct {
	OperationKey      string
	RequestID         string
	TaskID            string
	UserID            int
	TokenID           int
	ChannelID         int
	FundingSource     string
	PreConsumedQuota  int
	ActualQuota       int
	ActualQuotaSet    bool
	FinalUsageApplied bool
	Status            BillingOperationStatus
}

var billingOperationMigrateMu sync.Mutex
var billingOperationMigratedDB *gorm.DB

// ensureBillingOperationTable keeps isolated model tests usable when they
// replace the package database handle. Production startup creates the table
// through migrateDB before any billing request is accepted.
func ensureBillingOperationTable() error {
	if DB == nil {
		return errors.New("database is not initialized")
	}
	billingOperationMigrateMu.Lock()
	defer billingOperationMigrateMu.Unlock()
	if billingOperationMigratedDB == DB {
		return nil
	}
	if DB.Migrator().HasTable(&BillingOperation{}) {
		billingOperationMigratedDB = DB
		return nil
	}
	if err := DB.AutoMigrate(&BillingOperation{}); err != nil {
		return err
	}
	billingOperationMigratedDB = DB
	return nil
}

// EnsureBillingOperation creates the operation exactly once by its key.
func EnsureBillingOperation(attrs BillingOperationAttrs) (*BillingOperation, error) {
	if strings.TrimSpace(attrs.OperationKey) == "" {
		return nil, errors.New("billing operation key is required")
	}
	if attrs.PreConsumedQuota < 0 || attrs.ActualQuota < 0 {
		return nil, errors.New("billing operation quota cannot be negative")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return nil, err
	}
	status := attrs.Status
	if status == "" {
		status = BillingOperationReserved
	}
	now := common.GetTimestamp()
	operation := &BillingOperation{
		OperationKey:        attrs.OperationKey,
		RequestID:           attrs.RequestID,
		TaskID:              attrs.TaskID,
		UserID:              attrs.UserID,
		TokenID:             attrs.TokenID,
		ChannelID:           attrs.ChannelID,
		FundingSource:       attrs.FundingSource,
		PreConsumedQuota:    attrs.PreConsumedQuota,
		PreConsumedQuotaSet: attrs.PreConsumedQuota != 0,
		ActualQuota:         attrs.ActualQuota,
		// A non-zero actual quota supplied when the operation is first created is
		// an explicit final amount. Zero remains ambiguous, so callers must set
		// ActualQuotaSet for an intentional zero-usage result.
		ActualQuotaSet:    attrs.ActualQuotaSet || attrs.ActualQuota != 0,
		FinalUsageApplied: attrs.FinalUsageApplied,
		Status:            status,
		NextRetryAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	result := DB.Where("operation_key = ?", attrs.OperationKey).FirstOrCreate(operation)
	if result.Error != nil {
		// Two nodes may race on the unique key. Reload the winner instead of
		// turning an otherwise idempotent request into a billing failure.
		var existing BillingOperation
		if findErr := DB.Where("operation_key = ?", attrs.OperationKey).First(&existing).Error; findErr == nil {
			if err := validateBillingOperationAttrs(&existing, attrs); err != nil {
				return nil, err
			}
			return &existing, nil
		}
		return nil, result.Error
	}
	if err := validateBillingOperationAttrs(operation, attrs); err != nil {
		return nil, err
	}
	return operation, nil
}

func validateBillingOperationAttrs(operation *BillingOperation, attrs BillingOperationAttrs) error {
	if operation == nil {
		return errors.New("billing operation is nil")
	}
	if attrs.RequestID != "" && operation.RequestID != "" && attrs.RequestID != operation.RequestID {
		return fmt.Errorf("billing operation %s belongs to request %s: %w", attrs.OperationKey, operation.RequestID, ErrBillingOperationQuotaConflict)
	}
	if attrs.TaskID != "" && operation.TaskID != "" && attrs.TaskID != operation.TaskID {
		return fmt.Errorf("billing operation %s belongs to task %s: %w", attrs.OperationKey, operation.TaskID, ErrBillingOperationQuotaConflict)
	}
	if attrs.UserID > 0 && operation.UserID > 0 && attrs.UserID != operation.UserID {
		return fmt.Errorf("billing operation %s belongs to user %d: %w", attrs.OperationKey, operation.UserID, ErrBillingOperationQuotaConflict)
	}
	if attrs.TokenID > 0 && operation.TokenID > 0 && attrs.TokenID != operation.TokenID {
		return fmt.Errorf("billing operation %s belongs to token %d: %w", attrs.OperationKey, operation.TokenID, ErrBillingOperationQuotaConflict)
	}
	if attrs.ChannelID > 0 && operation.ChannelID > 0 && attrs.ChannelID != operation.ChannelID {
		return fmt.Errorf("billing operation %s belongs to channel %d: %w", attrs.OperationKey, operation.ChannelID, ErrBillingOperationQuotaConflict)
	}
	if attrs.FundingSource != "" && operation.FundingSource != "" && attrs.FundingSource != operation.FundingSource {
		return fmt.Errorf("billing operation %s has funding source %s: %w", attrs.OperationKey, operation.FundingSource, ErrBillingOperationQuotaConflict)
	}
	if attrs.PreConsumedQuota != 0 && operation.PreConsumedQuotaSet && attrs.PreConsumedQuota != operation.PreConsumedQuota {
		return fmt.Errorf("billing operation %s has reserved quota %d: %w", attrs.OperationKey, operation.PreConsumedQuota, ErrBillingOperationQuotaConflict)
	}
	return nil
}

func GetBillingOperation(operationKey string) (*BillingOperation, error) {
	if err := ensureBillingOperationTable(); err != nil {
		return nil, err
	}
	var operation BillingOperation
	if err := DB.Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
		return nil, err
	}
	return &operation, nil
}

func MarkBillingOperationTokenReserved(operationKey string) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND token_reserved = ?", operationKey, false).
		Updates(map[string]any{"token_reserved": true, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var operation BillingOperation
	if err := DB.Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
		return err
	}
	if operation.TokenReserved {
		return nil
	}
	return errors.New("billing operation token reservation was not marked")
}

// MarkBillingOperationComponent atomically marks one component as complete.
// Unknown component names are rejected so typos cannot silently bypass the
// idempotency barrier.
func MarkBillingOperationComponent(operationKey, component string) error {
	column := map[string]string{
		BillingComponentFunding: "funding_applied",
		BillingComponentToken:   "token_applied",
		BillingComponentStats:   "stats_applied",
		BillingComponentLog:     "log_applied",
	}[component]
	if column == "" {
		return fmt.Errorf("unknown billing component: %s", component)
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if operationComponentApplied(&operation, component, false) {
			return nil
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept positive component %s in state %s", operationKey, component, operation.Status)
		}
		result := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND "+column+" = ?", operationKey, false).
			Updates(map[string]any{column: true, "updated_at": common.GetTimestamp()})
		return billingOperationMarkerResult(result)
	})
}

// MarkBillingOperationComponentOwned is the lease-aware marker used by the
// reconciliation worker when replaying a main/log-database component.
func MarkBillingOperationComponentOwned(operationKey, component, workerID string, now int64) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("billing worker id is required")
	}
	column := map[string]string{
		BillingComponentFunding: "funding_applied",
		BillingComponentToken:   "token_applied",
		BillingComponentStats:   "stats_applied",
		BillingComponentLog:     "log_applied",
	}[component]
	if column == "" {
		return fmt.Errorf("unknown billing component: %s", component)
	}
	now = maxBillingOperationNow(now)
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if operationComponentApplied(&operation, component, false) {
			return nil
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept positive component %s in state %s", operationKey, component, operation.Status)
		}
		if operation.LockedBy != workerID || operation.LeaseUntil <= now {
			return errors.New("billing operation component lease is not owned")
		}
		markerNow := maxBillingOperationNow(now)
		result := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND "+column+" = ? AND locked_by = ? AND lease_until > ?", operationKey, false, workerID, markerNow).
			Updates(map[string]any{column: true, "updated_at": common.GetTimestamp()})
		return billingOperationMarkerResult(result)
	})
}

func MarkBillingOperationRefundComponent(operationKey, component string) error {
	return markBillingOperationComponent(operationKey, component, true, "", 0)
}

// ReserveBillingOperationToken reserves the token quota and records the
// reservation in the same main-database transaction as the operation row. The
// operation is the idempotency boundary: retrying an already recorded target
// quota is a no-op, while a larger target only applies the missing delta.
func ReserveBillingOperationToken(operationKey string, tokenID int, tokenKey string, targetQuota int, unlimited bool) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if tokenID <= 0 || targetQuota < 0 {
		return errors.New("billing token reservation is invalid")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	if tokenKey == "" {
		var token Token
		if err := DB.Select("key").First(&token, tokenID).Error; err != nil {
			return err
		}
		tokenKey = token.Key
	}
	if err := invalidateTokenCacheForMutation(tokenKey); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if operation.Status == BillingOperationSettled || operation.Status == BillingOperationRefunded ||
			operation.Status == BillingOperationRefundPending || operation.Status == BillingOperationFailed {
			return fmt.Errorf("billing operation %s is already %s", operationKey, operation.Status)
		}
		// A zero-cost initial pre-consume records that no token was reserved.
		// A later submit-time adjustment may still need to reserve quota, so this
		// marker is cleared together with the new reservation below.
		if operation.TokenReservationSkipped && operation.TokenReservedQuota > 0 {
			return errors.New("billing operation has conflicting token reservation state")
		}
		reservedQuota := 0
		if operation.TokenReserved {
			reservedQuota = operation.TokenReservedQuota
		}
		if targetQuota <= reservedQuota {
			return tx.Model(&BillingOperation{}).Where("operation_key = ?", operationKey).
				Updates(map[string]any{
					"pre_consumed_quota":     reservedQuota,
					"pre_consumed_quota_set": operation.PreConsumedQuotaSet || reservedQuota != 0,
					"updated_at":             common.GetTimestamp(),
				}).Error
		}
		delta := targetQuota - reservedQuota
		result := tx.Model(&Token{}).Where("id = ?", tokenID)
		if !unlimited {
			result = result.Where("unlimited_quota = ? OR remain_quota >= ?", true, delta)
		}
		result = result.Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota - ?", delta),
			"used_quota":    gorm.Expr("used_quota + ?", delta),
			"accessed_time": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTokenQuotaInsufficient
		}
		return tx.Model(&BillingOperation{}).Where("operation_key = ?", operationKey).
			Updates(map[string]any{
				"token_id":                  tokenID,
				"token_reserved":            true,
				"token_reserved_quota":      targetQuota,
				"token_reservation_skipped": false,
				"pre_consumed_quota":        targetQuota,
				"pre_consumed_quota_set":    true,
				"updated_at":                common.GetTimestamp(),
			}).Error
	})
}

// MarkBillingOperationTokenReservationSkipped records that no token quota was
// reserved (for example a zero-cost request or a failed pre-consume) without
// creating a refund obligation for a quota that never moved.
func MarkBillingOperationTokenReservationSkipped(operationKey string) error {
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if operation.TokenReservationSkipped {
			return nil
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept token reservation skip in state %s", operationKey, operation.Status)
		}
		// A retry that reaches the zero-cost path must not erase a reservation
		// already committed by an earlier attempt with the same operation key.
		if operation.TokenReserved {
			return nil
		}
		return tx.Model(&BillingOperation{}).Where("operation_key = ? AND token_reserved = ?", operationKey, false).
			Updates(map[string]any{
				"token_reserved":            true,
				"token_reserved_quota":      0,
				"token_reservation_skipped": true,
				"updated_at":                common.GetTimestamp(),
			}).Error
	})
}

// ApplyBillingOperationToken applies the final token delta and marks the
// settlement component in one transaction. It is safe to call after a process
// restart because the token row and marker are committed together.
func ApplyBillingOperationToken(operationKey string, tokenID int, tokenKey string, delta int, unlimited bool) error {
	return applyBillingOperationToken(operationKey, tokenID, tokenKey, delta, unlimited, "", 0)
}

// ApplyBillingOperationTokenOwned is the lease-aware counterpart used by the
// reconciliation worker. The token adjustment and settlement marker are kept
// in one transaction while requiring the worker to still own the operation.
func ApplyBillingOperationTokenOwned(operationKey string, tokenID int, tokenKey string, delta int, unlimited bool, workerID string, now int64) error {
	return applyBillingOperationToken(operationKey, tokenID, tokenKey, delta, unlimited, workerID, now)
}

// ApplyBillingOperationTokenAdjustment moves a task/request token charge from
// the operation's previously recorded actual quota to targetQuota. Unlike the
// initial settlement helper, this method intentionally permits TokenApplied to
// already be true: asynchronous task polling may discover a later provider
// usage value after the submit-time reservation has been settled.
func ApplyBillingOperationTokenAdjustment(operationKey string, tokenID int, tokenKey string, targetQuota int, unlimited bool) error {
	return applyBillingOperationTokenAdjustment(operationKey, tokenID, tokenKey, targetQuota, unlimited, "", 0)
}

// ApplyBillingOperationTokenAdjustmentOwned is the lease-aware counterpart
// used by the reconciliation worker.
func ApplyBillingOperationTokenAdjustmentOwned(operationKey string, tokenID int, tokenKey string, targetQuota int, unlimited bool, workerID string, now int64) error {
	return applyBillingOperationTokenAdjustment(operationKey, tokenID, tokenKey, targetQuota, unlimited, workerID, now)
}

func applyBillingOperationTokenAdjustment(operationKey string, tokenID int, tokenKey string, targetQuota int, unlimited bool, workerID string, now int64) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if tokenID <= 0 || targetQuota < 0 {
		return errors.New("billing token adjustment is invalid")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	if tokenKey == "" {
		var token Token
		if err := DB.Select("key").First(&token, tokenID).Error; err != nil {
			return err
		}
		tokenKey = token.Key
	}
	if err := invalidateTokenCacheForMutation(tokenKey); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		operationQuery := tx.Where("operation_key = ?", operationKey)
		if workerID != "" {
			now = maxBillingOperationNow(now)
			operationQuery = operationQuery.Where("locked_by = ? AND lease_until > ?", workerID, now)
		}
		if err := lockForUpdate(operationQuery).First(&operation).Error; err != nil {
			return err
		}
		if operation.Status == BillingOperationSettled || operation.Status == BillingOperationRefunded ||
			operation.Status == BillingOperationRefundPending || operation.Status == BillingOperationFailed {
			return fmt.Errorf("billing operation %s is already %s", operationKey, operation.Status)
		}
		if operation.FinalUsageApplied {
			if operation.ActualQuota == targetQuota {
				return nil
			}
			return fmt.Errorf("billing operation %s final quota %d cannot replace %d: %w",
				operationKey, targetQuota, operation.ActualQuota, ErrBillingOperationQuotaConflict)
		}
		// TokenApplied records that the submit-time component was committed; it
		// does not mean that an asynchronous task can no longer be adjusted.
		// Compare the durable target first so repeated polling of the same final
		// usage is idempotent, while a later target applies only the difference.
		if operation.TokenApplied && operation.ActualQuotaSet && operation.ActualQuota == targetQuota {
			return nil
		}
		if operation.TokenReservationSkipped {
			// Keep the no-op marker for a zero target. If final usage is positive,
			// this is the first real charge for a request that had no reservation;
			// fall through to the atomic token adjustment and clear the skip marker.
			if targetQuota == 0 {
				markerQuery := tx.Model(&BillingOperation{}).Where("operation_key = ?", operationKey)
				if workerID != "" {
					markerQuery = markerQuery.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
				}
				result := markerQuery.Updates(map[string]any{
					"actual_quota":     targetQuota,
					"actual_quota_set": true,
					"token_applied":    true,
					"token_delta":      targetQuota - operation.PreConsumedQuota,
					"token_delta_set":  true,
					"updated_at":       common.GetTimestamp(),
				})
				return billingOperationMarkerResult(result)
			}
		}
		// TokenApplied is the durable boundary for the previous adjustment.  A
		// task may be polled more than once after its submit-time reservation,
		// so calculate the next delta from the amount already applied instead of
		// returning early (or charging from the original pre-consume each time).
		// Before the first atomic token application, the reservation itself is the
		// amount already deducted from the token row.
		currentQuota := operation.PreConsumedQuota
		if operation.TokenApplied && operation.ActualQuotaSet {
			currentQuota = operation.ActualQuota
		}
		delta := targetQuota - currentQuota
		if delta != 0 {
			result := tx.Model(&Token{}).Where("id = ?", tokenID)
			if delta > 0 && !unlimited {
				result = result.Where("unlimited_quota = ? OR remain_quota >= ?", true, delta)
			}
			updates := map[string]any{
				"remain_quota":  gorm.Expr("remain_quota + ?", -delta),
				"used_quota":    gorm.Expr("used_quota + ?", delta),
				"accessed_time": common.GetTimestamp(),
			}
			if delta < 0 {
				amount := -delta
				updates["remain_quota"] = gorm.Expr("remain_quota + ?", amount)
				updates["used_quota"] = gorm.Expr("CASE WHEN used_quota >= ? THEN used_quota - ? ELSE 0 END", amount, amount)
			}
			result = result.Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTokenQuotaInsufficient
			}
		}
		markerQuery := tx.Model(&BillingOperation{}).Where("operation_key = ?", operationKey)
		if workerID != "" {
			markerQuery = markerQuery.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
		}
		updates := map[string]any{
			"actual_quota":     targetQuota,
			"actual_quota_set": true,
			"token_applied":    true,
			"token_delta":      targetQuota - operation.PreConsumedQuota,
			"token_delta_set":  true,
			"updated_at":       common.GetTimestamp(),
		}
		if operation.TokenReservationSkipped && targetQuota > 0 {
			updates["token_reservation_skipped"] = false
		}
		return billingOperationMarkerResult(markerQuery.Updates(updates))
	})
}

func applyBillingOperationToken(operationKey string, tokenID int, tokenKey string, delta int, unlimited bool, workerID string, now int64) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if tokenID <= 0 {
		return errors.New("billing token id is invalid")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	if tokenKey == "" {
		var token Token
		if err := DB.Select("key").First(&token, tokenID).Error; err != nil {
			return err
		}
		tokenKey = token.Key
	}
	if err := invalidateTokenCacheForMutation(tokenKey); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		operationQuery := tx.Where("operation_key = ?", operationKey)
		if workerID != "" {
			now = maxBillingOperationNow(now)
			operationQuery = operationQuery.Where("locked_by = ? AND lease_until > ?", workerID, now)
		}
		if err := lockForUpdate(operationQuery).First(&operation).Error; err != nil {
			return err
		}
		if operation.Status == BillingOperationSettled || operation.Status == BillingOperationRefunded ||
			operation.Status == BillingOperationRefundPending || operation.Status == BillingOperationFailed {
			return fmt.Errorf("billing operation %s is already %s", operationKey, operation.Status)
		}
		if operation.TokenApplied {
			return nil
		}
		if operation.TokenReservationSkipped {
			// A skipped reservation is normally a deliberate no-op (playground or
			// zero-cost request). A zero-cost request can still report actual usage
			// at settlement time, however; in that case the positive delta is the
			// first token charge and must be applied here. Negative deltas cannot
			// refund a token that was never reserved.
			if delta <= 0 {
				markerQuery := tx.Model(&BillingOperation{}).Where("operation_key = ? AND token_applied = ?", operationKey, false)
				if workerID != "" {
					markerQuery = markerQuery.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
				}
				return billingOperationMarkerResult(markerQuery.Updates(map[string]any{"token_applied": true, "token_delta": delta, "token_delta_set": true, "updated_at": common.GetTimestamp()}))
			}
			// Continue through the normal atomic token update below. The marker is
			// changed to a real reservation so a later refund can restore it.
		}
		if delta != 0 {
			result := tx.Model(&Token{}).Where("id = ?", tokenID)
			if delta > 0 && !unlimited {
				result = result.Where("unlimited_quota = ? OR remain_quota >= ?", true, delta)
			}
			updates := map[string]any{
				"remain_quota":  gorm.Expr("remain_quota + ?", -delta),
				"used_quota":    gorm.Expr("used_quota + ?", delta),
				"accessed_time": common.GetTimestamp(),
			}
			if delta < 0 {
				amount := -delta
				updates["remain_quota"] = gorm.Expr("remain_quota + ?", amount)
				updates["used_quota"] = gorm.Expr("CASE WHEN used_quota >= ? THEN used_quota - ? ELSE 0 END", amount, amount)
			}
			result = result.Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTokenQuotaInsufficient
			}
		}
		markerQuery := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND token_applied = ?", operationKey, false)
		if workerID != "" {
			markerQuery = markerQuery.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
		}
		updates := map[string]any{"token_applied": true, "token_delta": delta, "token_delta_set": true, "updated_at": common.GetTimestamp()}
		if operation.TokenReservationSkipped && delta > 0 {
			updates["token_reservation_skipped"] = false
		}
		return billingOperationMarkerResult(markerQuery.Updates(updates))
	})
}

// ApplyBillingOperationRefundToken reverses a token reservation or a final
// token charge and records the refund marker in one transaction. A missing
// reservation and unapplied token component are deliberate no-ops, which cover
// requests that failed before token pre-consume.
func ApplyBillingOperationRefundToken(operationKey string, tokenID int, tokenKey string, quota int, unlimited bool) error {
	return applyBillingOperationRefundToken(operationKey, tokenID, tokenKey, quota, unlimited, "", 0)
}

// ApplyBillingOperationRefundTokenOwned is the lease-aware counterpart used by
// the reconciliation worker. The token mutation and refund marker remain one
// transaction, and both are guarded by the worker lease so an expired worker
// cannot restore quota after a successor has claimed the operation.
func ApplyBillingOperationRefundTokenOwned(operationKey string, tokenID int, tokenKey string, quota int, unlimited bool, workerID string, now int64) error {
	return applyBillingOperationRefundToken(operationKey, tokenID, tokenKey, quota, unlimited, workerID, now)
}

func applyBillingOperationRefundToken(operationKey string, tokenID int, tokenKey string, quota int, unlimited bool, workerID string, now int64) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if quota < 0 {
		return errors.New("billing refund quota cannot be negative")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	if tokenID <= 0 {
		var existing BillingOperation
		if err := DB.Where("operation_key = ?", operationKey).First(&existing).Error; err == nil && existing.TokenID > 0 {
			tokenID = existing.TokenID
		}
	}
	if tokenKey == "" && tokenID > 0 {
		var token Token
		if err := DB.Select("key").First(&token, tokenID).Error; err != nil {
			return err
		}
		tokenKey = token.Key
	}
	if err := invalidateTokenCacheForMutation(tokenKey); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		operationQuery := tx.Where("operation_key = ?", operationKey)
		if workerID != "" {
			now = maxBillingOperationNow(now)
			operationQuery = operationQuery.Where("locked_by = ? AND lease_until > ?", workerID, now)
		}
		if err := lockForUpdate(operationQuery).First(&operation).Error; err != nil {
			return err
		}
		if operation.RefundTokenApplied {
			return nil
		}
		if operation.Status == BillingOperationSettled || operation.Status == BillingOperationRefunded || operation.Status == BillingOperationFailed {
			return fmt.Errorf("billing operation %s cannot be refunded from %s", operationKey, operation.Status)
		}
		// A positive reservation must always carry a token identity. Silently
		// marking the refund complete without restoring that quota would lose
		// the user's allowance and make the durable operation unrecoverable.
		// Rows with no token reservation (usage-only/playground requests) remain
		// valid no-ops and are handled below.
		if tokenID <= 0 && operation.TokenID > 0 {
			tokenID = operation.TokenID
		}
		refundQuota := quota
		if operation.TokenReservationSkipped {
			refundQuota = 0
		}
		if !operation.TokenReserved && !operation.TokenApplied && !(operation.ActualQuotaSet && operation.PreConsumedQuota > 0 && operation.TokenID > 0) {
			refundQuota = 0
		}
		if refundQuota > 0 && tokenID <= 0 {
			return errors.New("billing token refund requires a token id")
		}
		if refundQuota > 0 && tokenID > 0 {
			result := tx.Model(&Token{}).Where("id = ?", tokenID).Updates(map[string]any{
				"remain_quota":  gorm.Expr("remain_quota + ?", refundQuota),
				"used_quota":    gorm.Expr("CASE WHEN used_quota >= ? THEN used_quota - ? ELSE 0 END", refundQuota, refundQuota),
				"accessed_time": common.GetTimestamp(),
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		markerQuery := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND refund_token_applied = ?", operationKey, false)
		if workerID != "" {
			markerQuery = markerQuery.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
		}
		return billingOperationMarkerResult(markerQuery.Updates(map[string]any{"refund_token_applied": true, "updated_at": common.GetTimestamp()}))
	})
}

// MarkBillingOperationRefundComponentOwned marks a refund component only while
// the caller still owns the active reconciliation lease.  This prevents an
// expired worker from closing a component after another worker reclaimed the
// operation.
func MarkBillingOperationRefundComponentOwned(operationKey, component, workerID string, now int64) error {
	return markBillingOperationComponent(operationKey, component, true, workerID, now)
}

func markBillingOperationComponent(operationKey, component string, refund bool, workerID string, now int64) error {
	column := map[string]string{
		BillingComponentFunding: "funding_applied",
		BillingComponentToken:   "token_applied",
		BillingComponentStats:   "stats_applied",
		BillingComponentLog:     "log_applied",
	}[component]
	if refund {
		column = map[string]string{
			BillingComponentFunding: "refund_funding_applied",
			BillingComponentToken:   "refund_token_applied",
			BillingComponentStats:   "refund_stats_applied",
			BillingComponentLog:     "refund_log_applied",
		}[component]
	}
	if column == "" {
		kind := ""
		if refund {
			kind = "refund "
		}
		return fmt.Errorf("unknown billing %scomponent: %s", kind, component)
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	now = maxBillingOperationNow(now)
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if operationComponentApplied(&operation, component, true) {
			return nil
		}
		if !billingOperationStatusAllowsRefund(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept refund component %s in state %s", operationKey, component, operation.Status)
		}
		if workerID != "" && (operation.LockedBy != workerID || operation.LeaseUntil <= now) {
			return errors.New("billing operation lease is not owned")
		}
		query := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND "+column+" = ?", operationKey, false)
		if workerID != "" {
			query = query.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
		}
		result := query.Updates(map[string]any{column: true, "updated_at": common.GetTimestamp()})
		return billingOperationMarkerResult(result)
	})
}

// ApplyBillingOperationStats applies the main-database usage aggregates and
// records the component marker in one transaction. A retry that observes the
// marker does nothing, so a process crash cannot apply the user aggregate twice
// before the outbox row is marked complete.
func ApplyBillingOperationStats(operationKey string, userID, channelID, quota int, requestCount bool) error {
	return applyBillingOperationStats(operationKey, userID, channelID, quota, requestCount, "", 0)
}

// ApplyBillingOperationStatsOwned is the lease-aware counterpart used by the
// reconciliation worker. Aggregate updates and the component marker are kept
// in one transaction while requiring the worker to still own the operation.
func ApplyBillingOperationStatsOwned(operationKey string, userID, channelID, quota int, requestCount bool, workerID string, now int64) error {
	return applyBillingOperationStats(operationKey, userID, channelID, quota, requestCount, workerID, now)
}

func applyBillingOperationStats(operationKey string, userID, channelID, quota int, requestCount bool, workerID string, now int64) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if userID <= 0 {
		return gorm.ErrRecordNotFound
	}
	if quota < 0 {
		return errors.New("billing operation quota cannot be negative")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		operationQuery := tx.Where("operation_key = ?", operationKey)
		if workerID != "" {
			now = maxBillingOperationNow(now)
			operationQuery = operationQuery.Where("locked_by = ? AND lease_until > ?", workerID, now)
		}
		if err := lockForUpdate(operationQuery).First(&operation).Error; err != nil {
			return err
		}
		if operation.StatsApplied {
			return nil
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept positive stats in state %s", operationKey, operation.Status)
		}

		updates := map[string]any{"used_quota": gorm.Expr("used_quota + ?", quota)}
		if requestCount {
			updates["request_count"] = gorm.Expr("request_count + ?", 1)
		}
		result := tx.Model(&User{}).Where("id = ?", userID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if channelID > 0 {
			result = tx.Model(&Channel{}).Where("id = ?", channelID).
				Update("used_quota", gorm.Expr("used_quota + ?", quota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		markerQuery := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND stats_applied = ?", operationKey, false)
		if workerID != "" {
			markerQuery = markerQuery.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
		}
		result = markerQuery.
			Updates(map[string]any{"stats_applied": true, "stats_quota": quota, "stats_quota_set": true, "updated_at": common.GetTimestamp()})
		return billingOperationMarkerResult(result)
	})
}

// ApplyBillingOperationStatsAdjustment moves the usage aggregates from the
// amount already recorded for an operation to targetQuota. The applied amount
// is persisted with the marker so repeated task polling cannot apply the same
// delta twice.
func ApplyBillingOperationStatsAdjustment(operationKey string, userID, channelID, targetQuota int) error {
	return applyBillingOperationStatsAdjustment(operationKey, userID, channelID, targetQuota, "", 0)
}

// ApplyBillingOperationStatsAdjustmentOwned is the lease-aware counterpart
// used by the reconciliation worker for asynchronous task adjustments.
func ApplyBillingOperationStatsAdjustmentOwned(operationKey string, userID, channelID, targetQuota int, workerID string, now int64) error {
	return applyBillingOperationStatsAdjustment(operationKey, userID, channelID, targetQuota, workerID, now)
}

func applyBillingOperationStatsAdjustment(operationKey string, userID, channelID, targetQuota int, workerID string, now int64) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if userID <= 0 || targetQuota < 0 {
		return errors.New("billing operation stats adjustment is invalid")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		operationQuery := tx.Where("operation_key = ?", operationKey)
		if workerID != "" {
			now = maxBillingOperationNow(now)
			operationQuery = operationQuery.Where("locked_by = ? AND lease_until > ?", workerID, now)
		}
		if err := lockForUpdate(operationQuery).First(&operation).Error; err != nil {
			return err
		}
		if operation.StatsApplied && operation.StatsQuotaSet && operation.StatsQuota == targetQuota {
			return nil
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept stats adjustment in state %s", operationKey, operation.Status)
		}
		appliedQuota := 0
		if operation.StatsApplied && operation.StatsQuotaSet {
			appliedQuota = operation.StatsQuota
		}
		delta := targetQuota - appliedQuota
		if delta != 0 {
			result := tx.Model(&User{}).Where("id = ?", userID).Update("used_quota", usageQuotaExpression(delta))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
			if channelID > 0 {
				result = tx.Model(&Channel{}).Where("id = ?", channelID).Update("used_quota", usageQuotaExpression(delta))
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return gorm.ErrRecordNotFound
				}
			}
		}
		markerQuery := tx.Model(&BillingOperation{}).Where("operation_key = ?", operationKey)
		if workerID != "" {
			markerQuery = markerQuery.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
		}
		return billingOperationMarkerResult(markerQuery.
			Updates(map[string]any{"stats_applied": true, "stats_quota": targetQuota, "stats_quota_set": true, "updated_at": common.GetTimestamp()}))
	})
}

func usageQuotaExpression(delta int) any {
	if delta < 0 {
		amount := -delta
		return gorm.Expr("CASE WHEN used_quota >= ? THEN used_quota - ? ELSE 0 END", amount, amount)
	}
	return gorm.Expr("used_quota + ?", delta)
}

// ApplyBillingOperationRefundStats reverses an applied stats component and
// marks the refund in the same main-database transaction. If the original
// stats component was never applied (for example, an immediate task failure),
// the refund marker is recorded as a no-op.
func ApplyBillingOperationRefundStats(operationKey string, userID, channelID, quota int) error {
	return applyBillingOperationRefundStats(operationKey, userID, channelID, quota, "", 0)
}

// ApplyBillingOperationRefundStatsOwned is the lease-aware counterpart used by
// the reconciliation worker. It keeps the aggregate update and refund marker
// in one transaction while requiring the worker to still own the operation.
func ApplyBillingOperationRefundStatsOwned(operationKey string, userID, channelID, quota int, workerID string, now int64) error {
	return applyBillingOperationRefundStats(operationKey, userID, channelID, quota, workerID, now)
}

func applyBillingOperationRefundStats(operationKey string, userID, channelID, quota int, workerID string, now int64) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if quota < 0 {
		return errors.New("billing refund quota cannot be negative")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		operationQuery := tx.Where("operation_key = ?", operationKey)
		if workerID != "" {
			now = maxBillingOperationNow(now)
			operationQuery = operationQuery.Where("locked_by = ? AND lease_until > ?", workerID, now)
		}
		if err := lockForUpdate(operationQuery).First(&operation).Error; err != nil {
			return err
		}
		if operation.RefundStatsApplied {
			return nil
		}
		if !billingOperationStatusAllowsRefund(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept stats refund in state %s", operationKey, operation.Status)
		}
		// Standalone task submissions are marked StatsApplied by
		// EnsureTaskBillingOperation when their submit path already updated the
		// aggregates. An empty RequestID is not sufficient evidence: request
		// operations may legitimately omit it, and refunding their stats would
		// underflow the user/channel usage counters.
		if operation.StatsApplied {
			if userID <= 0 {
				return gorm.ErrRecordNotFound
			}
			appliedQuota := quota
			if operation.StatsQuotaSet {
				appliedQuota = operation.StatsQuota
			}
			result := tx.Model(&User{}).Where("id = ?", userID).
				Update("used_quota", usageQuotaExpression(-appliedQuota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
			if channelID > 0 {
				result = tx.Model(&Channel{}).Where("id = ?", channelID).
					Update("used_quota", usageQuotaExpression(-appliedQuota))
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return gorm.ErrRecordNotFound
				}
			}
		}
		markerQuery := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND refund_stats_applied = ?", operationKey, false)
		if workerID != "" {
			markerQuery = markerQuery.Where("locked_by = ? AND lease_until > ?", workerID, maxBillingOperationNow(now))
		}
		result := markerQuery.
			Updates(map[string]any{"refund_stats_applied": true, "updated_at": common.GetTimestamp()})
		return billingOperationMarkerResult(result)
	})
}

func operationComponentApplied(operation *BillingOperation, component string, refund bool) bool {
	if operation == nil {
		return false
	}
	if refund {
		switch component {
		case BillingComponentFunding:
			return operation.RefundFundingApplied
		case BillingComponentToken:
			return operation.RefundTokenApplied
		case BillingComponentStats:
			return operation.RefundStatsApplied
		case BillingComponentLog:
			return operation.RefundLogApplied
		}
		return false
	}
	switch component {
	case BillingComponentFunding:
		return operation.FundingApplied
	case BillingComponentToken:
		return operation.TokenApplied
	case BillingComponentStats:
		return operation.StatsApplied
	case BillingComponentLog:
		return operation.LogApplied
	}
	return false
}

func billingOperationMarkerResult(result *gorm.DB) error {
	if result == nil {
		return gorm.ErrRecordNotFound
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func maxBillingOperationNow(now int64) int64 {
	current := common.GetTimestamp()
	if now < current {
		return current
	}
	return now
}

func billingOperationStatusAllowsPositive(status BillingOperationStatus) bool {
	return status == BillingOperationReserved || status == BillingOperationApplying
}

func billingOperationStatusAllowsRefund(status BillingOperationStatus) bool {
	return status == BillingOperationReserved || status == BillingOperationApplying || status == BillingOperationRefundPending
}

func UpdateBillingOperationActualQuota(operationKey string, actualQuota int) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if actualQuota < 0 {
		return errors.New("billing operation actual quota cannot be negative")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept actual quota in state %s", operationKey, operation.Status)
		}
		if operation.ActualQuotaSet {
			if operation.ActualQuota == actualQuota {
				return nil
			}
			return fmt.Errorf("billing operation %s actual quota %d cannot replace %d: %w",
				operationKey, actualQuota, operation.ActualQuota, ErrBillingOperationQuotaConflict)
		}
		updates := map[string]any{
			"actual_quota":     actualQuota,
			"actual_quota_set": true,
			"updated_at":       common.GetTimestamp(),
		}
		result := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND actual_quota_set = ?", operationKey, operation.ActualQuotaSet).
			Updates(updates)
		return billingOperationMarkerResult(result)
	})
}

// UpdateBillingOperationActualQuotaOwned updates the final task/request usage
// only while the reconciliation worker still owns its active lease. It keeps
// a stale worker from writing a new actual amount after a successor reclaimed
// the operation.
func UpdateBillingOperationActualQuotaOwned(operationKey, workerID string, actualQuota int, now int64) (bool, error) {
	if strings.TrimSpace(workerID) == "" {
		return false, errors.New("billing worker id is required")
	}
	if actualQuota < 0 {
		return false, errors.New("billing operation actual quota cannot be negative")
	}
	now = maxBillingOperationNow(now)
	if err := ensureBillingOperationTable(); err != nil {
		return false, err
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND locked_by = ? AND lease_until > ? AND status IN ? AND final_usage_applied = ? AND (actual_quota_set = ? OR actual_quota = ?)", operationKey, workerID, now,
			[]BillingOperationStatus{BillingOperationReserved, BillingOperationApplying}, false, false, actualQuota).
		Updates(map[string]any{"actual_quota": actualQuota, "actual_quota_set": true, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	var operation BillingOperation
	if err := DB.Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
		return false, err
	}
	if operation.LockedBy == workerID && operation.LeaseUntil > now &&
		(operation.Status == BillingOperationReserved || operation.Status == BillingOperationApplying) &&
		operation.ActualQuotaSet && operation.ActualQuota == actualQuota {
		return true, nil
	}
	if operation.LockedBy == workerID && operation.LeaseUntil > now &&
		(operation.Status == BillingOperationReserved || operation.Status == BillingOperationApplying) &&
		operation.ActualQuotaSet && operation.ActualQuota != actualQuota {
		if !operation.FinalUsageApplied {
			return true, nil
		}
		return false, fmt.Errorf("billing operation %s actual quota %d cannot replace %d: %w",
			operationKey, actualQuota, operation.ActualQuota, ErrBillingOperationQuotaConflict)
	}
	return false, nil
}

// MarkBillingOperationFinalUsage records that the asynchronous task's final
// provider usage has been observed and all final quota adjustments are ready
// to be considered for terminal settlement. Submit-time operations also set
// ActualQuotaSet, so that field cannot serve as this boundary by itself.
func MarkBillingOperationFinalUsage(operationKey string, actualQuota int) error {
	return markBillingOperationFinalUsage(operationKey, "", actualQuota, 0)
}

// MarkBillingOperationFinalUsageOwned is the lease-aware form used by the
// reconciliation worker when a caller has already persisted final usage and
// only needs to close the durable marker.
func MarkBillingOperationFinalUsageOwned(operationKey, workerID string, actualQuota int, now int64) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("billing worker id is required")
	}
	return markBillingOperationFinalUsage(operationKey, workerID, actualQuota, now)
}

func markBillingOperationFinalUsage(operationKey, workerID string, actualQuota int, now int64) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if actualQuota < 0 {
		return errors.New("billing operation final quota cannot be negative")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if workerID != "" {
			now = maxBillingOperationNow(now)
			if operation.LockedBy != workerID || operation.LeaseUntil <= now {
				return gorm.ErrRecordNotFound
			}
		}
		if operation.FinalUsageApplied {
			if operation.ActualQuota == actualQuota {
				return nil
			}
			return fmt.Errorf("billing operation %s final quota %d cannot replace %d: %w",
				operationKey, actualQuota, operation.ActualQuota, ErrBillingOperationQuotaConflict)
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept final usage in state %s", operationKey, operation.Status)
		}
		if operation.ActualQuotaSet && operation.ActualQuota != actualQuota && operation.FinalUsageApplied {
			return fmt.Errorf("billing operation %s final quota %d cannot replace %d: %w",
				operationKey, actualQuota, operation.ActualQuota, ErrBillingOperationQuotaConflict)
		}
		updates := map[string]any{
			"actual_quota":        actualQuota,
			"actual_quota_set":    true,
			"final_usage_applied": true,
			"updated_at":          common.GetTimestamp(),
		}
		query := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND final_usage_applied = ?", operationKey, false)
		if workerID != "" {
			query = query.Where("locked_by = ? AND lease_until > ?", workerID, now)
		}
		return billingOperationMarkerResult(query.Updates(updates))
	})
}

func UpdateBillingOperationReservedQuota(operationKey string, reservedQuota int) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if reservedQuota < 0 {
		return errors.New("billing operation reserved quota cannot be negative")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s does not accept reserved quota in state %s", operationKey, operation.Status)
		}
		if operation.PreConsumedQuotaSet {
			if operation.PreConsumedQuota == reservedQuota {
				return nil
			}
			return fmt.Errorf("billing operation %s reserved quota %d cannot replace %d: %w",
				operationKey, reservedQuota, operation.PreConsumedQuota, ErrBillingOperationQuotaConflict)
		}
		result := tx.Model(&BillingOperation{}).
			Where("operation_key = ? AND pre_consumed_quota_set = ?", operationKey, false).
			Updates(map[string]any{
				"pre_consumed_quota":     reservedQuota,
				"pre_consumed_quota_set": true,
				"updated_at":             common.GetTimestamp(),
			})
		return billingOperationMarkerResult(result)
	})
}

// UpdateBillingOperationChannelID binds the channel selected after request
// pricing. Channel selection happens after pre-consume for ordinary requests,
// and a retry may legitimately select a different channel before usage
// statistics are written. Once stats or the consume log is durable, changing
// the channel would split the accounting record and is rejected.
func UpdateBillingOperationChannelID(operationKey string, channelID int) error {
	if strings.TrimSpace(operationKey) == "" {
		return errors.New("billing operation key is required")
	}
	if channelID <= 0 {
		return errors.New("billing operation channel id is invalid")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var operation BillingOperation
		if err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error; err != nil {
			return err
		}
		if operation.ChannelID == channelID {
			return nil
		}
		if !billingOperationStatusAllowsPositive(operation.Status) {
			return fmt.Errorf("billing operation %s cannot bind channel in state %s", operationKey, operation.Status)
		}
		if operation.ChannelID != 0 && (operation.RequestID == "" || operation.StatsApplied || operation.LogApplied) {
			return fmt.Errorf("billing operation %s already belongs to channel %d", operationKey, operation.ChannelID)
		}
		result := tx.Model(&BillingOperation{}).
			Where("operation_key = ?", operationKey).
			Update("channel_id", channelID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func HasDueBillingOperations(now int64) bool {
	if err := ensureBillingOperationTable(); err != nil {
		return false
	}
	var count int64
	err := DB.Model(&BillingOperation{}).
		Where("status IN ? AND next_retry_at <= ? AND (lease_until <= ? OR lease_until IS NULL)",
			[]BillingOperationStatus{BillingOperationReserved, BillingOperationApplying, BillingOperationRefundPending}, now, now, now).
		Count(&count).Error
	return err == nil && count > 0
}

// UpdateBillingOperationStatus performs a guarded state transition. A stale
// worker cannot overwrite a newer retry or terminal result.
func UpdateBillingOperationStatus(operationKey string, from []BillingOperationStatus, to BillingOperationStatus, lastError string, nextRetryAt int64) (bool, error) {
	if err := ensureBillingOperationTable(); err != nil {
		return false, err
	}
	if len(from) == 0 {
		return false, errors.New("billing operation source status is required")
	}
	updates := map[string]any{
		"status":        to,
		"last_error":    lastError,
		"next_retry_at": nextRetryAt,
		"updated_at":    common.GetTimestamp(),
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND status IN ?", operationKey, from).
		Updates(updates)
	return result.RowsAffected == 1, result.Error
}

// UpdateBillingOperationStatusOwned performs the same guarded transition as
// UpdateBillingOperationStatus, but also requires the caller to hold the
// currently active worker lease.  Reconciliation workers must use this form
// when completing or deferring a claimed row; otherwise a worker whose lease
// expired could overwrite the retry/terminal state written by its successor.
// The lease is released as part of the transition so the row can be claimed
// again immediately when it remains retryable.
func UpdateBillingOperationStatusOwned(operationKey, workerID string, from []BillingOperationStatus, to BillingOperationStatus, lastError string, nextRetryAt, now int64) (bool, error) {
	if strings.TrimSpace(workerID) == "" {
		return false, errors.New("billing worker id is required")
	}
	now = maxBillingOperationNow(now)
	if err := ensureBillingOperationTable(); err != nil {
		return false, err
	}
	if len(from) == 0 {
		return false, errors.New("billing operation source status is required")
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND locked_by = ? AND lease_until > ? AND status IN ?", operationKey, workerID, now, from).
		Updates(map[string]any{
			"status":        to,
			"last_error":    lastError,
			"next_retry_at": nextRetryAt,
			"locked_by":     "",
			"lease_until":   0,
			"updated_at":    common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// UpdateBillingOperationStatusOwnedKeepLease is the same owner-checked state
// transition but deliberately retains the lease. Refund reconciliation needs
// to move reserved/applying work into refund_pending and then apply several
// idempotent components before releasing ownership at the terminal state.
func UpdateBillingOperationStatusOwnedKeepLease(operationKey, workerID string, from []BillingOperationStatus, to BillingOperationStatus, lastError string, nextRetryAt, now int64) (bool, error) {
	if strings.TrimSpace(workerID) == "" {
		return false, errors.New("billing worker id is required")
	}
	now = maxBillingOperationNow(now)
	if err := ensureBillingOperationTable(); err != nil {
		return false, err
	}
	if len(from) == 0 {
		return false, errors.New("billing operation source status is required")
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND locked_by = ? AND lease_until > ? AND status IN ?", operationKey, workerID, now, from).
		Updates(map[string]any{
			"status":        to,
			"last_error":    lastError,
			"next_retry_at": nextRetryAt,
			"updated_at":    common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// ExpireUnfinishedRequestBillingOperationOwned atomically turns a stale
// request reservation into refund_pending. The actual-quota guard is part of
// the CAS so a request that settles concurrently cannot be mistaken for an
// abandoned request by the recovery worker.
func ExpireUnfinishedRequestBillingOperationOwned(operationKey, workerID string, cutoff, now int64, reason string) (bool, error) {
	if strings.TrimSpace(operationKey) == "" || strings.TrimSpace(workerID) == "" {
		return false, errors.New("billing operation and worker id are required")
	}
	now = maxBillingOperationNow(now)
	if err := ensureBillingOperationTable(); err != nil {
		return false, err
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND task_id = ? AND actual_quota_set = ? AND created_at <= ? AND locked_by = ? AND lease_until > ? AND status IN ?",
			operationKey, "", false, cutoff, workerID, now,
			[]BillingOperationStatus{BillingOperationReserved, BillingOperationApplying}).
		Updates(map[string]any{
			"status":        BillingOperationRefundPending,
			"last_error":    reason,
			"next_retry_at": now,
			"updated_at":    common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// RenewBillingOperationLease extends a worker's lease only while it is still
// the owner.  It is intentionally a conditional update so a late heartbeat
// from an expired worker cannot steal a row back from a newer worker.
func RenewBillingOperationLease(operationKey, workerID string, now int64, leaseSeconds int) (bool, error) {
	if strings.TrimSpace(workerID) == "" {
		return false, errors.New("billing worker id is required")
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	if leaseSeconds <= 0 {
		leaseSeconds = 60
	}
	if err := ensureBillingOperationTable(); err != nil {
		return false, err
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND locked_by = ? AND lease_until > ? AND status IN ?", operationKey, workerID, now,
			[]BillingOperationStatus{BillingOperationReserved, BillingOperationApplying, BillingOperationRefundPending}).
		Updates(map[string]any{
			"lease_until": now + int64(leaseSeconds),
			"updated_at":  common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// ReleaseBillingOperationLease releases a worker's claim without changing
// the durable operation state. It is used after task-bound accounting, whose
// terminal transition is performed by the task CAS/refund helpers rather than
// the generic request transition above. Ownership is still checked so a late
// worker cannot clear a successor's lease.
func ReleaseBillingOperationLease(operationKey, workerID string) (bool, error) {
	if strings.TrimSpace(workerID) == "" {
		return false, errors.New("billing worker id is required")
	}
	if err := ensureBillingOperationTable(); err != nil {
		return false, err
	}
	result := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND locked_by = ?", operationKey, workerID).
		Updates(map[string]any{
			"locked_by":   "",
			"lease_until": 0,
			"updated_at":  common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// BillingOperationLeaseOwned reports whether workerID still owns an active
// lease. It is a lightweight guard for task-bound reconciliation helpers that
// perform their own task CAS before updating this operation row.
func BillingOperationLeaseOwned(operationKey, workerID string, now int64) (bool, error) {
	if strings.TrimSpace(workerID) == "" {
		return false, errors.New("billing worker id is required")
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	if err := ensureBillingOperationTable(); err != nil {
		return false, err
	}
	var count int64
	if err := DB.Model(&BillingOperation{}).
		Where("operation_key = ? AND locked_by = ? AND lease_until > ?", operationKey, workerID, now).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count == 1, nil
}

// ClaimBillingOperations returns due operations and gives each a short lease.
// Claiming is done with a conditional update per row so multiple processes can
// safely scan the same queue on SQLite, MySQL, and PostgreSQL.
func ClaimBillingOperations(workerID string, now int64, leaseSeconds int, limit int) ([]*BillingOperation, error) {
	if workerID == "" {
		return nil, errors.New("billing worker id is required")
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	if leaseSeconds <= 0 {
		leaseSeconds = 60
	}
	if limit <= 0 {
		limit = 50
	}
	if err := ensureBillingOperationTable(); err != nil {
		return nil, err
	}
	var candidates []*BillingOperation
	if err := DB.Where("status IN ?", []BillingOperationStatus{BillingOperationReserved, BillingOperationApplying, BillingOperationRefundPending}).
		Where("next_retry_at <= ?", now).
		Where("lease_until <= ? OR lease_until IS NULL", now).
		Order("id").Limit(limit).Find(&candidates).Error; err != nil {
		return nil, err
	}
	claimed := make([]*BillingOperation, 0, len(candidates))
	for _, candidate := range candidates {
		// Preserve refund_pending while leasing the row. The lease fields already
		// exclude concurrent workers; changing the status here would erase the
		// durable intent and let the request reconciler mistake a refund for a
		// normal settlement.
		targetStatus := BillingOperationApplying
		if candidate.Status == BillingOperationRefundPending {
			targetStatus = BillingOperationRefundPending
		}
		result := DB.Model(&BillingOperation{}).
			Where("id = ? AND status = ? AND next_retry_at <= ? AND (lease_until <= ? OR lease_until IS NULL)", candidate.ID,
				candidate.Status, now, now).
			Updates(map[string]any{
				"status":        targetStatus,
				"locked_by":     workerID,
				"lease_until":   now + int64(leaseSeconds),
				"attempt_count": gorm.Expr("attempt_count + ?", 1),
				"updated_at":    now,
			})
		if result.Error != nil {
			return claimed, result.Error
		}
		if result.RowsAffected == 1 {
			candidate.Status = targetStatus
			candidate.LockedBy = workerID
			candidate.LeaseUntil = now + int64(leaseSeconds)
			candidate.AttemptCount++
			claimed = append(claimed, candidate)
		}
	}
	return claimed, nil
}

func BillingOperationKeyForRequest(requestID string) string {
	if requestID == "" {
		requestID = common.NewRequestId()
	}
	return boundedBillingOperationKey("request:", requestID)
}

func BillingOperationKeyForTask(taskID string) string {
	return boundedBillingOperationKey("task:", taskID)
}

// MySQL 5.7 with utf8mb4 limits a 191-character indexed varchar to 767
// bytes. Task and request identifiers can be supplied by plugins, so bound
// the generated operation key consistently across all supported databases.
func boundedBillingOperationKey(prefix, value string) string {
	key := prefix + value
	if len(key) <= 160 {
		return key
	}
	digest := sha256.Sum256([]byte(key))
	return prefix + fmt.Sprintf("hash:%x", digest[:])
}

// EnsureTaskBillingOperation binds the task identity to the operation created
// before submission. A deterministic task key is used only for callers that
// construct a task without a request session (for example an internal task
// runner); normal relay requests always carry the session key.
func EnsureTaskBillingOperation(task *Task) (*BillingOperation, error) {
	if task == nil || task.TaskID == "" {
		return nil, errors.New("task billing operation requires a task id")
	}
	key := strings.TrimSpace(task.PrivateData.BillingOperationKey)
	if key == "" {
		key = BillingOperationKeyForTask(task.TaskID)
		task.PrivateData.BillingOperationKey = key
	}
	fundingSource := task.PrivateData.BillingSource
	if strings.TrimSpace(fundingSource) == "" {
		fundingSource = "usage"
	}
	// Re-entry happens after asynchronous quota reconciliation has updated the
	// task row. The task's current quota is the latest actual amount, not the
	// immutable submit-time reservation recorded on the operation. Reuse the
	// durable reservation when the row already exists so retries do not turn a
	// legitimate reconciliation into an immutable-quota conflict.
	preConsumedQuota := task.Quota
	if existing, getErr := GetBillingOperation(key); getErr == nil {
		if existing.PreConsumedQuotaSet {
			preConsumedQuota = existing.PreConsumedQuota
		}
	} else if !errors.Is(getErr, gorm.ErrRecordNotFound) {
		return nil, getErr
	}
	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey:     key,
		TaskID:           task.TaskID,
		UserID:           task.UserId,
		TokenID:          task.PrivateData.TokenId,
		ChannelID:        task.ChannelId,
		FundingSource:    fundingSource,
		PreConsumedQuota: preConsumedQuota,
	})
	if err != nil {
		return nil, err
	}
	// Standalone task runners do not have a request session to perform the
	// submit-time reservation. Seed their persisted snapshot once so terminal
	// retries can still reverse the exact token/statistics amount. Normal relay
	// tasks use a request:* operation and never enter this branch.
	if operation.RequestID == "" && strings.HasPrefix(key, "task:") {
		updates := map[string]any{"updated_at": common.GetTimestamp()}
		changed := false
		if !operation.StatsApplied {
			updates["stats_applied"] = true
			updates["stats_quota"] = task.Quota
			updates["stats_quota_set"] = true
			changed = true
		}
		if task.PrivateData.TokenId > 0 && task.Quota > 0 && !operation.TokenReserved && !operation.TokenApplied {
			updates["token_id"] = task.PrivateData.TokenId
			updates["token_reserved"] = true
			updates["token_reserved_quota"] = task.Quota
			changed = true
		}
		if changed {
			if err := DB.Model(&BillingOperation{}).Where("operation_key = ?", key).Updates(updates).Error; err != nil {
				return nil, err
			}
			operation, err = GetBillingOperation(key)
			if err != nil {
				return nil, err
			}
		}
	}
	// A request-bound task may be reconstructed after the submitter has
	// persisted the task row but before the synchronous settlement marker was
	// written. When a token reservation exists, its task quota is the known
	// submit-time target and can safely seed ActualQuota for recovery. Tokenless
	// operations leave the target unset so a later final usage value can still be
	// recorded without conflicting with this submit-time marker.
	if strings.HasPrefix(key, "request:") && operation.RequestID == "" &&
		!operation.ActualQuotaSet && task.Quota > 0 &&
		(operation.TokenID > 0 || task.PrivateData.TokenId > 0) {
		if err := DB.Model(&BillingOperation{}).Where("operation_key = ? AND actual_quota_set = ?", key, false).
			Updates(map[string]any{
				"actual_quota":     task.Quota,
				"actual_quota_set": true,
				"updated_at":       common.GetTimestamp(),
			}).Error; err != nil {
			return nil, err
		}
		operation, err = GetBillingOperation(key)
		if err != nil {
			return nil, err
		}
	}
	if operation.TaskID != "" && operation.TaskID != task.TaskID {
		return nil, fmt.Errorf("billing operation %s belongs to task %s", key, operation.TaskID)
	}
	if operation.FundingSource == "" && fundingSource != "" {
		if err := DB.Model(&BillingOperation{}).Where("operation_key = ? AND funding_source = ''", key).
			Update("funding_source", fundingSource).Error; err != nil {
			return nil, err
		}
		operation.FundingSource = fundingSource
	}
	if operation.UserID != 0 && operation.UserID != task.UserId {
		return nil, fmt.Errorf("billing operation %s belongs to user %d", key, operation.UserID)
	}
	// A request operation is created before an async task is inserted. Attach
	// the durable task identity once it exists so a restarted poller can find
	// and resume the same accounting operation.
	if operation.TaskID == "" || operation.UserID == 0 || operation.TokenID == 0 || operation.ChannelID == 0 {
		updates := map[string]any{"updated_at": common.GetTimestamp()}
		if operation.TaskID == "" {
			updates["task_id"] = task.TaskID
		}
		if operation.UserID == 0 {
			updates["user_id"] = task.UserId
		}
		if operation.TokenID == 0 && task.PrivateData.TokenId > 0 {
			updates["token_id"] = task.PrivateData.TokenId
		}
		if operation.ChannelID == 0 && task.ChannelId > 0 {
			updates["channel_id"] = task.ChannelId
		}
		if err := DB.Model(&BillingOperation{}).Where("operation_key = ?", key).Updates(updates).Error; err != nil {
			return nil, err
		}
		operation, err = GetBillingOperation(key)
		if err != nil {
			return nil, err
		}
	}
	return operation, nil
}
