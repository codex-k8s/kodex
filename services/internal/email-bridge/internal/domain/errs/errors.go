package errs

import "errors"

var (
	// Invalid marks malformed or inconsistent mailbox command input before an effect is allowed.
	// Invalid обозначает неверные или противоречивые входные данные почтовой команды до допуска эффекта.
	Invalid = errors.New("INVALID")
	// Denied marks a rejected transport identity, execution grant, or owner authorization decision.
	// Denied обозначает отклонённую транспортную идентичность, грант выполнения или решение владельца о полномочиях.
	Denied = errors.New("DENIED")
	// Gate marks an operation that still requires an authoritative human approval decision.
	// Gate обозначает операцию, для которой ещё требуется авторитетное решение человека о подтверждении.
	Gate = errors.New("GATE_REQUIRED")
	// Conflict marks a stale or incompatible receipt, scope, or idempotency state without granting a retry.
	// Conflict обозначает устаревшее или несовместимое состояние квитанции, области либо идемпотентности и не разрешает повтор.
	Conflict = errors.New("CONFLICT")
	// NotFound marks an absent configured mailbox, message, or receipt in the selected scope without revealing an alternative scope.
	// NotFound обозначает отсутствие настроенного ящика, письма или квитанции в выбранной области без раскрытия другой области.
	NotFound = errors.New("NOT_FOUND")
	// Unsupported marks an operation outside the configured mailbox capability set.
	// Unsupported обозначает операцию вне настроенного набора возможностей почтового ящика.
	Unsupported = errors.New("UNSUPPORTED")
	// Unavailable marks a failed dependency or unknown external outcome; it never proves that a send did not occur.
	// Unavailable обозначает отказ зависимости или неопределённый внешний исход и не доказывает, что отправка не состоялась.
	Unavailable = errors.New("UNAVAILABLE")
)
