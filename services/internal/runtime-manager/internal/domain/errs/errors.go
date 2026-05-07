// Package errs contains runtime-manager domain sentinel errors.
package errs

import "errors"

var (
	// ErrInvalidArgument marks malformed input.
	ErrInvalidArgument = errors.New("runtime invalid argument")
	// ErrForbidden marks a denied operation.
	ErrForbidden = errors.New("runtime forbidden")
	// ErrNotFound marks a missing aggregate.
	ErrNotFound = errors.New("runtime not found")
	// ErrAlreadyExists marks a duplicate aggregate.
	ErrAlreadyExists = errors.New("runtime already exists")
	// ErrConflict marks optimistic concurrency or lease conflicts.
	ErrConflict = errors.New("runtime conflict")
	// ErrPreconditionFailed marks a violated domain precondition.
	ErrPreconditionFailed = errors.New("runtime precondition failed")
	// ErrDependencyUnavailable marks an unavailable external dependency.
	ErrDependencyUnavailable = errors.New("runtime dependency unavailable")
)
