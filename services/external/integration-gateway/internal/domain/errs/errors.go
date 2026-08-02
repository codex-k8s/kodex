package errs

import "errors"

var (
	ErrUnauthenticated       = errors.New("authentication failed")
	ErrForbidden             = errors.New("access denied")
	ErrNotFound              = errors.New("resource not found")
	ErrConflict              = errors.New("resource conflict")
	ErrInvalid               = errors.New("request is invalid")
	ErrExpired               = errors.New("resource expired")
	ErrQuotaExceeded         = errors.New("quota exceeded")
	ErrApprovalRequired      = errors.New("approval is required")
	ErrOutcomeUnknown        = errors.New("provider outcome is unknown")
	ErrCredentialUnavailable = errors.New("credential is unavailable")
)
