package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneralSettingDefaultsToCNY(t *testing.T) {
	assert.Equal(t, QuotaDisplayTypeCNY, generalSetting.QuotaDisplayType)
	assert.Equal(t, QuotaDisplayTypeCNY, GetQuotaDisplayType())
	assert.Equal(t, "¥", GetCurrencySymbol())
}

func TestGeneralSettingPreservesExplicitDisplayType(t *testing.T) {
	original := generalSetting
	t.Cleanup(func() { generalSetting = original })

	require.NoError(t, config.UpdateConfigFromMap(&generalSetting, map[string]string{
		"quota_display_type": QuotaDisplayTypeUSD,
	}))

	assert.Equal(t, QuotaDisplayTypeUSD, generalSetting.QuotaDisplayType)
	assert.Equal(t, QuotaDisplayTypeUSD, GetQuotaDisplayType())
	assert.Equal(t, "$", GetCurrencySymbol())
}
