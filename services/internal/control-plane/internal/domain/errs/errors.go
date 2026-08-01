// Package errs задаёт безопасные доменные ошибки control-plane.
package errs

import "errors"

var (
	ErrInvalidInput        = errors.New("invalid control-plane input")
	ErrUnauthenticated     = errors.New("control-plane authentication required")
	ErrPermissionDenied    = errors.New("control-plane permission denied")
	ErrNotFound            = errors.New("control-plane resource not found")
	ErrStateConflict       = errors.New("control-plane state conflict")
	ErrIdempotencyConflict = errors.New("control-plane idempotency conflict")
	ErrVersionMismatch     = errors.New("control-plane version mismatch")
	ErrUnavailable         = errors.New("control-plane dependency unavailable")
	ErrInternal            = errors.New("control-plane internal error")
)
