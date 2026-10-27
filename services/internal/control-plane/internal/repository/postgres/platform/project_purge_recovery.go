package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryProjectPurgeRecoveryAuthority Checks for a PENDING/DONE purge receipt under exact
// organization/project ref, allowing owner recovery lookup after project removal. It establishes
// receipt existence only; role/workload authority is enforced outside SQL.
// queryProjectPurgeRecoveryAuthority Проверяет квитанцию очистки PENDING/DONE по точным
// организации и ссылке проекта, позволяя восстановление после удаления проекта. Подтверждает лишь
// наличие квитанции; роль и полномочия workload проверяются вне SQL.
//
//go:embed sql/project_trash__recovery_authority.sql
var queryProjectPurgeRecoveryAuthority string

// queryProjectPurgeRecoveryReadiness Checks existence of the five exact recovery SQL function
// signatures. This storage prerequisite does not prove object-store connectivity, function
// privileges or successful cleanup.
// queryProjectPurgeRecoveryReadiness Проверяет наличие пяти точных сигнатур SQL-функций
// восстановления. Эта предпосылка хранилища не доказывает доступность object store, права
// выполнения или успешную очистку.
//
//go:embed sql/project_trash__recovery_readiness.sql
var queryProjectPurgeRecoveryReadiness string

// queryProjectPurgeRecoverySnapshot Calls the owner recovery snapshot function with exact
// organization/project and configured storage identity. The returned JSON is decoded and
// shape-checked by the caller; it is not authorization to delete objects.
// queryProjectPurgeRecoverySnapshot Вызывает функцию снимка восстановления владельца по точным
// организации, проекту и настроенной идентичности хранилища. JSON декодируется и проверяется
// вызывающим; удаление объектов снимком не разрешается.
//
//go:embed sql/project_purge_recovery__snapshot.sql
var queryProjectPurgeRecoverySnapshot string

// queryProjectPurgeRecoveryResolve Calls the owner resolution function with exact
// organization/project, expected version, snapshot digest, actor, generated resolution ref and
// storage identity. Caller transaction owns the resulting command receipt/audit/event lifecycle.
// queryProjectPurgeRecoveryResolve Вызывает функцию решения владельца с точными организацией,
// проектом, ожидаемой версией, хешем снимка, субъектом, новой ссылкой решения и идентичностью
// хранилища. Квитанция команды, аудит и события принадлежат транзакции вызывающего.
//
//go:embed sql/project_purge_recovery__resolve.sql
var queryProjectPurgeRecoveryResolve string

// queryProjectPurgeRecoveryClaim Calls bounded recovery claim using worker owner and configured
// storage identity, projecting exact ledger/object pins, generation and cleanup deadline. The
// result is internal worker metadata; no object is deleted by this query wrapper.
// queryProjectPurgeRecoveryClaim Вызывает ограниченный захват восстановления по владельцу worker и
// идентичности хранилища, проецирует точные ledger, привязки объекта, поколение и срок очистки.
// Это внутренние данные worker; сама обёртка объект не удаляет.
//
//go:embed sql/project_purge_recovery__claim.sql
var queryProjectPurgeRecoveryClaim string

// queryProjectPurgeRecoveryPin Passes ledger ID, worker owner, generation, storage identity and
// observed object version/ETag to the owner pin function. A boolean confirmation must succeed
// before the caller proceeds; no object bytes are read here.
// queryProjectPurgeRecoveryPin Передаёт ledger ID, владельца worker, поколение, идентичность
// хранилища и наблюдавшиеся версию объекта с ETag функции закрепления. Вызывающий требует
// положительное подтверждение; байты объекта здесь не читаются.
//
//go:embed sql/project_purge_recovery__pin.sql
var queryProjectPurgeRecoveryPin string

