package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeViolationFeeErrorKeepsViolationErrorsNonRetryable(t *testing.T) {
	original := types.NewOpenAIError(
		errors.New(CSAMViolationMarker),
		types.ErrorCodeBadResponse,
		http.StatusBadRequest,
	)

	normalized := NormalizeViolationFeeError(original)

	require.NotNil(t, normalized)
	assert.Equal(t, types.ErrorCodeViolationFeeGrokCSAM, normalized.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(normalized))
	assert.Equal(t, http.StatusBadRequest, normalized.StatusCode)
}

func TestNormalizeViolationFeeErrorDoesNotCreateBillingSideEffects(t *testing.T) {
	original := types.NewError(
		errors.New(ContentViolatesUsageMarker),
		types.ErrorCodeBadResponse,
	)

	normalized := NormalizeViolationFeeError(original)

	require.NotNil(t, normalized)
	assert.Equal(t, types.ErrorCodeViolationFeeGrokCSAM, normalized.GetErrorCode())
	assert.Equal(t, original.Error(), normalized.Error())
	// Violation handling is error classification only. Model usage, token
	// quota, consume logs, and BillingOperation rows are written by the normal
	// request billing path, never by error normalization.
	assert.False(t, IsViolationFeeCode(types.ErrorCodeBadResponse))
}
