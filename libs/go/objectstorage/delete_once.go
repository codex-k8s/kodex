package objectstorage

import "context"

// SingleAttemptDeleter deletes one exact provider version with no adapter/SDK retry or HTTP redirect. Callers
// must durably admit that attempt before invocation and treat every uncertain response as requiring
// authoritative Head reconciliation. The port grants no tenant/key authority and never selects a
// latest version. Ordinary Store.Delete keeps its existing retry and compatibility contract.
// SingleAttemptDeleter удаляет одну точную provider version без adapter/SDK retry либо HTTP redirect. Caller обязан
// устойчиво допустить attempt до вызова; неизвестный ответ требует authoritative Head reconciliation.
// Порт не выдаёт tenant/key authority и не выбирает latest version. Обычный Store.Delete сохраняет
// прежний retry и compatibility contract.
type SingleAttemptDeleter interface {
	// CheckDeleteOnce attests the concrete adapter can suppress retry and redirect without IO.
	// CheckDeleteOnce проверяет, что concrete adapter подавляет retry и redirect без IO.
	CheckDeleteOnce(context.Context) error
	// DeleteOnce performs at most one physical HTTP request for an owner-resolved key/version. A
	// returned error does not establish whether deletion occurred; callers must not fall back to Delete.
	// DeleteOnce делает не более одного physical HTTP request для owner-resolved key/version. Ошибка
	// не доказывает исход удаления; fallback на Delete запрещён.
	DeleteOnce(context.Context, string, string) error
}