// queryProjectPurgeRecoveryFinish Passes exact ledger/worker/generation/storage binding and a
// closed cleanup outcome to the owner finish function. Its boolean reports acceptance of the
// observation rather than performing external deletion.
// queryProjectPurgeRecoveryFinish Передаёт точную привязку ledger, worker, поколения и хранилища с
// закрытым исходом очистки функции завершения. Boolean подтверждает принятие наблюдения, а не
// выполняет внешнее удаление.
//
//go:embed sql/project_purge_recovery__finish.sql
var queryProjectPurgeRecoveryFinish string

// queryProjectPurgeRecoveryBlocker Checks nonadopted/noncleaned prepared content for the exact
// organization/project, excluding owner-abandoned content only with an owner resolution and
// confirmed-deleted or observed-absent recovery state. It is a blocker projection, not a cleanup
// command.
// queryProjectPurgeRecoveryBlocker Проверяет неподтверждённое или неочищенное prepared content
// точного проекта организации. OWNER_ABANDONED исключается лишь при решении владельца и
// DELETED_CONFIRMED либо OBSERVING с наблюдавшимся отсутствием. Это проекция препятствий, а не
// команда очистки.
//
//go:embed sql/project_purge_recovery__blocker.sql
var queryProjectPurgeRecoveryBlocker string

// ConfigureProjectPurgeStorage Hashes the exact typed JSON endpoint/region/bucket/path-style tuple
// as storage identity after rejecting empty required fields. Credentials are excluded, so rotation
// does not redirect an owner resolution; this method performs no connection probe or URL
// normalization.
// ConfigureProjectPurgeStorage Хеширует точный типизированный JSON из endpoint, region, bucket и
// path-style после отказа пустым обязательным полям. Credential исключены, поэтому ротация не
// перенаправляет решение владельца. Подключение не проверяет, URL не нормализует.
func (repository *Repository) ConfigureProjectPurgeStorage(endpoint, region, bucket string, pathStyle bool) error {
	if endpoint == "" || region == "" || bucket == "" {
		return errs.ErrInvalid
	}
	encoded, err := json.Marshal(struct {
		Endpoint, Region, Bucket string
		PathStyle                bool
	}{endpoint, region, bucket, pathStyle})
	if err != nil {
		return errs.ErrInvalid
	}
	digest := sha256.Sum256(encoded)
	repository.projectPurgeStorageIdentity = hex.EncodeToString(digest[:])
	return nil
}

// authorizeProjectPurgeRecovery Requires an existing same-organization PENDING/DONE purge receipt
// for the resolved project ref, mapping false to not-found and scan failure to unavailable. It
// does not itself check the actor role/workload or permit object deletion.
// authorizeProjectPurgeRecovery Требует существующую квитанцию очистки PENDING/DONE той же
// организации по разрешённой ссылке проекта. False даёт NotFound, сбой чтения — Unavailable.
// Самостоятельно не проверяет роль или workload и не разрешает удаление объектов.
func (repository *Repository) authorizeProjectPurgeRecovery(ctx context.Context, tx pgx.Tx, current scope, ref string) error {
	var exists bool
	if err := tx.QueryRow(ctx, queryProjectPurgeRecoveryAuthority, current.organizationID, ref).Scan(&exists); err != nil {
		return errs.ErrUnavailable
	}
	if !exists {
		return errs.ErrNotFound
	}
	return nil
}

