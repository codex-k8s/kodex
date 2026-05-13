// Package errs contains runtime-manager domain sentinel errors.
package errs

import "errors"

// ErrInvalidArgument marks malformed input.
var ErrInvalidArgument = runtimeError("invalid argument")

// ErrForbidden marks a denied operation.
var ErrForbidden = runtimeError("forbidden")

// ErrNotFound marks a missing aggregate.
var ErrNotFound = runtimeError("not found")

// ErrAlreadyExists marks a duplicate aggregate.
var ErrAlreadyExists = runtimeError("already exists")

// ErrConflict marks optimistic concurrency or lease conflicts.
var ErrConflict = runtimeError("conflict")

// ErrPreconditionFailed marks a violated domain precondition.
var ErrPreconditionFailed = runtimeError("precondition failed")

// ErrPlacementRejected marks a fleet-owned placement refusal.
var ErrPlacementRejected = runtimeError("placement rejected")

// ErrDependencyUnavailable marks an unavailable external dependency.
var ErrDependencyUnavailable = runtimeError("dependency unavailable")

func runtimeError(message string) error {
	return errors.New("runtime " + message)
}
