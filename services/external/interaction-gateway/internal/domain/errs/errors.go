// Package errs содержит стабильные ошибки domain boundary.
package errs

import "errors"

var (
	ErrIgnored      = errors.New("inbound event ignored")
	ErrBusy         = errors.New("inbound event is already processing")
	ErrUnauthorized = errors.New("inbound authority is invalid")
	ErrNotFound     = errors.New("interaction resource not found")
	ErrConflict     = errors.New("interaction state conflict")
	ErrUnavailable  = errors.New("interaction dependency unavailable")
)
