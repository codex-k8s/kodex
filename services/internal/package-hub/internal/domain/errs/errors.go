// Package errs contains package-hub domain sentinel errors.
package errs

import "errors"

var (
	ErrAlreadyExists      = errors.New("already exists")
	ErrConflict           = errors.New("conflict")
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrNotFound           = errors.New("not found")
	ErrPreconditionFailed = errors.New("precondition failed")
)
