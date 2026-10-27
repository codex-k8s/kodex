package platform

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
)

// queryAssistantPurgeDue embeds the trusted global retention sweep of due ARCHIVED conversations
// using bounded SKIP LOCKED candidates and the owner purge function. Returned
// owner/project/version metadata supports atomic event snapshots, not caller-selected tenant
// authority.
// queryAssistantPurgeDue встраивает доверенную глобальную очистку просроченных ARCHIVED разговоров
// с ограниченным SKIP LOCKED выбором и функцией очистки владельца. Возвращённые данные
// организации, проекта и версии нужны атомарным снимкам событий, а не назначению клиентом
// tenant-полномочий.
//
//go:embed sql/assistant_purge_due.sql
var queryAssistantPurgeDue string

// PurgeDueAssistantConversations accepts batches from 1 to 100, purges due conversations and
// validates every returned deletion tuple before emitting PURGED platform snapshots in the same
// transaction. It commits only after all rows/events succeed; invalid input/tuples return
// pgx.ErrNoRows, while storage errors propagate without domain remapping.
// PurgeDueAssistantConversations принимает пакеты от 1 до 100, очищает просроченные разговоры и
// проверяет каждый возвращённый кортеж удаления до записи PURGED снимков платформенных событий в
// той же транзакции. Завершает её после всех строк и событий; неверный вход либо кортеж даёт
// pgx.ErrNoRows, ошибки хранилища передаются без доменного преобразования.
func (repository *Repository) PurgeDueAssistantConversations(ctx context.Context, batchSize int32) error {
	if batchSize < 1 || batchSize > 100 {
		return pgx.ErrNoRows
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryAssistantPurgeDue, pgx.StrictNamedArgs{
		"batch_size": batchSize,
	})
	if err != nil {
		return err
	}
	// purgedConversation stores the database-returned organization, project, ref and pre-purge version
	// until all result rows close. These local fields bind event emission to the actual retention
	// deletion, not to an external request.
	// purgedConversation хранит возвращённые БД организацию, проект, ссылку и версию до очистки, пока
	// не закрыты все строки результата. Эти локальные поля связывают событие с фактической очисткой по
	// сроку, а не внешним запросом.
	type purgedConversation struct {
		organizationID, organizationRef, projectRef, ref string
		version                                          int64
	}
	purged := make([]purgedConversation, 0, batchSize)
	for rows.Next() {
		var item purgedConversation
		var deleted bool
		if err := rows.Scan(&item.organizationID, &item.organizationRef, &item.projectRef,
			&item.ref, &item.version, &deleted); err != nil {
			rows.Close()
			return err
		}
		if item.organizationID == "" || item.organizationRef == "" || item.ref == "" || item.version < 1 || !deleted {
			rows.Close()
			return pgx.ErrNoRows
		}
		purged = append(purged, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range purged {
		current := scope{
			organizationID: item.organizationID, organizationRef: item.organizationRef,
			correlationRef: item.ref,
		}
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "SYSTEM_ASSISTANT_CHANGED",
			item.projectRef, item.ref, "i18n:ASSISTANT_CONVERSATION_PURGED", item.version, "PURGED"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
