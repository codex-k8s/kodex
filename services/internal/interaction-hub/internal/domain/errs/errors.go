package errs

import "errors"

var (
	// ErrInvalidArgument marks transport or domain input that cannot be accepted.
	ErrInvalidArgument = errors.New("interaction invalid argument")
	// ErrNotImplemented marks IH-2 backlog operations that have a stable contract but no business implementation yet.
	ErrNotImplemented = errors.New("interaction operation is not implemented")
	// ErrUnavailable marks a required interaction-hub dependency that is not ready.
	ErrUnavailable = errors.New("interaction dependency unavailable")
)