// decodeProjectPurgeRecovery Decodes nonempty recovery JSON and validates positive version,
// lexical snapshot digest, project presence/version consistency, nonnegative counters, closed
// states and CanResolve only in WAITING_OWNER. Empty input is not-found; malformed/inconsistent
// data is conflict. It does not recompute the snapshot commitment.
// decodeProjectPurgeRecovery Декодирует непустой JSON восстановления, проверяет положительную
// версию, форму хеша снимка, согласованность наличия и версии проекта, неотрицательные счётчики,
// закрытые состояния и CanResolve только для WAITING_OWNER. Пустой вход даёт NotFound, повреждение
// — Conflict. Сам хеш снимка не пересчитывает.
func decodeProjectPurgeRecovery(raw []byte) (entity.ProjectPurgeRecovery, error) {
	var item entity.ProjectPurgeRecovery
	if len(raw) == 0 {
		return item, errs.ErrNotFound
	}
	if json.Unmarshal(raw, &item) != nil || item.Version < 1 || !exactSHA256(item.SnapshotDigest) || item.ProjectRef == "" || item.UnknownContentCount < 0 || item.CleanupPendingCount < 0 || item.ProjectPresent != (item.ProjectVersion > 0) {
		return entity.ProjectPurgeRecovery{}, errs.ErrConflict
	}
	switch item.State {
	case "WAITING_OWNER", "QUEUED", "OBSERVING", "DELETED_CONFIRMED", "NONE", "BLOCKED":
	default:
		return entity.ProjectPurgeRecovery{}, errs.ErrConflict
	}
	if item.CanResolve && item.State != "WAITING_OWNER" {
		return entity.ProjectPurgeRecovery{}, errs.ErrConflict
	}
	return item, nil
}

// GetProjectPurgeRecovery Resolves scope and hides access unless the caller is the control API
// gateway with organization-wide OWNER/ADMINISTRATOR authority, then reads/decodes a storage-bound
// recovery snapshot. Missing storage configuration is unavailable; the result grants no cleanup
// permission.
// GetProjectPurgeRecovery Разрешает область и скрывает доступ, если вызов не от control API
// gateway с организационными OWNER/ADMINISTRATOR полномочиями. Затем читает и декодирует снимок
// нужного хранилища. Отсутствие настройки даёт Unavailable; снимок не разрешает очистку.
func (repository *Repository) GetProjectPurgeRecovery(ctx context.Context, principal value.Principal, ref string) (entity.ProjectPurgeRecovery, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ProjectPurgeRecovery{}, err
	}
	if current.authorityProjectID != "" || current.role != "OWNER" && current.role != "ADMINISTRATOR" || principal.CallerWorkload != "control-api-gateway" {
		return entity.ProjectPurgeRecovery{}, errs.ErrNotFound
	}
	if repository.projectPurgeStorageIdentity == "" {
		return entity.ProjectPurgeRecovery{}, errs.ErrUnavailable
	}
	var raw []byte
	if err := repository.pool.QueryRow(ctx, queryProjectPurgeRecoverySnapshot, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": ref, "storage_identity": repository.projectPurgeStorageIdentity}).Scan(&raw); err != nil {
		return entity.ProjectPurgeRecovery{}, errs.ErrUnavailable
	}
	return decodeProjectPurgeRecovery(raw)
}

