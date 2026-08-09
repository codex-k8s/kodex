// Package errs содержит стабильные ошибки domain boundary.
package errs

import "errors"

var (
	ErrIgnored             = errors.New("inbound event ignored")
	ErrBusy                = errors.New("inbound event is already processing")
	ErrUnauthorized        = errors.New("inbound authority is invalid")
	ErrNotFound            = errors.New("interaction resource not found")
	ErrConflict            = errors.New("interaction state conflict")
	ErrUnavailable         = errors.New("interaction dependency unavailable")
	ErrIdempotencyConflict = errors.New("interaction idempotency conflict")
	ErrVersionMismatch     = errors.New("interaction version mismatch")
	ErrProviderConflict    = errors.New("interaction provider state conflict")
	ErrProviderDeleted     = errors.New("interaction provider object is deleted")
	ErrAmbiguousEffect     = errors.New("interaction provider effect is ambiguous")
	ErrRepairRequired      = errors.New("interaction repair is required")
)

// ResponseError сохраняет безопасный пользовательский outcome отдельно от
// стабильной runtime-ошибки, используемой transport для HTTP status.
type ResponseError struct {
	cause   error
	message string
}

func (err ResponseError) Error() string           { return err.cause.Error() }
func (err ResponseError) Unwrap() error           { return err.cause }
func (err ResponseError) ResponseMessage() string { return err.message }

func WithResponse(cause error, message string) error {
	return ResponseError{cause: cause, message: message}
}
