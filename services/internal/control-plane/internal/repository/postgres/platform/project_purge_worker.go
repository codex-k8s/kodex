package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryProjectPurgeDue selects due TRASHED projects with SKIP LOCKED, advances
// PURGE_PENDING/version, inserts pending receipts and cancels READY archive tasks in one
// statement.
// queryProjectPurgeDue выбирает просроченные TRASHED проекты через SKIP LOCKED, меняет
// PURGE_PENDING/version, создаёт pending receipts и отменяет READY archive tasks одним statement.
//
//go:embed sql/project_purge__due.sql
var queryProjectPurgeDue string

// queryProjectPurgePending lists bounded pending receipts only when the exact tenant/project/ref
// still identifies a PURGE_PENDING project.
// queryProjectPurgePending выдаёт ограниченный список pending receipts только при точном
// tenant/project/ref проекта в PURGE_PENDING.
//
//go:embed sql/project_purge__pending.sql
var queryProjectPurgePending string

// queryProjectPurgeInventory reads the dedicated external inventory function for exact
// organization/project IDs with stable kind/target/version ordering.
// queryProjectPurgeInventory читает отдельную функцию внешнего inventory для точных
// organization/project IDs с устойчивой сортировкой kind/target/version.
//
//go:embed sql/project_purge__inventory.sql
var queryProjectPurgeInventory string

// queryProjectPurgeForeignObject checks artifact, avatar and archive references to an object key
// outside the candidate organization/project; a true result prevents treating the key as
// exclusive.
// queryProjectPurgeForeignObject проверяет ссылки artifact, avatar и archive на ключ объекта вне
// organization/project кандидата; true запрещает считать ключ эксклюзивным.
//
//go:embed sql/project_purge__foreign_object.sql
var queryProjectPurgeForeignObject string

// queryProjectPurgeArchiveOwner checks archive/task ownership through tenant, session and
// PURGE_PENDING project, optional exact object version and approved project/system key prefixes.
// queryProjectPurgeArchiveOwner проверяет владение archive/task через tenant, session и
// PURGE_PENDING проект, необязательную точную версию объекта и допустимые project/system префиксы
// ключа.
//
//go:embed sql/project_purge__archive_owner.sql
var queryProjectPurgeArchiveOwner string

// queryProjectPurgeGlobalPVCOwner checks a SYSTEM conversation/session already promoted into the
// same frozen project before considering its formerly global PVC owned.
// queryProjectPurgeGlobalPVCOwner проверяет SYSTEM conversation/session, уже перенесённые в тот же
// замороженный проект, перед признанием прежнего global PVC принадлежащим проекту.
//
//go:embed sql/project_purge__global_pvc_owner.sql
var queryProjectPurgeGlobalPVCOwner string

// queryProjectPurgeSubjectArtifactOwner requires exact nonempty object version and the
// artifact/run/session/SYSTEM-conversation/project chain; the subject path itself is not
// authority.
// queryProjectPurgeSubjectArtifactOwner требует точную непустую версию объекта и цепочку
// artifact/run/session/SYSTEM-conversation/project; сам subject path не доказывает полномочия.
//
//go:embed sql/project_purge__subject_artifact_owner.sql
var queryProjectPurgeSubjectArtifactOwner string

// queryProjectPurgeLockReceipt locks the exact organization/project/ref purge receipt so
// finalization observes a stable lifecycle state.
// queryProjectPurgeLockReceipt блокирует purge receipt точных organization/project/ref для чтения
// устойчивого состояния при финализации.
//
//go:embed sql/project_purge__lock_receipt.sql
var queryProjectPurgeLockReceipt string

// queryProjectPurgeLockProject locks the candidate project only in PURGE_PENDING and returns its
// authoritative reference and version.
// queryProjectPurgeLockProject блокирует проект кандидата только в PURGE_PENDING и возвращает его
// авторитетные reference и version.
//
//go:embed sql/project_purge__lock_project.sql
var queryProjectPurgeLockProject string

// queryProjectPurgeActiveTasks counts READY or CLAIMED archive tasks in the candidate
// tenant/project as blockers to inventory or finalization.
// queryProjectPurgeActiveTasks считает READY либо CLAIMED archive tasks области tenant/project
// кандидата как блокеры inventory либо финализации.
//
//go:embed sql/project_purge__active_tasks.sql
var queryProjectPurgeActiveTasks string

