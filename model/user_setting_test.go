package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersonalUserSettingPersistenceExcludesRetiredFields(t *testing.T) {
	setting := dto.UserSetting{
		QuotaWarningThreshold: 123,
		BillingPreference:     "wallet",
		Language:              "zh",
	}

	encoded, err := marshalPersonalUserSetting(setting)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "quota_warning_threshold")
	assert.NotContains(t, string(encoded), "billing_preference")

	decoded := decodePersonalUserSetting(`{"quota_warning_threshold":123,"billing_preference":"wallet","language":"zh"}`)
	require.Equal(t, "zh", decoded.Language)
	assert.Zero(t, decoded.QuotaWarningThreshold)
	assert.Empty(t, decoded.BillingPreference)

	var persisted map[string]any
	require.NoError(t, common.Unmarshal(encoded, &persisted))
	assert.Equal(t, "zh", persisted["language"])
}
