package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenMetadataUpdateDoesNotOverwriteConcurrentQuota(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	token := createReserveTestToken(t, 100)
	stale, err := GetTokenById(token.Id)
	require.NoError(t, err)

	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 30))
	stale.Name = "metadata-only-update"
	require.NoError(t, stale.Update())

	got := getTokenFromDB(t, token.Id)
	assert.Equal(t, "metadata-only-update", got.Name)
	assert.Equal(t, 70, got.RemainQuota)
	assert.Equal(t, 30, got.UsedQuota)
}

func TestTokenQuotaUpdateRejectsConcurrentAccountingChange(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	token := createReserveTestToken(t, 100)
	stale, err := GetTokenById(token.Id)
	require.NoError(t, err)

	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 30))
	err = stale.UpdateWithQuota(80, false, stale.RemainQuota, stale.UsedQuota)
	require.ErrorIs(t, err, ErrTokenQuotaChanged)

	got := getTokenFromDB(t, token.Id)
	assert.Equal(t, "reserve-test", got.Name)
	assert.Equal(t, 70, got.RemainQuota)
	assert.Equal(t, 30, got.UsedQuota)
}

func TestTokenQuotaUpdatePreservesExplicitEditWhenSnapshotMatches(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)

	token := createReserveTestToken(t, 100)
	stale, err := GetTokenById(token.Id)
	require.NoError(t, err)

	// A no-op quota edit must still succeed on databases such as MySQL where
	// RowsAffected is zero when an UPDATE leaves every value unchanged.
	require.NoError(t, stale.UpdateWithQuota(100, false, stale.RemainQuota, stale.UsedQuota))
	require.NoError(t, stale.UpdateWithQuota(80, false, stale.RemainQuota, stale.UsedQuota))
	got := getTokenFromDB(t, token.Id)
	assert.Equal(t, 80, got.RemainQuota)
	assert.False(t, got.UnlimitedQuota)
	assert.Zero(t, got.UsedQuota)
}
