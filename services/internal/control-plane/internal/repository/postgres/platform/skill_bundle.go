package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/skillpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// skillBundleRow combines the public skill bundle projection with private bundle, project, current
// revision and draft revision IDs for transactional reads and writes. It carries no independent
// authorization proof.
// skillBundleRow объединяет публичное представление набора навыков с внутренними идентификаторами
// набора, проекта, текущей ревизии и черновика для транзакционного чтения и записи. Само по себе
// не подтверждает полномочия.
type skillBundleRow struct {
	entity.SkillBundle
	id, projectID, currentRevisionID, draftRevisionID string
}

// skillBundleAccess enforces the authority-project boundary and project.view for the bundle
// project. File-level artifact visibility and bundle lifecycle checks are performed separately by
// callers.
// skillBundleAccess проверяет границу проекта полномочий и project.view для проекта набора.
// Видимость файловых артефактов и жизненный цикл набора вызывающий код проверяет отдельно.
func (repository *Repository) skillBundleAccess(ctx context.Context, tx pgx.Tx, current scope, bundle skillBundleRow) error {
	if current.authorityProjectID != "" && current.authorityProjectID != bundle.projectID {
		return errs.ErrForbidden
	}
	return repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: bundle.ProjectRef})
}

// refreshSkillReceipt rechecks access to the current bundle before issuing a stored receipt. It
// checks artifact.view for files in the receipt revisions, or clears their file lists when the
// current bundle is PURGED, and propagates PURGED state. It neither reloads file bytes nor
// commits.
// refreshSkillReceipt повторно проверяет доступ к текущему набору перед выдачей сохранённой
// квитанции. Проверяет artifact.view для файлов ревизий квитанции либо очищает их списки, если
// текущий набор PURGED, и переносит состояние PURGED. Не загружает содержимое файлов и не
// завершает транзакцию.
func (repository *Repository) refreshSkillReceipt(ctx context.Context, tx pgx.Tx, current scope, result *command.Result) error {
	if result.SkillBundle == nil {
		return nil
	}
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, result.SkillBundle.Ref))
	if err != nil {
		return err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != bundle.projectID {
		return errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: bundle.ProjectRef}); err != nil {
		return err
	}
	for _, revision := range []*entity.SkillBundleRevision{result.SkillBundle.CurrentRevision, result.SkillBundle.DraftRevision} {
		if revision == nil {
			continue
		}
		if bundle.State == "PURGED" {
			revision.Files = nil
			continue
		}
		for _, file := range revision.Files {
			if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
				return err
			}
		}
	}
	if bundle.State == "PURGED" {
		result.SkillBundle.State = "PURGED"
	}
	return nil
}

// scanSkillBundle scans private IDs and decodes the bundle JSON projection, mapping missing rows
// to NotFound and storage or decoding failures to Unavailable. A failed scan may leave a partially
// populated row; no access check is implied.
// scanSkillBundle читает внутренние идентификаторы и декодирует JSON представления набора,
// преобразуя отсутствие строки в NotFound, ошибки хранилища или декодирования — в Unavailable. При
// ошибке строка может быть частично заполнена; чтение не подразумевает проверку доступа.
func scanSkillBundle(row rowScanner) (skillBundleRow, error) {
	var result skillBundleRow
	var raw []byte
	if err := row.Scan(&result.id, &result.projectID, &result.currentRevisionID, &result.draftRevisionID, &raw); errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	} else if err != nil {
		return result, errs.ErrUnavailable
	}
	if json.Unmarshal(raw, &result.SkillBundle) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// GetSkillBundle reads the tenant-scoped bundle in a five-second repeatable-read, read-only
// transaction, checks project access and artifact.view for files in current and draft revisions,
// then commits. Errors return an empty domain result; deferred rollback remains local.
// GetSkillBundle читает набор организации в read-only транзакции RepeatableRead с бюджетом пять
// секунд, проверяет доступ к проекту и artifact.view для файлов текущей ревизии и черновика, затем
// завершает транзакцию. При ошибках возвращает пустой доменный результат; отложенный rollback
// остаётся здесь.
func (repository *Repository) GetSkillBundle(ctx context.Context, principal value.Principal, ref string) (entity.SkillBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.SkillBundle{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, ref))
	if err != nil {
		return entity.SkillBundle{}, err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != bundle.projectID {
		return entity.SkillBundle{}, errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: bundle.ProjectRef}); err != nil {
		return entity.SkillBundle{}, err
	}
	for _, revision := range []*entity.SkillBundleRevision{bundle.CurrentRevision, bundle.DraftRevision} {
		if revision == nil {
			continue
		}
		for _, file := range revision.Files {
			if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
				return entity.SkillBundle{}, err
			}
		}
	}
	if tx.Commit(ctx) != nil {
		return entity.SkillBundle{}, errs.ErrUnavailable
	}
	return bundle.SkillBundle, nil
}

