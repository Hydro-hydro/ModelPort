package model

import "errors"

// Common errors
var (
	ErrDatabase                 = errors.New("database error")
	ErrWalletQuotaLimitExceeded = errors.New("wallet quota limit exceeded")
)

// Token auth errors
var (
	ErrTokenNotProvided = errors.New("token not provided")
	ErrTokenInvalid     = errors.New("token invalid")
)