// resolveProjectPurgeRecovery Requires typed acknowledgement, expected version and snapshot
// digest, invokes the owner resolution SQL and maps known SQLSTATE classes without exposing driver
// detail. Decodes the result and, when the project remains, locks its exact matching version and
// emits PROJECT_CHANGED intent. Outer execution owns authorization/audit/idempotency/commit.
// resolveProjectPurgeRecovery Требует типизированное подтверждение, ожидаемую версию и хеш снимка,
// вызывает SQL решения владельца и преобразует известные SQLSTATE без раскрытия драйвера.
// Декодирует результат, при сохранённом проекте блокирует точную совпавшую версию и задаёт
// PROJECT_CHANGED. Авторизация, аудит, идемпотентность и commit принадлежат внешнему исполнению.
func (repository *Repository) resolveProjectPurgeRecovery(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProjectPurgeRecoveryInput)
	if !ok || input.Mutation.ExpectedVersion == nil || !payload.AcknowledgeUnknownOutcome || !exactSHA256(payload.SnapshotDigest) {
		return commandOutcome{}, errs.ErrInvalid
	}
	if repository.projectPurgeStorageIdentity == "" {
		return commandOutcome{}, errs.ErrUnavailable
	}
	resolutionRef, err := newRef("ppr")
	if err != nil {
		return commandOutcome{}, err
	}
	var raw []byte
	err = tx.QueryRow(ctx, queryProjectPurgeRecoveryResolve, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": payload.Ref, "expected_version": *input.Mutation.ExpectedVersion, "snapshot_digest": payload.SnapshotDigest, "actor_id": current.actorID, "resolution_ref": resolutionRef, "storage_identity": repository.projectPurgeStorageIdentity}).Scan(&raw)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "40001":
				return commandOutcome{}, errs.ErrVersionMismatch
			case "P0002", "42501":
				return commandOutcome{}, errs.ErrNotFound
			case "22023":
				return commandOutcome{}, errs.ErrInvalid
			case "P0001":
				return commandOutcome{}, errs.ErrConflict
			}
		}
		return commandOutcome{}, errs.ErrUnavailable
	}
	item, err := decodeProjectPurgeRecovery(raw)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome := commandOutcome{result: command.Result{ProjectPurgeRecovery: &item}, resourceKind: "PROJECT_PURGE_RECOVERY", resourceRef: payload.Ref, summary: "i18n:PROJECT_PURGE_RECOVERY_RESOLVED"}
	if item.ProjectPresent {
		var project entity.Project
		err = tx.QueryRow(ctx, queryProjectTrashLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": payload.Ref}).Scan(&outcome.projectID, &project.Ref, &project.Name, &project.Purpose, &project.Language, &project.Lifecycle, &project.Version, &project.CreatedAt, &project.UpdatedAt, &project.DeletedAt, &project.PurgeAfter)
		if err != nil || project.Version != item.ProjectVersion {
			return commandOutcome{}, errs.ErrConflict
		}
		outcome.projectRef = payload.Ref
		outcome.platformEvent = "PROJECT_CHANGED"
		outcome.platformAggregateVersion = project.Version
		outcome.platformState = project.Lifecycle
	}
	return outcome, nil
}

// ProjectPurgeRecoveryClaim Carries internal ledger/organization/project identity, exact object
// locator/version/ETag/digest/size and worker generation/deadline for cleanup reconciliation. It
// contains descriptors rather than object content and must stay bound to the configured storage
// and owner claim.
// ProjectPurgeRecoveryClaim Хранит внутренние идентификаторы ledger, организации и проекта, точные
// указатель, версию, ETag, хеш, размер объекта, поколение и срок worker для сверки очистки.
// Содержит дескрипторы вместо тела объекта; сохраняет привязку к настроенному хранилищу и
// владельцу захвата.
type ProjectPurgeRecoveryClaim struct {
	LedgerID, OrganizationID, ProjectRef string
	Key, Version, ETag, Digest           string
	SizeBytes, Generation                int64
	Deadline                             time.Time
}

