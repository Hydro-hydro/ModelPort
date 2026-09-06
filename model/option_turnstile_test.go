package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTurnstileEnableRequiresBothKeys(t *testing.T) {
	previousSiteKey := common.TurnstileSiteKey
	previousSecretKey := common.TurnstileSecretKey
	previousEnabled := common.TurnstileCheckEnabled
	t.Cleanup(func() {
		common.TurnstileSiteKey = previousSiteKey
		common.TurnstileSecretKey = previousSecretKey
		common.TurnstileCheckEnabled = previousEnabled
	})

	common.TurnstileSiteKey = ""
	common.TurnstileSecretKey = ""
	assert.Error(t, validateTurnstileOption("TurnstileCheckEnabled", "true"))

	common.TurnstileSiteKey = "site-key"
	assert.Error(t, validateTurnstileOption("TurnstileCheckEnabled", "true"))

	common.TurnstileSecretKey = "secret-key"
	require.NoError(t, validateTurnstileOption("TurnstileCheckEnabled", "true"))
}

func TestValidateTurnstileCannotClearActiveKey(t *testing.T) {
	previousSiteKey := common.TurnstileSiteKey
	previousSecretKey := common.TurnstileSecretKey
	previousEnabled := common.TurnstileCheckEnabled
	t.Cleanup(func() {
		common.TurnstileSiteKey = previousSiteKey
		common.TurnstileSecretKey = previousSecretKey
		common.TurnstileCheckEnabled = previousEnabled
	})

	common.TurnstileSiteKey = "site-key"
	common.TurnstileSecretKey = "secret-key"
	common.TurnstileCheckEnabled = true

	assert.Error(t, validateTurnstileOption("TurnstileSiteKey", ""))
	assert.Error(t, validateTurnstileOption("TurnstileSecretKey", ""))
	require.NoError(t, validateTurnstileOption("TurnstileCheckEnabled", "false"))
}

func TestRetiredPasswordLoginOptionIsIgnored(t *testing.T) {
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{"PasswordLoginEnabled": "true"}
	t.Cleanup(func() { common.OptionMap = previousOptions })

	require.NoError(t, updateOptionMap("PasswordLoginEnabled", "false"))
	assert.NotContains(t, common.OptionMap, "PasswordLoginEnabled")
}
