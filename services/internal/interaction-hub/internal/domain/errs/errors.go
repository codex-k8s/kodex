package errs

import "errors"

var (
	// ErrAlreadyExists marks duplicate domain identity or idempotency result.
	ErrAlreadyExists = errors.New("interaction already exists")
	// ErrConflict marks stale versions or incompatible idempotent replays.
	ErrConflict = errors.New("interaction conflict")
	// ErrInvalidArgument marks transport or domain input that cannot be accepted.
	ErrInvalidArgument = errors.New("interaction invalid argument")
	// ErrNotFound marks an interaction aggregate that does not exist.
	ErrNotFound = errors.New("interaction not found")
	// ErrNotImplemented marks IH-2 backlog operations that have a stable contract but no business implementation yet.
	ErrNotImplemented = errors.New("interaction operation is not implemented")
	// ErrUnavailable marks a required interaction-hub dependency that is not ready.
	ErrUnavailable = errors.New("interaction dependency unavailable")
)
