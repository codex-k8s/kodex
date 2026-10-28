// Package availability preserves a closed broker failure class while retaining only the caller's safe public error.
// Package availability сохраняет закрытый класс отказа брокера и только безопасную публичную ошибку вызывающего.
package availability

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/nats-io/nats.go"
)

// failure retains no native error, broker response, subject or credential after classification.
// failure после классификации не сохраняет исходную ошибку, ответ брокера, subject либо credentials.
type failure struct {
	public error
	class  string
}

// Error exposes only the normalized class, independently of the public sentinel's text.
// Error выдаёт только нормализованный класс независимо от текста публичного sentinel.
func (value *failure) Error() string { return "broker operation unavailable: " + value.class }

// Format prevents debug formatting from traversing even the safe public error field.
// Format запрещает debug-форматированию обход даже поля безопасной публичной ошибки.
func (value *failure) Format(state fmt.State, _ rune) { _, _ = io.WriteString(state, value.Error()) }

// Unwrap preserves the preexisting public errors.Is contract without exposing the discarded native cause.
// Unwrap сохраняет прежний публичный errors.Is-контракт, не раскрывая отброшенную исходную причину.
func (value *failure) Unwrap() error { return value.public }

// Wrap classifies a native error before discarding it; the public error alone determines caller semantics.
// Wrap классифицирует исходную ошибку перед отбрасыванием; семантику вызывающего определяет только публичная ошибка.
func Wrap(public, cause error) error { return &failure{public: public, class: Class(cause)} }

// Invalid marks an invalid broker record without retaining the rejected record or adding an I/O operation.
// Invalid помечает неверную запись брокера, не сохраняя отвергнутую запись и не добавляя I/O.
func Invalid(public error) error { return &failure{public: public, class: "record_invalid"} }

// Class recognizes only typed context/NATS failures or this package's sealed value; error strings are never inspected.
// Class распознаёт только типизированные context/NATS-ошибки либо закрытое значение пакета; тексты ошибок не анализируются.
func Class(err error) string {
	var retained *failure
	if errors.As(err, &retained) {
		return retained.class
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, nats.ErrConnectionClosed):
		return "connection_closed"
	case errors.Is(err, nats.ErrDisconnected):
		return "disconnected"
	case errors.Is(err, nats.ErrNoResponders):
		return "no_responders"
	case errors.Is(err, nats.ErrTimeout):
		return "timeout"
	case errors.Is(err, nats.ErrAuthorization), errors.Is(err, nats.ErrAuthExpired), errors.Is(err, nats.ErrAuthRevoked), errors.Is(err, nats.ErrAccountAuthExpired), errors.Is(err, nats.ErrPermissionViolation):
		return "authorization"
	default:
		return "unknown"
	}
}
