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

func TestLoadTurnstileOptionsRestoresPersistedValuesRegardlessOfDatabaseOrder(t *testing.T) {
	db := openMainSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	// Insert the dependent switch before its keys to reproduce a database
	// result order that would make the old loader reject the enabled value.
	for _, option := range []*Option{
		{Key: "TurnstileCheckEnabled", Value: "true"},
		{Key: "TurnstileSecretKey", Value: "secret-key"},
		{Key: "TurnstileSiteKey", Value: "site-key"},
	} {
		require.NoError(t, db.Create(option).Error)
	}
	useMainSchemaTestDB(t, db)

	previousOptions := common.OptionMap
	previousSiteKey := common.TurnstileSiteKey
	previousSecretKey := common.TurnstileSecretKey
	previousEnabled := common.TurnstileCheckEnabled
	common.OptionMap = map[string]string{
		"TurnstileCheckEnabled": "false",
		"TurnstileSiteKey":      "",
		"TurnstileSecretKey":    "",
	}
	common.TurnstileSiteKey = ""
	common.TurnstileSecretKey = ""
	common.TurnstileCheckEnabled = false
	t.Cleanup(func() {
		common.OptionMap = previousOptions
		common.TurnstileSiteKey = previousSiteKey
		common.TurnstileSecretKey = previousSecretKey
		common.TurnstileCheckEnabled = previousEnabled
	})

	loadOptionsFromDatabase()

	assert.Equal(t, "site-key", common.TurnstileSiteKey)
	assert.Equal(t, "secret-key", common.TurnstileSecretKey)
	assert.True(t, common.TurnstileCheckEnabled)
	assert.Equal(t, "true", common.OptionMap["TurnstileCheckEnabled"])
}

func TestRetiredPasswordLoginOptionIsIgnored(t *testing.T) {
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{"PasswordLoginEnabled": "true"}
	t.Cleanup(func() { common.OptionMap = previousOptions })

	require.NoError(t, updateOptionMap("PasswordLoginEnabled", "false"))
	assert.NotContains(t, common.OptionMap, "PasswordLoginEnabled")
}
