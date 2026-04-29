// Package errs contains stable domain error sentinels for access-manager.
package errs

import "errors"

var (
	// ErrAlreadyExists reports a natural-key or singleton invariant conflict.
	ErrAlreadyExists = errors.New("already exists")
	// ErrConflict reports an optimistic concurrency conflict.
	ErrConflict = errors.New("conflict")
	// ErrForbidden reports a denied operation for an authenticated subject.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidArgument reports malformed command input.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrNotFound reports a missing aggregate or invisible object.
	ErrNotFound = errors.New("not found")
	// ErrOwnerOrgImmutable reports an unsupported owner organization mutation.
	ErrOwnerOrgImmutable = errors.New("owner organization cannot be disabled")
	// ErrPreconditionFailed reports a domain invariant violation.
	ErrPreconditionFailed = errors.New("precondition failed")
	// ErrUnauthorizedSubject reports failed primary admission for a subject.
	ErrUnauthorizedSubject = errors.New("unauthorized subject")
)
