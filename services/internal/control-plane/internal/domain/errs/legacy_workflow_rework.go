package errs

import "errors"

var (
	// ErrLegacyReworkScopeUnsupported rejects operations outside the explicitly read-only external scope of an owner rework execution.
	// ErrLegacyReworkScopeUnsupported отклоняет операции вне явно read-only внешнего scope owner rework execution.
	ErrLegacyReworkScopeUnsupported = errors.Join(ErrConflict, errors.New("legacy workflow rework operation scope is unsupported"))
	// ErrLegacyReworkWrite preserves a state-conflict class while requiring explicit reconciliation of an earlier external write; a new lease never grants a retry.
	// ErrLegacyReworkWrite сохраняет класс state-conflict и требует сверки прежней внешней записи; новый lease не разрешает повтор.
	ErrLegacyReworkWrite = errors.Join(ErrConflict, errors.New("legacy workflow rework requires external write reconciliation"))
	// ErrLegacyReworkParentBusy identifies an executing coordinator boundary that the owner command must not silently cancel.
	// ErrLegacyReworkParentBusy обозначает исполняющуюся границу координатора, которую команда владельца не отменяет скрыто.
	ErrLegacyReworkParentBusy = errors.Join(ErrConflict, errors.New("legacy workflow coordinator is still executing"))
	// ErrLegacyReworkDeliveryBlocked keeps an undelivered result visible without treating an empty model queue as success.
	// ErrLegacyReworkDeliveryBlocked сохраняет видимый отказ доставки, не считая пустую очередь моделей успехом.
	ErrLegacyReworkDeliveryBlocked = errors.Join(ErrConflict, errors.New("legacy workflow rework result cannot be delivered"))
	// ErrLegacyReworkReadLimit preserves the existing graph/input readability bound instead of allocating an unreadable attempt.
	// ErrLegacyReworkReadLimit сохраняет существующий предел читаемости графа/ввода вместо создания нечитаемой попытки.
	ErrLegacyReworkReadLimit = errors.Join(ErrConflict, errors.New("legacy workflow rework exceeds its readable execution budget"))
)
