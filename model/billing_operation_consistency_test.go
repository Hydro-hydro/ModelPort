package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureBillingOperationRequiresStartupSchema(t *testing.T) {
	db := openMainSchemaTestDB(t)
	useMainSchemaTestDB(t, db)

	_, err := EnsureBillingOperation(BillingOperationAttrs{OperationKey: "request:missing-schema"})

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "billing operation table is missing"))
	assert.False(t, db.Migrator().HasTable(&BillingOperation{}))
}

func TestEnsureBillingOperationRejectsImmutableAttributeConflict(t *testing.T) {
	require.NoError(t, ensureBillingOperationTable())
	const key = "request:billing-operation-immutable-attrs"
	require.NoError(t, DB.Where("operation_key = ?", key).Delete(&BillingOperation{}).Error)
	t.Cleanup(func() { _ = DB.Where("operation_key = ?", key).Delete(&BillingOperation{}).Error })

	_, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey:     key,
		RequestID:        "request-a",
		UserID:           11,
		TokenID:          22,
		ChannelID:        33,
		PreConsumedQuota: 100,
	})
	require.NoError(t, err)

	_, err = EnsureBillingOperation(BillingOperationAttrs{
		OperationKey: key,
		RequestID:    "request-b",
		UserID:       11,
		TokenID:      22,
		ChannelID:    33,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBillingOperationQuotaConflict))
}

func TestUpdateBillingOperationActualQuotaIsFirstWriterWins(t *testing.T) {
	require.NoError(t, ensureBillingOperationTable())
	const key = "request:billing-operation-actual-first-writer"
	require.NoError(t, DB.Where("operation_key = ?", key).Delete(&BillingOperation{}).Error)
	t.Cleanup(func() { _ = DB.Where("operation_key = ?", key).Delete(&BillingOperation{}).Error })

	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey:     key,
		RequestID:        key,
		PreConsumedQuota: 100,
	})
	require.NoError(t, err)
	assert.False(t, operation.ActualQuotaSet)

	require.NoError(t, UpdateBillingOperationActualQuota(key, 120))
	require.NoError(t, UpdateBillingOperationActualQuota(key, 120))
	err = UpdateBillingOperationActualQuota(key, 180)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBillingOperationQuotaConflict))

	updated, err := GetBillingOperation(key)
	require.NoError(t, err)
	assert.Equal(t, 120, updated.ActualQuota)
	assert.True(t, updated.ActualQuotaSet)
}

func TestUpdateBillingOperationReservedQuotaIsFirstWriterWins(t *testing.T) {
	require.NoError(t, ensureBillingOperationTable())
	const key = "request:billing-operation-reserved-first-writer"
	require.NoError(t, DB.Where("operation_key = ?", key).Delete(&BillingOperation{}).Error)
	t.Cleanup(func() { _ = DB.Where("operation_key = ?", key).Delete(&BillingOperation{}).Error })

	_, err := EnsureBillingOperation(BillingOperationAttrs{OperationKey: key})
	require.NoError(t, err)
	require.NoError(t, UpdateBillingOperationReservedQuota(key, 0))
	require.NoError(t, UpdateBillingOperationReservedQuota(key, 0))
	err = UpdateBillingOperationReservedQuota(key, 90)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBillingOperationQuotaConflict))

	updated, err := GetBillingOperation(key)
	require.NoError(t, err)
	assert.Zero(t, updated.PreConsumedQuota)
	assert.True(t, updated.PreConsumedQuotaSet)
}

func TestMarkBillingOperationFinalUsageIsFirstWriterWins(t *testing.T) {
	require.NoError(t, ensureBillingOperationTable())
	const key = "request:billing-operation-final-first-writer"
	require.NoError(t, DB.Where("operation_key = ?", key).Delete(&BillingOperation{}).Error)
	t.Cleanup(func() { _ = DB.Where("operation_key = ?", key).Delete(&BillingOperation{}).Error })

	_, err := EnsureBillingOperation(BillingOperationAttrs{OperationKey: key})
	require.NoError(t, err)
	require.NoError(t, MarkBillingOperationFinalUsage(key, 70))
	require.NoError(t, MarkBillingOperationFinalUsage(key, 70))
	err = MarkBillingOperationFinalUsage(key, 80)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBillingOperationQuotaConflict))
}
