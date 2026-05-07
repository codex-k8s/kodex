// Package errs contains package-hub domain sentinel errors.
package errs

import "errors"

const (
	alreadyExistsMessage         = "already exists"
	conflictMessage              = "conflict"
	dependencyUnavailableMessage = "dependency unavailable"
	forbiddenMessage             = "forbidden"
	invalidArgumentMessage       = "invalid argument"
	notFoundMessage              = "not found"
	preconditionFailedMessage    = "precondition failed"
)

var (
	ErrAlreadyExists         = errors.New(alreadyExistsMessage)
	ErrConflict              = errors.New(conflictMessage)
	ErrDependencyUnavailable = errors.New(dependencyUnavailableMessage)
	ErrForbidden             = errors.New(forbiddenMessage)
	ErrInvalidArgument       = errors.New(invalidArgumentMessage)
	ErrNotFound              = errors.New(notFoundMessage)
	ErrPreconditionFailed    = errors.New(preconditionFailedMessage)
)
