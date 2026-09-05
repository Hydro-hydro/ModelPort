package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateModelRequestRateLimitGroupPreservesValueWhenJSONIsInvalid(t *testing.T) {
	ModelRequestRateLimitMutex.Lock()
	previous := ModelRequestRateLimitGroup
	ModelRequestRateLimitGroup = map[string][2]int{"default": {10, 2}}
	ModelRequestRateLimitMutex.Unlock()
	t.Cleanup(func() {
		ModelRequestRateLimitMutex.Lock()
		ModelRequestRateLimitGroup = previous
		ModelRequestRateLimitMutex.Unlock()
	})

	err := UpdateModelRequestRateLimitGroupByJSONString(`{"broken"`)

	require.Error(t, err)
	total, success, found := GetGroupRateLimit("default")
	assert.True(t, found)
	assert.Equal(t, 10, total)
	assert.Equal(t, 2, success)
}
