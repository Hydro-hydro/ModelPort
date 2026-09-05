package model

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizePersonalUserSettingDropsRetiredFields(t *testing.T) {
	setting := dto.UserSetting{
		QuotaWarningThreshold: 123,
		BillingPreference:     "wallet",
		Language:              "zh",
	}

	sanitized := SanitizePersonalUserSetting(setting)

	require.Equal(t, "zh", sanitized.Language)
	assert.Zero(t, sanitized.QuotaWarningThreshold)
	assert.Empty(t, sanitized.BillingPreference)
}
