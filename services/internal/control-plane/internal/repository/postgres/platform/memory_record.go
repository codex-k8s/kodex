package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// memoryRecordRow combines the public memory projection with internal record, project, agent and
// revision IDs needed by transactional access checks and writes. These IDs are not client
// authority.
// memoryRecordRow объединяет публичное представление памяти с внутренними идентификаторами записи,
// проекта, агента и ревизии для проверок доступа и записи в транзакции. Эти идентификаторы не
// подтверждают полномочия клиента.
type memoryRecordRow struct {
	entity.KodexMemoryRecord
	id, projectID, agentID, currentRevisionID string
}

// memoryReceiptMetadata copies the result, memory record and current revision before removing the
// revision summary from a receipt. Other referenced data remains shared; replay must reload
// content and recheck access.
// memoryReceiptMetadata копирует результат, запись памяти и текущую ревизию, затем удаляет текст
// summary из квитанции. Остальные ссылочные данные остаются общими; повторная выдача требует
// загрузки содержимого и новой проверки доступа.
func memoryReceiptMetadata(result command.Result) command.Result {
	if result.MemoryRecord != nil && result.MemoryRecord.CurrentRevision != nil {
		record := *result.MemoryRecord
		revision := *record.CurrentRevision
		revision.Summary = ""
		record.CurrentRevision = &revision
		result.MemoryRecord = &record
	}
	return result
}

// refreshMemoryReceipt rehydrates the exact revision referenced by a nonnil memory receipt after
// checking current record access and source-run visibility. It propagates PURGED state from the
// current record without replacing all historical metadata; it neither commits nor grants access
// from the receipt.
// refreshMemoryReceipt восстанавливает точную ревизию непустой квитанции памяти после проверки
// доступа к текущей записи и видимости исходного запуска. Переносит состояние PURGED из текущей
// записи, сохраняя прочие исторические метаданные; не завершает транзакцию и не выводит полномочия
// из квитанции.
func (repository *Repository) refreshMemoryReceipt(ctx context.Context, tx pgx.Tx, current scope, result *command.Result) error {
	if result.MemoryRecord == nil {
		return nil
	}
	if result.MemoryRecord.CurrentRevision == nil {
		return errs.ErrUnavailable
	}
	record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, result.MemoryRecord.Ref))
	if err != nil {
		return err
	}
	if err := repository.memoryRecordAccess(ctx, tx, current, record, false); err != nil {
		return err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, queryMemoryRevisionGet, current.organizationID, record.Ref, result.MemoryRecord.CurrentRevision.Ref).Scan(&raw); err != nil {
		return errs.ErrUnavailable
	}
	var revision entity.MemoryRecordRevision
	if json.Unmarshal(raw, &revision) != nil {
		return errs.ErrUnavailable
	}
	if revision.Provenance.SourceRef != "" {
		if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: revision.Provenance.SourceRef}); err != nil {
			return err
		}
	}
	result.MemoryRecord.CurrentRevision = &revision
	if record.State == "PURGED" {
		result.MemoryRecord.State = "PURGED"
	}
	return nil
}

// scanMemoryRecord scans private IDs and decodes the JSON domain projection. Missing rows map to
// NotFound and backend or decoding failures to Unavailable; an error may accompany a partially
// populated row.
// scanMemoryRecord читает внутренние идентификаторы и декодирует доменное представление из JSON.
// Отсутствие строки преобразуется в NotFound, ошибки хранилища или декодирования — в Unavailable;
// при ошибке строка может быть частично заполнена.
func scanMemoryRecord(row rowScanner) (memoryRecordRow, error) {
	var result memoryRecordRow
	var raw []byte
	if err := row.Scan(&result.id, &result.projectID, &result.agentID, &result.currentRevisionID, &raw); errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	} else if err != nil {
		return result, errs.ErrUnavailable
	}
	if json.Unmarshal(raw, &result.KodexMemoryRecord) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// memoryRecordAccess enforces the authority-project boundary and project view/manage permission,
// plus agent view/manage when the record belongs to an agent. The write flag selects permissions;
// content lifecycle and retention checks remain with callers.
// memoryRecordAccess проверяет границу проекта полномочий и разрешение просмотра либо изменения
// проекта, а для памяти агента — также агента. Флаг write выбирает разрешения; проверки жизненного
// цикла содержимого и срока хранения остаются у вызывающего кода.
func (repository *Repository) memoryRecordAccess(ctx context.Context, tx pgx.Tx, current scope, record memoryRecordRow, write bool) error {
	if current.authorityProjectID != "" && current.authorityProjectID != record.projectID {
		return errs.ErrForbidden
	}
	permission, kind, ref := "project.view", "PROJECT", record.ProjectRef
	if record.AgentRef != "" {
		permission, kind, ref = "agent.view", "AGENT", record.AgentRef
	}
	if write {
		permission = strings.TrimSuffix(permission, "view") + "manage"
	}
	if err := repository.requireAccess(ctx, tx, current, permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: kind, ResourceRef: ref}); err != nil {
		return err
	}
	return nil
}

