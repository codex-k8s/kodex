// Package errs contains stable domain error sentinels for access-manager.
package errs

import "errors"

// ErrAlreadyExists reports a natural-key or singleton invariant conflict.
var ErrAlreadyExists = errors.New("already exists")

// ErrConflict reports an optimistic concurrency conflict.
var ErrConflict = errors.New("conflict")

// ErrForbidden reports a denied operation for an authenticated subject.
var ErrForbidden = errors.New("forbidden")

// ErrInvalidArgument reports malformed command input.
var ErrInvalidArgument = errors.New("invalid argument")

// ErrNotFound reports a missing aggregate or invisible object.
var ErrNotFound = errors.New("not found")

// ErrOwnerOrgImmutable reports an unsupported owner organization mutation.
var ErrOwnerOrgImmutable = errors.New("owner organization cannot be disabled")

// ErrPreconditionFailed reports a domain invariant violation.
var ErrPreconditionFailed = errors.New("precondition failed")

// ErrUnauthorizedSubject reports failed primary admission for a subject.
var ErrUnauthorizedSubject = errors.New("unauthorized subject")
