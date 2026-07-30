package failure

import "errors"

// Kind задаёт устойчивый тип доменного отказа.
type Kind string

// Закрытый набор причин отказа доменного слоя.
const (
	// InvalidRequest и последующие значения образуют закрытый набор отказов.
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

// Error хранит безопасное сообщение и необязательную внутреннюю причину.
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

// New создаёт доменный отказ без внутренней причины.
func New(kind Kind, message string) error {
	return &Error{Kind: kind, Message: message}
}

// Wrap создаёт доменный отказ с внутренней причиной.
func Wrap(kind Kind, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

// IsKind проверяет тип отказа во всей цепочке ошибок.
func IsKind(err error, kind Kind) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Kind == kind
}
