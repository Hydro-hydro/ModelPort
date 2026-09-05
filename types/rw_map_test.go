package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromJsonStringPreservesValueWhenJSONIsInvalid(t *testing.T) {
	m := NewRWMap[string, int]()
	m.Set("keep", 7)

	err := LoadFromJsonString(m, `{"broken"`)

	require.Error(t, err)
	value, ok := m.Get("keep")
	assert.True(t, ok)
	assert.Equal(t, 7, value)
}

func TestLoadFromJsonStringWithCallbackRunsAfterSuccessfulSwap(t *testing.T) {
	m := NewRWMap[string, int]()
	m.Set("old", 1)
	callbackSawNewValue := false

	err := LoadFromJsonStringWithCallback(m, `{"new":2}`, func() {
		_, oldPresent := m.Get("old")
		value, newPresent := m.Get("new")
		callbackSawNewValue = !oldPresent && newPresent && value == 2
	})

	require.NoError(t, err)
	assert.True(t, callbackSawNewValue)
}
