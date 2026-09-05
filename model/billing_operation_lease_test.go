package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingOperationLeaseGuardsTerminalTransition(t *testing.T) {
	if err := ensureBillingOperationTable(); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, DB.Exec("DELETE FROM billing_operations").Error)
	t.Cleanup(func() { _ = DB.Exec("DELETE FROM billing_operations") })

	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey: "request:lease-guard",
		Status:       BillingOperationReserved,
	})
	require.NoError(t, err)

	now := common.GetTimestamp()
	claimed, err := ClaimBillingOperations("worker-a", now, 60, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	updated, err := UpdateBillingOperationStatusOwned(operation.OperationKey, "worker-b",
		[]BillingOperationStatus{BillingOperationApplying}, BillingOperationSettled, "stale", now, now)
	require.NoError(t, err)
	assert.False(t, updated)

	updated, err = UpdateBillingOperationStatusOwned(operation.OperationKey, "worker-a",
		[]BillingOperationStatus{BillingOperationApplying}, BillingOperationSettled, "", now, now)
	require.NoError(t, err)
	assert.True(t, updated)

	current, err := GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, BillingOperationSettled, current.Status)
	assert.Empty(t, current.LockedBy)
	assert.Zero(t, current.LeaseUntil)
}

func TestBillingOperationLeaseCannotRenewAfterExpiry(t *testing.T) {
	if err := ensureBillingOperationTable(); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, DB.Exec("DELETE FROM billing_operations").Error)
	t.Cleanup(func() { _ = DB.Exec("DELETE FROM billing_operations") })

	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey: "request:lease-expiry",
		Status:       BillingOperationReserved,
	})
	require.NoError(t, err)
	now := common.GetTimestamp()
	claimed, err := ClaimBillingOperations("worker-a", now, 10, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	renewed, err := RenewBillingOperationLease(operation.OperationKey, "worker-a", now+11, 60)
	require.NoError(t, err)
	assert.False(t, renewed)

	// A successor can claim the expired row, while the old worker remains
	// unable to complete it with the old owner id.
	claimed, err = ClaimBillingOperations("worker-b", now+11, 60, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	updated, err := UpdateBillingOperationStatusOwned(operation.OperationKey, "worker-a",
		[]BillingOperationStatus{BillingOperationApplying}, BillingOperationSettled, "stale", now+11, now+11)
	require.NoError(t, err)
	assert.False(t, updated)
}

func TestClaimBillingOperationsPreservesRefundPending(t *testing.T) {
	if err := ensureBillingOperationTable(); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, DB.Exec("DELETE FROM billing_operations").Error)
	t.Cleanup(func() { _ = DB.Exec("DELETE FROM billing_operations") })

	operation, err := EnsureBillingOperation(BillingOperationAttrs{
		OperationKey: "request:lease-refund",
		Status:       BillingOperationRefundPending,
	})
	require.NoError(t, err)
	claimed, err := ClaimBillingOperations("worker-a", common.GetTimestamp(), 60, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, BillingOperationRefundPending, claimed[0].Status)

	current, err := GetBillingOperation(operation.OperationKey)
	require.NoError(t, err)
	assert.Equal(t, BillingOperationRefundPending, current.Status)
}
