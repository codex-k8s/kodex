// Package errs contains fleet-manager domain error sentinels.
package errs

import "errors"

var (
	// ErrAlreadyExists marks a unique identity conflict.
	ErrAlreadyExists = errors.New("fleet already exists")
	// ErrConflict marks an optimistic concurrency conflict.
	ErrConflict = errors.New("fleet conflict")
	// ErrDependencyUnavailable marks an unavailable dependent service.
	ErrDependencyUnavailable = errors.New("fleet dependency unavailable")
	// ErrForbidden marks an access decision denial.
	ErrForbidden = errors.New("fleet forbidden")
	// ErrInvalidArgument marks an invalid fleet command or persistence argument.
	ErrInvalidArgument = errors.New("invalid fleet argument")
	// ErrNotFound marks absent fleet state.
	ErrNotFound = errors.New("fleet not found")
	// ErrPreconditionFailed marks a missing or invalid dependent fleet state.
	ErrPreconditionFailed = errors.New("fleet precondition failed")
)