// GetMemoryRecord reads a tenant-scoped memory record in a five-second repeatable-read, read-only
// transaction, checks record access and source-run visibility, then commits. Scope errors
// propagate and transaction failures map to Unavailable; deferred rollback remains owned here.
// GetMemoryRecord читает запись памяти организации в read-only транзакции RepeatableRead с
// бюджетом пять секунд, проверяет доступ к записи и видимость исходного запуска, затем завершает
// транзакцию. Ошибки определения области передаются выше, ошибки транзакции преобразуются в
// Unavailable; отложенный rollback выполняется здесь.
func (repository *Repository) GetMemoryRecord(ctx context.Context, principal value.Principal, ref string) (entity.KodexMemoryRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.KodexMemoryRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, ref))
	if err != nil {
		return record.KodexMemoryRecord, err
	}
	if err := repository.memoryRecordAccess(ctx, tx, current, record, false); err != nil {
		return entity.KodexMemoryRecord{}, err
	}
	if record.CurrentRevision != nil && record.CurrentRevision.Provenance.SourceRef != "" {
		if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: record.CurrentRevision.Provenance.SourceRef}); err != nil {
			return entity.KodexMemoryRecord{}, err
		}
	}
	if tx.Commit(ctx) != nil {
		return entity.KodexMemoryRecord{}, errs.ErrUnavailable
	}
	return record.KodexMemoryRecord, nil
}

// changeMemoryRecord applies typed create, revise, archive, restore or purge operations in the
// supplied transaction. Existing records are locked and version checked; revisions validate
// bounded UTF-8 content and retention, while nonactive states disable bindings and purge erases
// summaries. It returns a readback outcome without committing; outer command processing owns
// authorization and atomic effects.
// changeMemoryRecord выполняет типизированные создание, изменение ревизии, архивирование,
// восстановление или очистку в переданной транзакции. Существующая запись блокируется и
// проверяется по версии; ревизии проверяют ограничения UTF-8 и срока хранения, неактивные
// состояния отключают привязки, очистка удаляет summary. Возвращает результат повторного чтения
// без завершения транзакции; внешний обработчик отвечает за авторизацию и атомарность эффектов.
func (repository *Repository) changeMemoryRecord(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.MemoryRecordInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	create := input.Kind == command.CreateMemoryRecord
	var id, ref, state string
	var version int64
	if create {
		ref, _ = newRef("memr")
		if err := tx.QueryRow(ctx, queryMemoryRecordInsert, current.organizationID, ref, payload.ProjectRef, payload.AgentRef, current.actorID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		ref = payload.RecordRef
		if err := tx.QueryRow(ctx, queryMemoryRecordLock, current.organizationID, ref).Scan(&id, &version, &state); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if input.Mutation.ExpectedVersion == nil || version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if state == "PURGED" {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	if create || input.Kind == command.ReviseMemoryRecord {
		specification := payload.Specification
		if strings.TrimSpace(specification.Title) == "" || len([]rune(specification.Title)) > 160 || !utf8.ValidString(specification.Title) ||
			strings.TrimSpace(specification.Summary) == "" || len(specification.Summary) > 65536 || !utf8.ValidString(specification.Summary) ||
			strings.ContainsRune(specification.Title+specification.Summary, 0) || !specification.RetentionUntil.After(time.Now()) || specification.RetentionUntil.After(time.Now().Add(366*24*time.Hour)) {
			return commandOutcome{}, errs.ErrInvalid
		}
		raw, _ := json.Marshal(specification)
		digest := sha256.Sum256(raw)
		revisionRef, _ := newRef("memv")
		var revisionID string
		if err := tx.QueryRow(ctx, queryMemoryRevisionInsert, pgx.StrictNamedArgs{"organization_id": current.organizationID, "record_id": id, "revision_ref": revisionRef,
			"title": specification.Title, "summary": specification.Summary, "digest": hex.EncodeToString(digest[:]), "source_run_ref": specification.SourceRunRef,
			"retention_until": specification.RetentionUntil, "actor_id": current.actorID}).Scan(&revisionID); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		increment := int64(1)
		if create {
			increment = 0
		}
		if _, err := tx.Exec(ctx, queryMemoryRecordSetRevision, current.organizationID, id, revisionID, increment); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else {
		target := ""
		switch input.Kind {
		case command.ArchiveMemoryRecord:
			if state != "ACTIVE" {
				return commandOutcome{}, errs.ErrConflict
			}
			target = "ARCHIVED"
		case command.RestoreMemoryRecord:
			if state != "ARCHIVED" {
				return commandOutcome{}, errs.ErrConflict
			}
			record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, ref))
			if err != nil {
				return commandOutcome{}, err
			}
			if record.CurrentRevision == nil || record.CurrentRevision.Redacted {
				return commandOutcome{}, errs.ErrConflict
			}
			target = "ACTIVE"
		case command.PurgeMemoryRecord:
			if state != "ARCHIVED" {
				return commandOutcome{}, errs.ErrConflict
			}
			target = "PURGED"
		default:
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryMemoryRecordSetState, current.organizationID, id, target); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if target != "ACTIVE" {
			if _, err := tx.Exec(ctx, queryMemoryRecordDisableBindings, current.organizationID, id); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
		if target == "PURGED" {
			if _, err := tx.Exec(ctx, queryMemoryRecordPurge, current.organizationID, id); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
	}
	record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, ref))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{MemoryRecord: &record.KodexMemoryRecord}, resourceKind: "MEMORY_RECORD", resourceRef: ref,
		projectID: record.projectID, projectRef: record.ProjectRef, summary: "i18n:MEMORY_RECORD_CHANGED"}, nil
}
