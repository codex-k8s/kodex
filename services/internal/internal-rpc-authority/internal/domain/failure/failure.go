package failure

import "errors"

type Kind string

const (
	InvalidRequest         Kind = "INVALID_REQUEST"
	NotFound               Kind = "NOT_FOUND"
	Unauthenticated        Kind = "UNAUTHENTICATED"
	PermissionDenied       Kind = "PERMISSION_DENIED"
	OperationNotAllowed    Kind = "OPERATION_NOT_ALLOWED"
	AuthorityRejected      Kind = "AUTHORITY_REJECTED"
	BindingMismatch        Kind = "BINDING_MISMATCH"
	ReplayDetected         Kind = "REPLAY_DETECTED"
	SnapshotRejected       Kind = "SNAPSHOT_REJECTED"
	PersistenceUnavailable Kind = "PERSISTENCE_UNAVAILABLE"
	Internal               Kind = "INTERNAL"
)

type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

func (err *Error) Error() string {
	return err.Message
}

func (err *Error) Unwrap() error {
	return err.Cause
}

func New(kind Kind, message string) error {
	return &Error{Kind: kind, Message: message}
}

func Wrap(kind Kind, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

func IsKind(err error, kind Kind) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Kind == kind
}
