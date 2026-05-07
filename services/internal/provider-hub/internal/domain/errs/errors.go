// Package errs contains stable domain error sentinels for provider-hub.
package errs

import "errors"

var (
	// ErrAlreadyExists reports a natural-key conflict.
	ErrAlreadyExists = domainError("already exists")
	// ErrConflict reports an optimistic concurrency or idempotency conflict.
	ErrConflict = domainError("conflict")
	// ErrDependencyUnavailable reports a temporary dependency failure.
	ErrDependencyUnavailable = domainError("dependency unavailable")
	// ErrForbidden reports a denied provider or platform action.
	ErrForbidden = domainError("forbidden")
	// ErrInvalidArgument reports malformed command or query input.
	ErrInvalidArgument = domainError("invalid argument")
	// ErrNotFound reports a missing aggregate or invisible object.
	ErrNotFound = domainError("not found")
	// ErrPreconditionFailed reports a violated domain or persistence invariant.
	ErrPreconditionFailed = domainError("precondition failed")
)

func domainError(message string) error {
	return errors.New(message)
}
