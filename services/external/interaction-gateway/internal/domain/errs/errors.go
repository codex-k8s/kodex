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
