// Package errs contains fleet-manager domain error sentinels.
package errs

import "errors"

var (
	// ErrAlreadyExists marks a unique identity conflict.
	ErrAlreadyExists = errors.New("fleet already exists")
	// ErrConflict marks an optimistic concurrency conflict.
	ErrConflict = errors.New("fleet conflict")
	// ErrInvalidArgument marks an invalid fleet command or persistence argument.
	ErrInvalidArgument = errors.New("invalid fleet argument")
	// ErrNotFound marks absent fleet state.
	ErrNotFound = errors.New("fleet not found")
	// ErrPreconditionFailed marks a missing or invalid dependent fleet state.
	ErrPreconditionFailed = errors.New("fleet precondition failed")
)
