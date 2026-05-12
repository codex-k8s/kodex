// Package errs contains fleet-manager domain error sentinels.
package errs

import "errors"

var (
	// ErrAlreadyExists marks a unique identity conflict.
	ErrAlreadyExists = fleetError("already exists")
	// ErrConflict marks an optimistic concurrency conflict.
	ErrConflict = fleetError("conflict")
	// ErrDependencyUnavailable marks an unavailable dependent service.
	ErrDependencyUnavailable = fleetError("dependency unavailable")
	// ErrForbidden marks an access decision denial.
	ErrForbidden = fleetError("forbidden")
	// ErrInvalidArgument marks an invalid fleet command or persistence argument.
	ErrInvalidArgument = fleetError("invalid argument")
	// ErrNotFound marks absent fleet state.
	ErrNotFound = fleetError("not found")
	// ErrPreconditionFailed marks a missing or invalid dependent fleet state.
	ErrPreconditionFailed = fleetError("precondition failed")
)

func fleetError(message string) error {
	return errors.New("fleet " + message)
}
