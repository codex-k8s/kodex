// Package errs contains governance-manager domain sentinel errors.
package errs

import "errors"

const (
	alreadyExistsMessage         = "already exists"
	conflictMessage              = "conflict"
	dependencyUnavailableMessage = "dependency unavailable"
	forbiddenMessage             = "forbidden"
	invalidArgumentMessage       = "invalid argument"
	notFoundMessage              = "not found"
	notImplementedMessage        = "not implemented"
	preconditionFailedMessage    = "precondition failed"
)

var (
	ErrAlreadyExists         = errors.New(alreadyExistsMessage)
	ErrConflict              = errors.New(conflictMessage)
	ErrDependencyUnavailable = errors.New(dependencyUnavailableMessage)
	ErrForbidden             = errors.New(forbiddenMessage)
	ErrInvalidArgument       = errors.New(invalidArgumentMessage)
	ErrNotFound              = errors.New(notFoundMessage)
	ErrNotImplemented        = errors.New(notImplementedMessage)
	ErrPreconditionFailed    = errors.New(preconditionFailedMessage)
)
