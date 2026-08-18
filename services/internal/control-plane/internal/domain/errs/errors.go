// Package errs задаёт безопасные доменные ошибки control-plane.
package errs

import "errors"

type safeCoded interface {
	SafeCode() string
}

type safeCodedError struct {
	cause error
	code  string
}

func (err safeCodedError) Error() string    { return err.cause.Error() }
func (err safeCodedError) Unwrap() error    { return err.cause }
func (err safeCodedError) SafeCode() string { return err.code }

// WithSafeCode добавляет закрытый диагностический код без изменения типа
// доменной ошибки и без включения приватных значений в transport response.
func WithSafeCode(cause error, code string) error {
	if cause == nil || code == "" {
		return cause
	}
	return safeCodedError{cause: cause, code: code}
}

// SafeCode возвращает только явно назначенный доменным слоем код.
func SafeCode(err error) string {
	var coded safeCoded
	if errors.As(err, &coded) {
		return coded.SafeCode()
	}
	return ""
}

var (
	ErrInvalidInput        = errors.New("invalid control-plane input")
	ErrUnauthenticated     = errors.New("control-plane authentication required")
	ErrPermissionDenied    = errors.New("control-plane permission denied")
	ErrNotFound            = errors.New("control-plane resource not found")
	ErrStateConflict       = errors.New("control-plane state conflict")
	ErrIdempotencyConflict = errors.New("control-plane idempotency conflict")
	ErrAborted             = errors.New("control-plane operation aborted")
	ErrVersionMismatch     = errors.New("control-plane version mismatch")
	ErrFailedPrecondition  = errors.New("control-plane precondition failed")
	ErrDataLoss            = errors.New("control-plane stored data is corrupt")
	ErrUnavailable         = errors.New("control-plane dependency unavailable")
	ErrInternal            = errors.New("control-plane internal error")
)