// ClaimProjectPurgeRecovery Validates nonempty worker owner, limit 1..4 and configured storage
// identity, then scans owner-function claims into an allocated list. Read/iteration failures
// discard partial output as unavailable; SQL owns claim fencing and no external object operation
// occurs here.
// ClaimProjectPurgeRecovery Проверяет непустого владельца worker, предел 1..4 и идентичность
// хранилища, читает захваты функции владельца в созданный список. Сбой чтения отбрасывает
// частичный результат с Unavailable; fence принадлежит SQL, внешних действий с объектом здесь нет.
func (repository *Repository) ClaimProjectPurgeRecovery(ctx context.Context, owner string, limit int32) ([]ProjectPurgeRecoveryClaim, error) {
	if owner == "" || limit < 1 || limit > 4 || repository.projectPurgeStorageIdentity == "" {
		return nil, errs.ErrInvalid
	}
	rows, err := repository.pool.Query(ctx, queryProjectPurgeRecoveryClaim, pgx.StrictNamedArgs{"owner": owner, "limit": limit, "storage_identity": repository.projectPurgeStorageIdentity})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]ProjectPurgeRecoveryClaim, 0, limit)
	for rows.Next() {
		var item ProjectPurgeRecoveryClaim
		if err := rows.Scan(&item.LedgerID, &item.OrganizationID, &item.ProjectRef, &item.Key, &item.Version, &item.ETag, &item.Digest, &item.SizeBytes, &item.Generation, &item.Deadline); err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// PinProjectPurgeRecovery Requires observed key/digest/size to match the claim and a nonempty
// version/ETag, then submits exact owner/generation/storage pins. A false confirmation is conflict
// and read failure unavailable; it never reads object bytes or performs deletion.
// PinProjectPurgeRecovery Требует совпадения наблюдавшихся key, digest и size с захватом, непустых
// версии и ETag; передаёт точные привязки владельца, поколения и хранилища. False даёт Conflict,
// сбой чтения — Unavailable. Байты объекта не читает и удаление не выполняет.
func (repository *Repository) PinProjectPurgeRecovery(ctx context.Context, owner string, claim ProjectPurgeRecoveryClaim, receipt objectstorage.Receipt) error {
	if receipt.Key != claim.Key || receipt.Digest != claim.Digest || receipt.SizeBytes != claim.SizeBytes || receipt.VersionID == "" || receipt.ETag == "" {
		return errs.ErrConflict
	}
	return scanProjectPurgeRecoveryConfirmation(repository.pool.QueryRow(ctx, queryProjectPurgeRecoveryPin, pgx.StrictNamedArgs{"id": claim.LedgerID, "owner": owner, "generation": claim.Generation, "storage_identity": repository.projectPurgeStorageIdentity, "object_version": receipt.VersionID, "object_etag": receipt.ETag}))
}

// FinishProjectPurgeRecovery Accepts only ABSENT/DELETED/FAILED cleanup observations and submits
// exact ledger/owner/generation/storage binding to the owner function. False is conflict and read
// failure unavailable; acceptance is not independent proof that an external deletion happened.
// FinishProjectPurgeRecovery Допускает только наблюдения очистки ABSENT/DELETED/FAILED, передаёт
// точную привязку ledger, владельца, поколения и хранилища функции владельца. False даёт Conflict,
// ошибка чтения — Unavailable. Принятие само не доказывает внешнее удаление.
func (repository *Repository) FinishProjectPurgeRecovery(ctx context.Context, owner string, claim ProjectPurgeRecoveryClaim, outcome string) error {
	switch outcome {
	case "ABSENT", "DELETED", "FAILED":
	default:
		return errs.ErrInvalid
	}
	return scanProjectPurgeRecoveryConfirmation(repository.pool.QueryRow(ctx, queryProjectPurgeRecoveryFinish, pgx.StrictNamedArgs{"id": claim.LedgerID, "owner": owner, "generation": claim.Generation, "storage_identity": repository.projectPurgeStorageIdentity, "outcome": outcome}))
}

// scanProjectPurgeRecoveryConfirmation accepts a positive owner-function boolean, maps false to
// conflict and every scan failure, including missing rows, to unavailable. Pin/finish callers
// retain their distinct input checks, exact SQL/owner/generation/storage arguments and external
// observation responsibilities. A positive flag alone is not proof of object deletion.
// scanProjectPurgeRecoveryConfirmation принимает положительный boolean функции владельца,
// преобразует false в Conflict, любую ошибку чтения, включая отсутствие строки, в Unavailable.
// Pin и finish сохраняют разные проверки входа, точные SQL и аргументы владельца, поколения,
// хранилища, обязанности внешнего наблюдения. Сам флаг не доказывает удаление объекта.
func scanProjectPurgeRecoveryConfirmation(row pgx.Row) error {
	var confirmed bool
	if err := row.Scan(&confirmed); err != nil {
		return errs.ErrUnavailable
	}
	if !confirmed {
		return errs.ErrConflict
	}
	return nil
}
