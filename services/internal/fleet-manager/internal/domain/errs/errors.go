// Package errs contains fleet-manager domain error sentinels.
package errs

import "errors"

var (
	// ErrAlreadyExists marks a unique identity conflict.
	ErrAlreadyExists error
	// ErrConflict marks an optimistic concurrency conflict.
	ErrConflict error
	// ErrDependencyUnavailable marks an unavailable dependent service.
	ErrDependencyUnavailable error
	// ErrForbidden marks an access decision denial.
	ErrForbidden error
	// ErrInvalidArgument marks an invalid fleet command or persistence argument.
	ErrInvalidArgument error
	// ErrNotFound marks absent fleet state.
	ErrNotFound error
	// ErrPreconditionFailed marks a missing or invalid dependent fleet state.
	ErrPreconditionFailed error
)

func init() {
	ErrAlreadyExists = errors.New("fleet already exists")
	ErrConflict = errors.New("fleet conflict")
	ErrDependencyUnavailable = errors.New("fleet dependency unavailable")
	ErrForbidden = errors.New("fleet forbidden")
	ErrInvalidArgument = errors.New("invalid fleet argument")
	ErrNotFound = errors.New("fleet not found")
	ErrPreconditionFailed = errors.New("fleet precondition failed")
}
