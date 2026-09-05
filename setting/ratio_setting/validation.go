package ratio_setting

import "github.com/QuantumNous/new-api/common"

// ValidateFlatRatioJSON checks the shape of a model/group ratio document
// without changing the live ratio map.
func ValidateFlatRatioJSON(jsonStr string) error {
	var values map[string]float64
	return common.Unmarshal([]byte(jsonStr), &values)
}

// ValidateNestedRatioJSON checks the shape of a group-to-group ratio document
// without changing the live ratio map.
func ValidateNestedRatioJSON(jsonStr string) error {
	var values map[string]map[string]float64
	return common.Unmarshal([]byte(jsonStr), &values)
}
