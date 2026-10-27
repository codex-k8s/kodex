package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryAssistantArchiveLock embeds an owner- and tenant-bound conversation row lock with
// session/context metadata and a nonterminal-run busy indicator. It does not itself check expected
// version or current project permissions.
// queryAssistantArchiveLock встраивает блокировку разговора точных владельца и организации с
// метаданными сессии и контекста и признаком незавершённого запуска. Сам запрос не проверяет
// ожидаемую версию и текущие права на проект.
//
//go:embed sql/assistant_archive_lock.sql
var queryAssistantArchiveLock string

// queryAssistantArchiveUpdate embeds rejection of editable plans and version-guarded archival of
// an ACTIVE/CLOSED conversation, setting a 30-day purge deadline. Both effects use the caller
// transaction; no immediate physical deletion occurs.
// queryAssistantArchiveUpdate встраивает отклонение редактируемых планов и архивирование
// ACTIVE/CLOSED разговора с проверкой версии и сроком очистки через 30 дней. Оба эффекта
// используют транзакцию вызывающего кода; немедленного физического удаления нет.
//
//go:embed sql/assistant_archive_update.sql
var queryAssistantArchiveUpdate string

// queryAssistantRestoreUpdate embeds the exact organization/ref/version transition from ARCHIVED
// to ACTIVE, clearing deletion and purge timestamps and incrementing version. Authorization is
// checked before execution.
// queryAssistantRestoreUpdate встраивает переход ARCHIVED в ACTIVE по точным организации, ссылке и
// версии, очищая время удаления и очистки и увеличивая версию. Авторизация проверяется до
// исполнения.
//
//go:embed sql/assistant_restore_update.sql
var queryAssistantRestoreUpdate string

// queryAssistantPurgeDelete embeds the owner-request purge function call with explicit
// organization, owner, conversation, version and actor arguments. Lifecycle cleanup belongs to
// that database function and the enclosing transaction.
// queryAssistantPurgeDelete встраивает вызов функции очистки по запросу владельца с явными
// организацией, владельцем, разговором, версией и субъектом. Очисткой связанных состояний
// управляют функция БД и общая транзакция.
//
//go:embed sql/assistant_purge_delete.sql
var queryAssistantPurgeDelete string

// assistantArchiveOwner holds the locked conversation projection, private conversation/project IDs
// and busy state for archive authorization and transition checks. It is transaction-local data,
// not a transferable permission.
// assistantArchiveOwner хранит заблокированное представление разговора, внутренние идентификаторы
// разговора и проекта и занятость для авторизации и проверки переходов архива. Это данные текущей
// транзакции, а не передаваемое разрешение.
type assistantArchiveOwner struct {
	conversation  entity.AssistantConversation
	id, projectID string
	busy          bool
}

// authorizeAssistantArchive validates the typed ref and expected-version presence, locks the
// actor-owned conversation and hides missing, cross-authority-project or inaccessible resources as
// NotFound. It requires organization.view or project.view; the caller checks the actual version
// and allowed lifecycle transition.
// authorizeAssistantArchive проверяет типизированную ссылку и наличие ожидаемой версии, блокирует
// разговор субъекта и скрывает отсутствующий, чужой проекту полномочий или недоступный ресурс как
// NotFound. Требует organization.view либо project.view; фактическую версию и допустимый переход
// проверяет вызывающий код.
func (repository *Repository) authorizeAssistantArchive(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (assistantArchiveOwner, error) {
	var owner assistantArchiveOwner
	payload, ok := input.Payload.(command.AssistantConversationArchiveInput)
	if !ok || payload.ConversationRef == "" || input.Mutation.ExpectedVersion == nil {
		return owner, errs.ErrInvalid
	}
	c := &owner.conversation
	err := tx.QueryRow(ctx, queryAssistantArchiveLock, current.organizationID, payload.ConversationRef, current.actorID).Scan(
		&c.Ref, &c.Title, &c.TitleSource, &c.TitleRevision, &c.ProjectRef, &c.SessionRef, &c.State, &c.Version,
		&c.Context.Route, &c.Context.EntityKind, &c.Context.EntityRef, &c.Context.EntityName, &c.Context.EntityVersion,
		&c.Context.AllowedOperations, &c.CreatedAt, &c.UpdatedAt, &owner.id, &owner.projectID, &owner.busy)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, errs.ErrNotFound
	}
	if err != nil {
		return owner, errs.ErrUnavailable
	}
	if current.authorityProjectID != "" && current.authorityProjectID != owner.projectID {
		return owner, errs.ErrNotFound
	}
	permission, target := "organization.view", organizationTarget(current.organizationRef)
	if c.ProjectRef != "" {
		permission, target = "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: c.ProjectRef}
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return owner, errs.ErrNotFound
	}
	return owner, nil
}

// archiveAssistantConversation authorizes and version-checks a conversation before archive,
// restore or purge. Archive rejects busy/already archived conversations; restore and purge require
// ARCHIVED, and purge must return true. It returns the appropriate result/summary with
// SYSTEM_ASSISTANT_CHANGED, leaving atomic event persistence and commit to the outer command
// processor.
// archiveAssistantConversation проверяет доступ и версию разговора перед архивированием,
// восстановлением либо очисткой. Архивирование отклоняет занятый или уже архивный разговор;
// восстановление и очистка требуют ARCHIVED, очистка должна вернуть true. Возвращает
// соответствующий результат и описание с SYSTEM_ASSISTANT_CHANGED; атомарное сохранение события и
// завершение транзакции выполняет внешний обработчик.
func (repository *Repository) archiveAssistantConversation(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	owner, err := repository.authorizeAssistantArchive(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	c := owner.conversation
	if c.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	summary := "i18n:ASSISTANT_CONVERSATION_ARCHIVED"
	result := command.Result{}
	switch input.Kind {
	case command.ArchiveAssistantConversation:
		if owner.busy || c.State == "ARCHIVED" {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := tx.QueryRow(ctx, queryAssistantArchiveUpdate, current.organizationID, c.Ref, c.Version).Scan(&c.Version, &c.UpdatedAt); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		c.State = "ARCHIVED"
		result.Conversation = &c
	case command.RestoreAssistantConversation:
		if c.State != "ARCHIVED" {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := tx.QueryRow(ctx, queryAssistantRestoreUpdate, current.organizationID, c.Ref, c.Version).Scan(&c.Version, &c.UpdatedAt); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		c.State = "ACTIVE"
		result.Conversation = &c
		summary = "i18n:ASSISTANT_CONVERSATION_RESTORED"
	case command.PurgeAssistantConversation:
		if c.State != "ARCHIVED" {
			return commandOutcome{}, errs.ErrConflict
		}
		var purged bool
		if err := tx.QueryRow(ctx, queryAssistantPurgeDelete, current.organizationID, current.actorID, c.Ref, c.Version, current.actorID).Scan(&purged); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if !purged {
			return commandOutcome{}, errs.ErrConflict
		}
		summary = "i18n:ASSISTANT_CONVERSATION_PURGED"
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	return commandOutcome{result: result, projectID: owner.projectID, projectRef: c.ProjectRef,
		resourceKind: "ASSISTANT_CONVERSATION", resourceRef: c.Ref, summary: summary, platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}
