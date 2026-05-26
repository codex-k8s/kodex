// Package errs contains codex-hook-ingress domain errors.
package errs

// Code is a comparable domain error code used with errors.Is.
type Code string

// Error returns the stable domain error code.
func (code Code) Error() string {
	return string(code)
}

const (
	// ErrDependencyUnavailable means a required skeleton dependency is not composed.
	ErrDependencyUnavailable Code = "hook.dependency_unavailable"
	// ErrDuplicateConflict means the same event id was retried with another payload digest.
	ErrDuplicateConflict Code = "hook.duplicate_conflict"
	// ErrInvalidArgument means a normalized hook envelope violates domain invariants.
	ErrInvalidArgument Code = "hook.invalid_argument"
	// ErrInvalidBinding means source/run/session/slot binding is not accepted.
	ErrInvalidBinding Code = "hook.invalid_binding"
	// ErrPayloadRejected means sanitizer boundary rejected safe payload metadata.
	ErrPayloadRejected Code = "hook.payload_rejected"
	// ErrPayloadTooLarge means the normalized envelope exceeds configured schema limits.
	ErrPayloadTooLarge Code = "hook.payload_too_large"
	// ErrUnsupportedEvent means the hook event is outside the MVP set.
	ErrUnsupportedEvent Code = "hook.unsupported_event"
)
