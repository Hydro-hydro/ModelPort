package setting

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateAutoGroupsPreservesValueWhenJSONIsInvalid(t *testing.T) {
	original := AutoGroups2JsonString()
	require.NoError(t, UpdateAutoGroupsByJsonString(`["default"]`))
	t.Cleanup(func() {
		require.NoError(t, UpdateAutoGroupsByJsonString(original))
	})

	err := UpdateAutoGroupsByJsonString(`{"broken"`)

	require.Error(t, err)
	assert.Equal(t, []string{"default"}, GetAutoGroups())
}

func TestUpdateMaxTokenAutoGroupsAcceptsAnyPositiveInteger(t *testing.T) {
	original := GetMaxTokenAutoGroups()
	t.Cleanup(func() {
		require.NoError(t, UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", original)))
	})

	require.NoError(t, UpdateMaxTokenAutoGroups("123456"))
	assert.Equal(t, 123456, GetMaxTokenAutoGroups())
}

func TestUpdateMaxTokenAutoGroupsRejectsInvalidValuesWithoutChangingState(t *testing.T) {
	original := GetMaxTokenAutoGroups()
	for _, value := range []string{"", "0", "-1", "1.5", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			assert.Error(t, UpdateMaxTokenAutoGroups(value))
			assert.Equal(t, original, GetMaxTokenAutoGroups())
		})
	}
}