// queryProjectPurgeDigest asks the authoritative inventory-digest function for the exact
// tenant/project; Go validates the returned digest shape before use.
// queryProjectPurgeDigest вызывает авторитетную функцию inventory digest для точных
// tenant/project; Go проверяет форму результата до использования.
//
//go:embed sql/project_purge__digest.sql
var queryProjectPurgeDigest string

// queryProjectPurgeMarkObjectsCleared records OBJECTS_CLEARED with the authoritative inventory
// digest only on the exact pending organization/project receipt.
// queryProjectPurgeMarkObjectsCleared фиксирует OBJECTS_CLEARED с авторитетным inventory digest
// только в точном pending receipt organization/project.
//
//go:embed sql/project_purge__mark_objects_cleared.sql
var queryProjectPurgeMarkObjectsCleared string

// queryProjectPurgeDeleteDatabase calls the dedicated database purge function with exact
// organization, project and inventory digest and returns its deletion count.
// queryProjectPurgeDeleteDatabase вызывает отдельную функцию очистки БД с точными organization,
// project и inventory digest и возвращает число удалений.
//
//go:embed sql/project_purge__delete_database.sql
var queryProjectPurgeDeleteDatabase string

// ProjectPurgeCandidate carries database IDs and public references together for one pending
// tenant/project purge; values must originate from the authoritative candidate read.
// ProjectPurgeCandidate переносит database IDs и публичные ссылки одного pending tenant/project
// purge; значения должны происходить из авторитетного чтения кандидатов.
type ProjectPurgeCandidate struct {
	OrganizationID, OrganizationRef, ProjectID, ProjectRef string
}

// ProjectPurgeExternalItem identifies an inventory object by kind, target and version; exact
// ordered tuples are compared again before database finalization.
// ProjectPurgeExternalItem задаёт объект inventory через kind, target и version; перед
// финализацией БД точные упорядоченные tuples сравниваются повторно.
type ProjectPurgeExternalItem struct {
	Kind, Target, Version string
}

