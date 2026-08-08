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
	ErrAborted             = errors.New("control-plane operation aborted")
	ErrVersionMismatch     = errors.New("control-plane version mismatch")
	ErrFailedPrecondition  = errors.New("control-plane precondition failed")
	ErrDataLoss            = errors.New("control-plane stored data is corrupt")
	ErrUnavailable         = errors.New("control-plane dependency unavailable")
	ErrInternal            = errors.New("control-plane internal error")
)
