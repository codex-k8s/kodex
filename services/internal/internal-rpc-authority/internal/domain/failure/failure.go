package failure

import "errors"

// Kind identifies a stable domain failure without exposing a transport status or raw cause.
// Kind определяет устойчивый доменный отказ без раскрытия транспортного статуса или сырой причины.
type Kind string

// The closed failure kinds keep authority decisions distinct from infrastructure failures.
// Закрытые виды отказов отделяют решения о полномочиях от сбоев инфраструктуры.
const (
	// InvalidRequest rejects malformed or inconsistent input before an authority decision.
	// InvalidRequest отклоняет неверный или противоречивый ввод до решения о полномочиях.
	InvalidRequest Kind = "INVALID_REQUEST"
	// NotFound hides a missing resource without implying that another actor may access it.
	// NotFound скрывает отсутствующий ресурс и не означает доступность его другому субъекту.
	NotFound Kind = "NOT_FOUND"
	// Unauthenticated marks a caller whose required identity proof is absent or invalid.
	// Unauthenticated отмечает отсутствие или недействительность доказательства личности вызывающего.
	Unauthenticated Kind = "UNAUTHENTICATED"
	// PermissionDenied refuses an authenticated caller lacking the exact required grant.
	// PermissionDenied отказывает опознанному субъекту без точного требуемого допуска.
	PermissionDenied Kind = "PERMISSION_DENIED"
	// OperationNotAllowed rejects an operation outside the authority service's closed registry.
	// OperationNotAllowed отклоняет операцию вне закрытого реестра сервиса полномочий.
	OperationNotAllowed Kind = "OPERATION_NOT_ALLOWED"
	// AuthorityRejected records a definitive negative decision from the authority boundary.
	// AuthorityRejected фиксирует окончательное отрицательное решение границы полномочий.
	AuthorityRejected Kind = "AUTHORITY_REJECTED"
	// BindingMismatch rejects evidence bound to another workload, request or revision.
	// BindingMismatch отклоняет доказательство, привязанное к другой нагрузке, запросу или ревизии.
	BindingMismatch Kind = "BINDING_MISMATCH"
	// ReplayDetected rejects reuse of a proof that has already crossed its one-use boundary.
	// ReplayDetected отклоняет повторное использование доказательства с одноразовой границей.
	ReplayDetected Kind = "REPLAY_DETECTED"
	// SnapshotRejected refuses a snapshot that fails the authority service's pinned validation.
	// SnapshotRejected отклоняет снимок, не прошедший закреплённую проверку сервиса полномочий.
	SnapshotRejected Kind = "SNAPSHOT_REJECTED"
	// PersistenceUnavailable distinguishes a storage failure from a denial of authority.
	// PersistenceUnavailable отличает сбой хранилища от отказа в полномочиях.
	PersistenceUnavailable Kind = "PERSISTENCE_UNAVAILABLE"
	// Internal denotes a server failure that cannot be classified as a safe caller error.
	// Internal обозначает сбой сервера, не относящийся к безопасным ошибкам вызывающего.
	Internal Kind = "INTERNAL"
)

// Error carries a stable domain kind and a safe caller message while retaining
// an optional internal cause for errors.As/Is, not for transport serialization.
// RU: Error хранит устойчивый доменный вид и безопасное сообщение для вызывающего,
// сохраняя необязательную внутреннюю причину для errors.As/Is, а не для транспорта.
type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

// Error returns only the safe message and does not expose the wrapped cause.
// Error возвращает только безопасное сообщение и не раскрывает вложенную причину.
func (err *Error) Error() string {
	return err.Message
}

// Unwrap exposes the internal cause to local error inspection without changing the message.
// Unwrap открывает внутреннюю причину локальной проверке ошибок, не меняя сообщение.
func (err *Error) Unwrap() error {
	return err.Cause
}

// New constructs a typed domain failure without retaining an underlying cause.
// New создаёт типизированный доменный отказ без сохранения внутренней причины.
func New(kind Kind, message string) error {
	return &Error{Kind: kind, Message: message}
}

// Wrap constructs a typed failure whose cause remains available for local inspection.
// Wrap создаёт типизированный отказ с причиной, доступной локальной проверке.
func Wrap(kind Kind, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

// IsKind finds the first typed domain failure in the error chain and compares its kind.
// IsKind находит первый типизированный доменный отказ в цепочке и сравнивает его вид.
func IsKind(err error, kind Kind) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Kind == kind
}