// resolveSkillFiles validates bounded UTF-8 metadata and file paths, copies and sorts file
// entries, and resolves exact positive artifact revisions after artifact.view checks. It requires
// ACTIVE artifacts, per-file size at most 32 MiB and total size at most 64 MiB before hashing the
// specification. The scanned antivirus state is not an admission check here; errors may accompany
// a partially resolved copy.
// resolveSkillFiles проверяет ограничения UTF-8 метаданных и пути файлов, копирует и сортирует
// записи, затем разрешает точные положительные ревизии артефактов после artifact.view. Требует
// ACTIVE, размер каждого файла до 32 МиБ и общий размер до 64 МиБ перед вычислением дайджеста
// спецификации. Прочитанный антивирусный статус здесь не является проверкой допуска; при ошибке
// копия может быть частично разрешена.
func (repository *Repository) resolveSkillFiles(ctx context.Context, tx pgx.Tx, current scope, projectID string, specification entity.SkillBundleSpecification) (entity.SkillBundleSpecification, string, error) {
	if strings.TrimSpace(specification.Name) == "" || len([]rune(specification.Name)) > 160 || len([]rune(specification.Description)) > 2000 || !utf8.ValidString(specification.Name+specification.Description) || strings.ContainsRune(specification.Name+specification.Description, 0) {
		return specification, "", errs.ErrInvalid
	}
	paths := make([]string, 0, len(specification.Files))
	for _, file := range specification.Files {
		paths = append(paths, file.Path)
	}
	if skillpolicy.ValidatePaths(paths) != nil {
		return specification, "", errs.ErrInvalid
	}
	specification.Files = append([]entity.SkillBundleFile(nil), specification.Files...)
	sort.Slice(specification.Files, func(i, j int) bool { return specification.Files[i].Path < specification.Files[j].Path })
	var total int64
	for i := range specification.Files {
		file := &specification.Files[i]
		if file.ArtifactRevision < 1 {
			return specification, "", errs.ErrInvalid
		}
		if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
			return specification, "", err
		}
		var scan, state string
		if err := tx.QueryRow(ctx, querySkillArtifactGet, current.organizationID, projectID, file.ArtifactRef, file.ArtifactRevision).Scan(&file.Digest, &file.SizeBytes, &scan, &state); errors.Is(err, pgx.ErrNoRows) {
			return specification, "", errs.ErrNotFound
		} else if err != nil {
			return specification, "", errs.ErrUnavailable
		}
		if state != "ACTIVE" || file.SizeBytes < 0 || file.SizeBytes > 32<<20 {
			return specification, "", errs.ErrConflict
		}
		total += file.SizeBytes
		if total > 64<<20 {
			return specification, "", errs.ErrInvalid
		}
	}
	raw, _ := json.Marshal(specification)
	digest := sha256.Sum256(raw)
	return specification, hex.EncodeToString(digest[:]), nil
}

// saveSkillDraft creates a bundle draft or updates its exact existing draft inside the supplied
// transaction, locking existing bundles and enforcing expected version and ACTIVE state. It
// resolves file metadata, persists the draft specification, resets review/scan state on save and
// updates the bundle draft pointer before readback. It does not publish, scan files or commit;
// outer command processing owns authorization and atomic effects.
// saveSkillDraft создаёт черновик набора либо обновляет его точный существующий черновик в
// переданной транзакции, блокируя существующий набор и проверяя ожидаемую версию и ACTIVE.
// Разрешает метаданные файлов, сохраняет спецификацию, сбрасывает состояние проверки и
// сканирования при сохранении и обновляет указатель черновика перед повторным чтением. Не
// публикует, не сканирует файлы и не завершает транзакцию; внешний обработчик отвечает за
// авторизацию и атомарность эффектов.
func (repository *Repository) saveSkillDraft(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SkillBundleInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	create := input.Kind == command.CreateSkillBundleDraft && payload.BundleRef == ""
	var id, ref, state string
	var version int64
	if create {
		ref, _ = newRef("sklb")
		if err := tx.QueryRow(ctx, querySkillBundleInsert, current.organizationID, ref, payload.ProjectRef, current.actorID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		ref = payload.BundleRef
		if err := tx.QueryRow(ctx, querySkillBundleLock, current.organizationID, ref).Scan(&id, &version, &state); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != version {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if state != "ACTIVE" {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, ref))
	if err != nil {
		return commandOutcome{}, err
	}
	if payload.ProjectRef != "" && payload.ProjectRef != bundle.ProjectRef {
		return commandOutcome{}, errs.ErrForbidden
	}
	specification, digest, err := repository.resolveSkillFiles(ctx, tx, current, bundle.projectID, payload.Specification)
	if err != nil {
		return commandOutcome{}, err
	}
	files, _ := json.Marshal(specification.Files)
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "bundle_id": id, "name": specification.Name, "description": specification.Description, "files": files, "digest": digest}
	revisionID := bundle.draftRevisionID
	if input.Kind == command.CreateSkillBundleDraft {
		if revisionID != "" {
			return commandOutcome{}, errs.ErrConflict
		}
		revisionRef, _ := newRef("sklv")
		args["revision_ref"], args["actor_id"] = revisionRef, current.actorID
		if err := tx.QueryRow(ctx, querySkillRevisionInsert, args).Scan(&revisionID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else if input.Kind == command.SaveSkillBundleDraft {
		if bundle.DraftRevision == nil || payload.RevisionRef != bundle.DraftRevision.Ref {
			return commandOutcome{}, errs.ErrConflict
		}
		args["revision_ref"] = payload.RevisionRef
		tag, err := tx.Exec(ctx, querySkillRevisionSave, args)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	} else {
		return commandOutcome{}, errs.ErrInvalid
	}
	increment := int64(1)
	if create {
		increment = 0
	}
	if _, err := tx.Exec(ctx, querySkillBundleSetDraft, current.organizationID, id, revisionID, increment); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	bundle, err = scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, ref))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{SkillBundle: &bundle.SkillBundle}, resourceKind: "SKILL_BUNDLE", resourceRef: ref, projectID: bundle.projectID, projectRef: bundle.ProjectRef, summary: "i18n:SKILL_BUNDLE_CHANGED"}, nil
}
