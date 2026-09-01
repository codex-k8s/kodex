// Package errs содержит стабильные доменные классы ошибок control-plane.
package errs

import "errors"

var (
	ErrInvalid                     = errors.New("invalid input")
	ErrUnauthorized                = errors.New("unauthorized")
	ErrFreshAuthenticationRequired = errors.New("fresh authentication required")
	ErrForbidden                   = errors.New("forbidden")
	ErrNotFound                    = errors.New("not found")
	ErrConflict                    = errors.New("conflict")
	ErrCapabilityRequired          = errors.New("required capability is not enabled")
	ErrAlreadyResolved             = errors.New("resource is already resolved")
	ErrVersionMismatch             = errors.New("version mismatch")
	ErrIdempotencyReuse            = errors.New("idempotency key reused with different intent")
	ErrProtected                   = errors.New("protected system resource")
	ErrUnavailable                 = errors.New("temporarily unavailable")
)
