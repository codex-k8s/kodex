package codex

import (
	"errors"
)

// errResumeSourceInvalid is the fixed public sentinel for a rejected resumed-history source.
// errResumeSourceInvalid — фиксированный публичный sentinel отклонённого источника resumed history.
var errResumeSourceInvalid = errors.New("Codex resumed rollout source is invalid")

// resumeSourceFailure retains a private diagnostic detail behind a fixed public error.
// The string field itself is not an allowlist; downstream diagnostic projection validates its value.
// resumeSourceFailure сохраняет приватный diagnostic detail за фиксированной публичной ошибкой.
// Само строковое поле не является allowlist; значение проверяет downstream diagnostic projection.
type resumeSourceFailure struct {
	detail string
}

// Error returns only the canonical source sentinel and never interpolates the private detail.
// Error возвращает лишь канонический source sentinel, не подставляя приватный detail.
func (failure *resumeSourceFailure) Error() string { return errResumeSourceInvalid.Error() }

// Unwrap preserves errors.Is classification against the canonical source sentinel.
// Unwrap сохраняет классификацию errors.Is по каноническому source sentinel.
func (failure *resumeSourceFailure) Unwrap() error { return errResumeSourceInvalid }
