// Package errs содержит безопасные ошибки домена runtime-controller.
package errs

import "errors"

var (
	ErrNoWork              = errors.New("no runtime execution is available")
	ErrCapacityDeferred    = errors.New("runtime capacity is unavailable")
	ErrStateConflict       = errors.New("runtime state is stale")
	ErrInvalidInput        = errors.New("runtime input is invalid")
	ErrDependency          = errors.New("runtime dependency is unavailable")
	ErrArchiveUnverified   = errors.New("runtime archive is not verified")
	ErrCleanupUnauthorized = errors.New("runtime cleanup is not authorized")
)
