// Package errs contains governance-manager domain sentinel errors.
package errs

import "errors"

const (
	dependencyUnavailableMessage = "dependency unavailable"
	invalidArgumentMessage       = "invalid argument"
	notImplementedMessage        = "not implemented"
)

var (
	ErrDependencyUnavailable = errors.New(dependencyUnavailableMessage)
	ErrInvalidArgument       = errors.New(invalidArgumentMessage)
	ErrNotImplemented        = errors.New(notImplementedMessage)
)