// PromoteDueProjectPurges accepts batches of one to sixteen, validates promoted project/receipt
// counters and emits PROJECT_CHANGED snapshots in the same transaction before commit.
// PromoteDueProjectPurges принимает пачки от одного до шестнадцати, проверяет счётчики
// project/receipt и записывает PROJECT_CHANGED snapshots в той же транзакции до commit.
func (repository *Repository) PromoteDueProjectPurges(ctx context.Context, limit int32) error {
	if limit < 1 || limit > 16 {
		return errs.ErrInvalid
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// promotedProject holds validated tenant/project identity and the newly promoted project version
	// for transactional event emission after closing query rows.
	// promotedProject хранит проверенные tenant/project identity и новую версию проекта для записи
	// событий в транзакции после закрытия query rows.
	type promotedProject struct {
		organizationID, organizationRef, projectID, projectRef string
		version                                                int64
	}
	rows, err := tx.Query(ctx, queryProjectPurgeDue, pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return errs.ErrUnavailable
	}
	promoted := make([]promotedProject, 0, limit)
	for rows.Next() {
		var item promotedProject
		var receiptCount, cancelledTaskCount int64
		if err := rows.Scan(&item.organizationID, &item.organizationRef, &item.projectID,
			&item.projectRef, &item.version, &receiptCount, &cancelledTaskCount); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		if item.organizationID == "" || item.organizationRef == "" || item.projectID == "" ||
			item.projectRef == "" || item.version < 1 || receiptCount < 1 || cancelledTaskCount < 0 {
			rows.Close()
			return errs.ErrConflict
		}
		promoted = append(promoted, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return errs.ErrUnavailable
	}
	rows.Close()
	for _, item := range promoted {
		current := scope{
			organizationID: item.organizationID, organizationRef: item.organizationRef,
			correlationRef: item.projectRef,
		}
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "PROJECT_CHANGED", item.projectRef,
			item.projectRef, "i18n:PROJECT_PURGE_REQUESTED", item.version, "PURGE_PENDING"); err != nil {
			return errs.ErrUnavailable
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// ListPendingProjectPurges returns one to sixteen ordered pending candidates, preserving an
// allocated empty slice and mapping query, scan and iteration failures to unavailable.
// ListPendingProjectPurges возвращает от одного до шестнадцати упорядоченных pending кандидатов,
// сохраняя выделенный пустой срез и преобразуя ошибки запроса, scan и итерации в unavailable.
func (repository *Repository) ListPendingProjectPurges(ctx context.Context, limit int32) ([]ProjectPurgeCandidate, error) {
	if limit < 1 || limit > 16 {
		return nil, errs.ErrInvalid
	}
	rows, err := repository.pool.Query(ctx, queryProjectPurgePending, pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]ProjectPurgeCandidate, 0, limit)
	for rows.Next() {
		var item ProjectPurgeCandidate
		if err := rows.Scan(&item.OrganizationID, &item.OrganizationRef, &item.ProjectID, &item.ProjectRef); err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// ProjectPurgeInventory rejects recovery blockers and active archive tasks before reading the
// ordered external inventory; it lists targets without deleting external resources.
// ProjectPurgeInventory отклоняет recovery blockers и активные archive tasks перед чтением
// упорядоченного внешнего inventory; перечисляет цели без удаления внешних ресурсов.
func (repository *Repository) ProjectPurgeInventory(ctx context.Context, candidate ProjectPurgeCandidate) ([]ProjectPurgeExternalItem, error) {
	var blocked bool
	if err := repository.pool.QueryRow(ctx, queryProjectPurgeRecoveryBlocker, pgx.StrictNamedArgs{"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID}).Scan(&blocked); err != nil {
		return nil, errs.ErrUnavailable
	}
	if blocked {
		return nil, errs.ErrConflict
	}
	var activeTasks int
	if err := repository.pool.QueryRow(ctx, queryProjectPurgeActiveTasks,
		candidate.OrganizationID, candidate.ProjectID).Scan(&activeTasks); err != nil {
		return nil, errs.ErrUnavailable
	}
	if activeTasks != 0 {
		return nil, errs.ErrConflict
	}
	rows, err := repository.pool.Query(ctx, queryProjectPurgeInventory, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]ProjectPurgeExternalItem, 0)
	for rows.Next() {
		var item ProjectPurgeExternalItem
		if err := rows.Scan(&item.Kind, &item.Target, &item.Version); err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// ProjectPurgeObjectIsShared checks authoritative references outside the candidate tenant/project
// for the exact object key, returning unavailable rather than a successful negative on query
// failure.
// ProjectPurgeObjectIsShared проверяет авторитетные ссылки вне tenant/project кандидата для
// точного ключа объекта; ошибка запроса даёт unavailable, а не успешное отрицание.
func (repository *Repository) ProjectPurgeObjectIsShared(ctx context.Context, candidate ProjectPurgeCandidate, key string) (bool, error) {
	var shared bool
	err := repository.pool.QueryRow(ctx, queryProjectPurgeForeignObject, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID, "object_key": key,
	}).Scan(&shared)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return shared, nil
}

// ProjectPurgeArchiveIsOwned uses the archive-specific owner query with the exact candidate and
// object tuple; an empty version retains that query's documented optional-version semantics.
// ProjectPurgeArchiveIsOwned использует отдельный archive owner query с точными candidate и object
// tuple; пустая версия сохраняет необязательность версии именно этого запроса.
func (repository *Repository) ProjectPurgeArchiveIsOwned(ctx context.Context, candidate ProjectPurgeCandidate, item ProjectPurgeExternalItem) (bool, error) {
	return repository.projectPurgeVersionedObjectIsOwned(ctx, candidate, item, queryProjectPurgeArchiveOwner)
}

// ProjectPurgeSubjectArtifactIsOwned uses the subject-artifact owner query requiring a nonempty
// exact object version and full SYSTEM promotion lineage; it does not infer ownership from the key
// alone.
// ProjectPurgeSubjectArtifactIsOwned использует subject-artifact owner query с непустой точной
// версией объекта и полной SYSTEM promotion lineage; не выводит владение только из ключа.
func (repository *Repository) ProjectPurgeSubjectArtifactIsOwned(ctx context.Context, candidate ProjectPurgeCandidate, item ProjectPurgeExternalItem) (bool, error) {
	return repository.projectPurgeVersionedObjectIsOwned(ctx, candidate, item, queryProjectPurgeSubjectArtifactOwner)
}

// ProjectPurgeGlobalPVCIsOwned checks the exact session and candidate project through the
// specialized SYSTEM-conversation SQL without treating a PVC name as ownership proof.
// ProjectPurgeGlobalPVCIsOwned проверяет точные session и проект кандидата отдельным
// SYSTEM-conversation SQL, не считая имя PVC доказательством владения.
func (repository *Repository) ProjectPurgeGlobalPVCIsOwned(ctx context.Context, candidate ProjectPurgeCandidate, sessionRef string) (bool, error) {
	var owned bool
	err := repository.pool.QueryRow(ctx, queryProjectPurgeGlobalPVCOwner, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
		"project_ref": candidate.ProjectRef, "session_ref": sessionRef,
	}).Scan(&owned)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return owned, nil
}

// FinalizeProjectPurge locks receipt and project, accepts DONE replay, otherwise requires exact
// ordered inventory, no active archive tasks and a valid inventory digest; it clears objects,
// purges database state and emits the terminal snapshot in one transaction.
// FinalizeProjectPurge блокирует receipt и проект, принимает DONE replay, иначе требует точный
// упорядоченный inventory, отсутствие активных archive tasks и верный digest; фиксирует очистку
// объектов, удаляет состояние БД и записывает terminal snapshot в одной транзакции.
func (repository *Repository) FinalizeProjectPurge(ctx context.Context, candidate ProjectPurgeCandidate, expected []ProjectPurgeExternalItem) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	err = tx.QueryRow(ctx, queryProjectPurgeLockReceipt,
		candidate.OrganizationID, candidate.ProjectID, candidate.ProjectRef).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if state == "DONE" {
		return nil
	}
	if state != "PENDING" {
		return errs.ErrConflict
	}
	var projectRef string
	var projectVersion int64
	err = tx.QueryRow(ctx, queryProjectPurgeLockProject,
		candidate.OrganizationID, candidate.ProjectID).Scan(&projectRef, &projectVersion)
	if err != nil || projectRef != candidate.ProjectRef {
		return errs.ErrConflict
	}
	rows, err := tx.Query(ctx, queryProjectPurgeInventory, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	actual := make([]ProjectPurgeExternalItem, 0, len(expected))
	for rows.Next() {
		var item ProjectPurgeExternalItem
		if err := rows.Scan(&item.Kind, &item.Target, &item.Version); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		actual = append(actual, item)
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil || len(actual) != len(expected) {
		return errs.ErrConflict
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return errs.ErrConflict
		}
	}
	var activeTasks int
	if err := tx.QueryRow(ctx, queryProjectPurgeActiveTasks,
		candidate.OrganizationID, candidate.ProjectID).Scan(&activeTasks); err != nil || activeTasks != 0 {
		return errs.ErrConflict
	}
	var digest string
	if err := tx.QueryRow(ctx, queryProjectPurgeDigest,
		candidate.OrganizationID, candidate.ProjectID).Scan(&digest); err != nil || len(digest) != 64 {
		return errs.ErrConflict
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryProjectPurgeMarkObjectsCleared,
		candidate.OrganizationID, candidate.ProjectID, digest); err != nil {
		return errs.ErrUnavailable
	}
	var deletedRows int
	if err := tx.QueryRow(ctx, queryProjectPurgeDeleteDatabase,
		candidate.OrganizationID, candidate.ProjectID, digest).Scan(&deletedRows); err != nil || deletedRows < 0 {
		return errs.ErrConflict
	}
	current := scope{
		organizationID: candidate.OrganizationID, organizationRef: candidate.OrganizationRef,
		correlationRef: candidate.ProjectRef,
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "PROJECT_CHANGED", candidate.ProjectRef,
		candidate.ProjectRef, "i18n:PROJECT_PURGED", projectVersion, "PURGED"); err != nil {
		return errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrConflict
	}
	return nil
}

// projectPurgeVersionedObjectIsOwned runs one of the dedicated versioned-object ownership queries
// with identical candidate/object coordinates, preserving optional archive versus mandatory subject
// version policy in their separate SQL and mapping read failure to unavailable.
// projectPurgeVersionedObjectIsOwned выполняет один из отдельных запросов владения объектом с
// одинаковыми координатами candidate/object; необязательная archive и обязательная subject version
// проверяются раздельными SQL, а отказ чтения преобразуется в unavailable.
func (repository *Repository) projectPurgeVersionedObjectIsOwned(ctx context.Context, candidate ProjectPurgeCandidate, item ProjectPurgeExternalItem, query string) (bool, error) {
	var owned bool
	err := repository.pool.QueryRow(ctx, query, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
		"project_ref": candidate.ProjectRef, "object_key": item.Target, "object_version": item.Version,
	}).Scan(&owned)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return owned, nil
}
