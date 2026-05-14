// Package errs defines agent-manager domain error sentinels.
package errs

import "errors"

var (
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrNotFound           = errors.New("not found")
	ErrAlreadyExists      = errors.New("already exists")
	ErrConflict           = errors.New("conflict")
	ErrPreconditionFailed = errors.New("precondition failed")
)
