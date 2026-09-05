package setting

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

const DefaultMaxTokenAutoGroups = 5

var autoGroups = []string{
	"default",
}
var autoGroupsMutex sync.RWMutex

var DefaultUseAutoGroup = false

var maxTokenAutoGroups atomic.Int64

func init() {
	maxTokenAutoGroups.Store(DefaultMaxTokenAutoGroups)
}

func ContainsAutoGroup(group string) bool {
	for _, autoGroup := range GetAutoGroups() {
		if autoGroup == group {
			return true
		}
	}
	return false
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	var next []string
	if err := common.Unmarshal([]byte(jsonString), &next); err != nil {
		return err
	}
	if next == nil {
		next = make([]string, 0)
	}
	autoGroupsMutex.Lock()
	autoGroups = next
	autoGroupsMutex.Unlock()
	return nil
}

func AutoGroups2JsonString() string {
	jsonBytes, err := common.Marshal(GetAutoGroups())
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func GetAutoGroups() []string {
	autoGroupsMutex.RLock()
	defer autoGroupsMutex.RUnlock()
	return append([]string(nil), autoGroups...)
}

func GetDefaultUseAutoGroup() bool {
	autoGroupsMutex.RLock()
	defer autoGroupsMutex.RUnlock()
	return DefaultUseAutoGroup
}

func SetDefaultUseAutoGroup(enabled bool) {
	autoGroupsMutex.Lock()
	DefaultUseAutoGroup = enabled
	autoGroupsMutex.Unlock()
}

func GetMaxTokenAutoGroups() int {
	return int(maxTokenAutoGroups.Load())
}

func ValidateMaxTokenAutoGroups(value string) error {
	maxCount, err := strconv.Atoi(value)
	if err != nil || maxCount <= 0 {
		return fmt.Errorf("MaxTokenAutoGroups must be a positive integer")
	}
	return nil
}

func UpdateMaxTokenAutoGroups(value string) error {
	if err := ValidateMaxTokenAutoGroups(value); err != nil {
		return err
	}
	maxCount, _ := strconv.Atoi(value)
	maxTokenAutoGroups.Store(int64(maxCount))
	return nil
}
