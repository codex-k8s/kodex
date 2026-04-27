package errs

import "errors"

var (
	ErrAlreadyExists       = errors.New("already exists")
	ErrConflict            = errors.New("conflict")
	ErrForbidden           = errors.New("forbidden")
	ErrInvalidArgument     = errors.New("invalid argument")
	ErrNotFound            = errors.New("not found")
	ErrOwnerOrgImmutable   = errors.New("owner organization cannot be disabled")
	ErrPreconditionFailed  = errors.New("precondition failed")
	ErrUnauthorizedSubject = errors.New("unauthorized subject")
)
