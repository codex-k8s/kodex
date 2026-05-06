// Package errs contains stable domain error sentinels for project-catalog.
package errs

import "errors"

var (
	// ErrAlreadyExists reports a natural-key conflict.
	ErrAlreadyExists = errors.New("already exists")
	// ErrConflict reports an optimistic concurrency or idempotency conflict.
	ErrConflict = errors.New("conflict")
	// ErrDependencyUnavailable reports a temporary dependency failure.
	ErrDependencyUnavailable = errors.New("dependency unavailable")
	// ErrForbidden reports a denied access decision.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidArgument reports malformed command or query input.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrNotFound reports a missing aggregate or invisible object.
	ErrNotFound = errors.New("not found")
	// ErrPreconditionFailed reports a violated persistence invariant.
	ErrPreconditionFailed = errors.New("precondition failed")
)
