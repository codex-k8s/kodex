package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// createProjectFileRevision Names the closed assistant operation that creates an immutable
// revision of an existing project file. The string selects a specialized command path rather than
// generic artifact mutation.
// createProjectFileRevision Задаёт закрытый вид операции ассистента для создания неизменяемой
// ревизии существующего файла проекта. Строка выбирает специализированную команду, а не
// произвольное изменение артефакта.
const createProjectFileRevision = "CREATE_PROJECT_FILE_REVISION"

// redactAssistantFilePlanOperations For file operations, shallow-copies parameters/after maps,
// removes historical content fields and clears input in the returned slice entries. Stored
// historical rows/digests are not rewritten; non-file operations remain untouched.
// redactAssistantFilePlanOperations Для файловых операций поверхностно копирует карты
// parameters/after, удаляет исторические поля content и очищает input в элементах переданного
// среза. Сохранённые исторические строки и их хеши не переписываются; прочие операции не меняются.
func redactAssistantFilePlanOperations(operations []entity.AssistantPlanOperation) {
	for index, operation := range operations {
		if !assistantFileOperation(operation.Type) {
			continue
		}
		operation.Parameters = cloneAssistantFields(operation.Parameters)
		delete(operation.Parameters, "content")
		operation.After = cloneAssistantFields(operation.After)
		delete(operation.After, "content")
		operation.Input = nil
		operations[index] = operation
	}
}

// assistantFileOperation Recognizes only CREATE_PROJECT_FILE and CREATE_PROJECT_FILE_REVISION for
// file-specific persistence/redaction guards. Unknown operation names do not enter this path.
// assistantFileOperation Распознаёт только CREATE_PROJECT_FILE и CREATE_PROJECT_FILE_REVISION для
// файловых проверок сохранения и сокрытия. Неизвестные имена операций в этот путь не входят.
func assistantFileOperation(kind string) bool {
	return kind == "CREATE_PROJECT_FILE" || kind == createProjectFileRevision
}

// assistantFilePersistenceReady Before the first SQL write, requires prepared metadata and a
// server ledger mapping for every file operation, rejecting content in input or after. Other
// operation kinds are skipped; this check does not adopt content or authorize the later command.
// assistantFilePersistenceReady До первой SQL-записи требует подготовленные метаданные и серверную
// привязку ledger для каждой файловой операции, отклоняя content в input или after. Прочие виды
// пропускаются; содержимое не принимается и последующая команда не авторизуется.
func assistantFilePersistenceReady(operations []entity.AssistantPlanOperation, prepared map[string]string) error {
	for _, operation := range operations {
		if !assistantFileOperation(operation.Type) {
			continue
		}
		if !assistantProjectFileContentReady(operation) || prepared[operation.Key] == "" {
			return errs.ErrConflict
		}
		if _, exists := operation.Input["content"]; exists {
			return errs.ErrConflict
		}
		if _, exists := operation.After["content"]; exists {
			return errs.ErrConflict
		}
	}
	return nil
}

// rehydrateEditedAssistantFile Preserves target/before/expected-version identity during owner
// edits, distinguishes server-prepared descriptors from raw proposals and reconstructs permitted
// after metadata without persisting bodies. Rejects forged content pins or cross-project/artifact
// changes and clears input before return.
// rehydrateEditedAssistantFile Сохраняет target, before и ожидаемую версию при правках владельца,
// отличает подготовленный сервером дескриптор от сырого предложения и восстанавливает допустимые
// метаданные after без сохранения тела. Отклоняет подменённые привязки содержимого и смену проекта
// или артефакта, очищает input перед возвратом.
func rehydrateEditedAssistantFile(original, edited entity.AssistantPlanOperation, serverPrepared bool) (entity.AssistantPlanOperation, error) {
	if !assistantJSONEqual(original.Target, edited.Target) || !assistantJSONEqual(original.Before, edited.Before) ||
		!assistantJSONEqual(original.ExpectedVersion, edited.ExpectedVersion) {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	_, hasBody := edited.Parameters["content"]
	if serverPrepared {
		if hasBody || assistantString(edited.Parameters, "contentRef") == "" || !objectDigestValid(assistantString(edited.Parameters, "digest")) {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		if original.Type == createProjectFileRevision {
			if !assistantJSONEqual(edited.After, assistantRevisionAfter(edited)) || assistantString(edited.Parameters, "artifactRef") != original.Target.Ref {
				return entity.AssistantPlanOperation{}, errs.ErrForbidden
			}
		} else if !assistantJSONEqual(edited.After, edited.Parameters) {
			return entity.AssistantPlanOperation{}, errs.ErrForbidden
		}
	} else {
		if !assistantJSONEqual(original.After, edited.After) {
			return entity.AssistantPlanOperation{}, errs.ErrForbidden
		}
		if !hasBody {
			if !assistantJSONEqual(original.Parameters, edited.Parameters) {
				return entity.AssistantPlanOperation{}, errs.ErrForbidden
			}
		} else {
			for _, field := range []string{"contentRef", "digest", "sizeBytes"} {
				if _, ok := edited.Parameters[field]; ok {
					return entity.AssistantPlanOperation{}, errs.ErrInvalid
				}
			}
			if original.Type == createProjectFileRevision {
				if assistantString(edited.Parameters, "artifactRef") != original.Target.Ref {
					return entity.AssistantPlanOperation{}, errs.ErrForbidden
				}
				edited.After = assistantRevisionAfter(edited)
			} else {
				if assistantString(edited.Parameters, "projectRef") != assistantString(original.Parameters, "projectRef") {
					return entity.AssistantPlanOperation{}, errs.ErrForbidden
				}
				edited.After = cloneAssistantFields(edited.Parameters)
				delete(edited.After, "content")
			}
		}
	}
	edited.Input = nil
	return edited, nil
}

// assistantFileRevisionSnapshotMatches Reads the current eligible artifact head and requires both
// expected version and full before-projection equality. Read failures propagate distinctly; a
// matching snapshot does not reserve the head or replace later command authorization/OCC.
// assistantFileRevisionSnapshotMatches Читает текущую пригодную головную ревизию артефакта,
// требует совпадения ожидаемой версии и всей проекции before. Ошибка чтения возвращается отдельно;
// совпадение снимка не резервирует head и не заменяет авторизацию или OCC команды.
func (repository *Repository) assistantFileRevisionSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (bool, error) {
	item, err := repository.artifactRevisionReadHead(ctx, tx, current, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && item.Version == *operation.ExpectedVersion && assistantJSONEqual(assistantRevisionBefore(item), operation.Before), nil
}

// queryArtifactRevisionCreateLock Embeds an exact organization/ref ACTIVE-head lookup with FOR
// UPDATE, returning internal artifact/project IDs and version. The caller performs fresh authority
// first and compares immutable source/version after locking.
// queryArtifactRevisionCreateLock Встраивает поиск ACTIVE head точных организации и ссылки с FOR
// UPDATE, возвращая внутренние ID артефакта, проекта и версию. Вызывающий заранее проверяет свежие
// полномочия, после блокировки сравнивает исходную ревизию и версию.
//
//go:embed sql/artifact_revision_create_lock.sql
var queryArtifactRevisionCreateLock string

// queryArtifactRevisionCreate Embeds immutable revision insertion with caller-resolved
// artifact/actor IDs, prepared metadata and database transaction time, returning the internal
// revision ID. It does not advance the head or store file bytes.
// queryArtifactRevisionCreate Встраивает вставку неизменяемой ревизии с разрешёнными ID артефакта
// и субъекта, подготовленными метаданными и временем транзакции БД, возвращая внутренний ID
// ревизии. Head не продвигает, байты файла не хранит.
//
//go:embed sql/artifact_revision_create.sql
var queryArtifactRevisionCreate string

// queryArtifactRevisionCreateContent Embeds insertion of exact prepared object identity, digest,
// size and antivirus report under a revision ID. It stores descriptors rather than the file body;
// caller transaction and ledger adoption bind ownership.
// queryArtifactRevisionCreateContent Встраивает запись точной идентичности подготовленного
// объекта, хеша, размера и отчёта антивируса по ID ревизии. Хранит дескрипторы вместо тела файла;
// транзакция вызывающего и принятие ledger связывают владение.
//
//go:embed sql/artifact_revision_create_content.sql
var queryArtifactRevisionCreateContent string

// queryArtifactRevisionAdvanceHead Embeds an ACTIVE-head compare-and-swap by internal artifact ID
// and exact version, advancing the revision pointer and version. Callers require exactly one
// affected row; no independent actor/tenant check exists in this update.
// queryArtifactRevisionAdvanceHead Встраивает условное обновление ACTIVE head по внутреннему ID
// артефакта и точной версии, продвигая указатель ревизии и версию. Вызывающий требует ровно одну
// изменённую строку; самостоятельной проверки субъекта или организации в UPDATE нет.
//
//go:embed sql/artifact_revision_advance_head.sql
var queryArtifactRevisionAdvanceHead string

// queryArtifactRevisionCurrentID Embeds exact organization/artifact/current-revision-ref
// resolution for an ACTIVE head. Matching the current revision is a locator check; download or
// revision-create authority remains outside SQL.
// queryArtifactRevisionCurrentID Встраивает разрешение текущей ревизии ACTIVE head по точным
// организации, артефакту и ссылке ревизии. Совпадение является проверкой указателя; право
// скачивания или создания ревизии проверяется вне SQL.
//
//go:embed sql/artifact_revision_current_id.sql
var queryArtifactRevisionCurrentID string

// assistantRevisionBefore Builds the canonical artifact head snapshot of identity/current
// revision, counters, content metadata and lifecycle/scan state for plan OCC. It includes no file
// body or object-store credential.
// assistantRevisionBefore Строит канонический снимок head: идентичность, текущая ревизия,
// счётчики, метаданные содержимого, жизненный цикл и состояние сканирования для OCC плана. Тела
// файла и credential объектного хранилища нет.
func assistantRevisionBefore(item entity.Artifact) map[string]any {
	return map[string]any{"artifactRef": item.Ref, "currentRevisionRef": item.CurrentRevisionRef,
		"revision": item.Revision, "version": item.Version, "digest": item.Digest,
		"fileName": item.FileName, "mediaType": item.MediaType, "sizeBytes": item.SizeBytes,
		"scanState": item.ScanState, "lifecycleState": item.LifecycleState}
}

// assistantRevisionAfter Builds proposed immutable-revision metadata from typed parameter
// accessors and protected before fields, retaining content locator and previous revision pin. It
// does not include raw body or prove the locator was prepared by the server.
// assistantRevisionAfter Строит предложенные метаданные неизменяемой ревизии через типизированные
// параметры и защищённые поля before, сохраняя указатель содержимого и прежнюю ревизию. Тело не
// включает и серверную подготовку указателя не доказывает.
func assistantRevisionAfter(operation entity.AssistantPlanOperation) map[string]any {
	return map[string]any{"artifactRef": assistantString(operation.Parameters, "artifactRef"),
		"fileName": assistantString(operation.Before, "fileName"), "mediaType": assistantString(operation.Parameters, "mediaType"),
		"digest": assistantString(operation.Parameters, "digest"), "sizeBytes": operation.Parameters["sizeBytes"],
		"contentRef":          assistantString(operation.Parameters, "contentRef"),
		"previousRevisionRef": assistantString(operation.Before, "currentRevisionRef"), "createsImmutableRevision": true}
}

// hydrateAssistantFileRevision Resolves a clean file head in the exact project, requires
// artifact.download and artifact.revision.create, then assigns server-owned UPDATE
// target/before/expected-version/after metadata and selects the operation. It prepares a plan
// snapshot without writing a revision.
// hydrateAssistantFileRevision Разрешает чистый head файла точного проекта, требует
// artifact.download и artifact.revision.create, затем назначает серверные UPDATE target, before,
// ожидаемую версию и after, выбирает операцию. Готовит снимок плана без записи ревизии.
func (repository *Repository) hydrateAssistantFileRevision(ctx context.Context, tx pgx.Tx, current scope, projectRef string, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	ref := assistantString(operation.Parameters, "artifactRef")
	item, err := repository.artifactRevisionReadHead(ctx, tx, current, ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if projectRef == "" || item.ProjectRef != projectRef || item.ScanState != "CLEAN" {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	}
	for _, permission := range []string{"artifact.download", "artifact.revision.create"} {
		_, target, err := repository.resolveCommandTarget(ctx, tx, current, permission, "ARTIFACT", ref, projectRef)
		if err != nil {
			return entity.AssistantPlanOperation{}, err
		}
		if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
			return entity.AssistantPlanOperation{}, errs.ErrNotFound
		}
	}
	version := item.Version
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "ARTIFACT", Ref: item.Ref, Name: item.FileName, Version: &version}
	operation.Before = assistantRevisionBefore(item)
	operation.ExpectedVersion = &version
	operation.After = assistantRevisionAfter(operation)
	operation.Selected = true
	return operation, nil
}

// assistantFileRevisionCommand Maps closed typed input to CreateProjectFileRevision with positive
// expected version and an immutable source pin. Raw UTF-8/NUL-free bodies up to 1MiB are hashed in
// memory and cannot mix with prepared metadata; descriptor mode requires bounded size, content ref
// and exact digest grammar. No body is persisted here.
// assistantFileRevisionCommand Преобразует закрытый типизированный вход в
// CreateProjectFileRevision с положительной ожидаемой версией и закреплённой исходной ревизией.
// Сырое тело UTF-8 без NUL до 1MiB хешируется в памяти и не смешивается с подготовленными
// метаданными. Режим дескриптора требует ограниченный размер, ссылку содержимого и точный формат
// хеша; сохранения тела здесь нет.
func assistantFileRevisionCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	p := operation.Input
	if !onlyAssistantFields(p, "artifactRef", "mediaType", "contentEncoding", "content", "contentRef", "digest", "sizeBytes", "expectedVersion") ||
		!hasAssistantFields(p, "artifactRef", "mediaType", "expectedVersion") || !contains([]string{"text/plain", "text/markdown", "text/csv", "application/json"}, assistantString(p, "mediaType")) {
		return command.Command{}, errs.ErrInvalid
	}
	encoding := assistantString(p, "contentEncoding")
	if encoding != "" && encoding != "UTF8" {
		return command.Command{}, errs.ErrInvalid
	}
	expected, ok := assistantInt64(p, "expectedVersion")
	if !ok || expected < 1 {
		return command.Command{}, errs.ErrInvalid
	}
	input := command.ProjectFileRevisionInput{ArtifactRef: assistantString(p, "artifactRef"), MediaType: assistantString(p, "mediaType"),
		ContentRef: assistantString(p, "contentRef"), Digest: assistantString(p, "digest"), SourceRevisionRef: assistantString(operation.Before, "currentRevisionRef")}
	if content, supplied := p["content"]; supplied {
		body, ok := content.(string)
		if !ok || len(body) > 1<<20 || !utf8.ValidString(body) || strings.ContainsRune(body, 0) || input.ContentRef != "" || input.Digest != "" || p["sizeBytes"] != nil {
			return command.Command{}, errs.ErrInvalid
		}
		input.Content = []byte(body)
		input.SizeBytes = int64(len(input.Content))
		digest := sha256.Sum256(input.Content)
		input.Digest = fmt.Sprintf("sha256:%x", digest[:])
	} else {
		input.SizeBytes, ok = assistantInt64(p, "sizeBytes")
		if !ok || input.SizeBytes < 0 || input.SizeBytes > 1<<20 || !strings.HasPrefix(input.ContentRef, "pfcnt_") || !objectDigestValid(input.Digest) {
			return command.Command{}, errs.ErrInvalid
		}
	}
	if input.ArtifactRef == "" || input.SourceRevisionRef == "" {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.CreateProjectFileRevision, Payload: input, Mutation: value.Mutation{ExpectedVersion: &expected}}, nil
}

// objectDigestValid Requires the literal sha256: prefix and exactly 64 lowercase hexadecimal
// suffix bytes through the shared lexical validator. It checks representation only, without
// hashing bytes, resolving an object or validating authority.
// objectDigestValid Требует буквальный префикс sha256: и ровно 64 байта нижнего hex-регистра через
// общий лексический валидатор. Проверяет только представление без хеширования байтов, разрешения
// объекта или проверки полномочий.
func objectDigestValid(digest string) bool {
	return strings.HasPrefix(digest, "sha256:") && exactSHA256(digest[7:])
}

// createProjectFileRevision Requires server-prepared body-free input/ledger, repeats fresh command
// authority before head lock/OCC, validates source revision and exact clean antivirus/object pins,
// inserts revision/content descriptors, advances one head row and adopts the prepared ledger.
// Returns fresh artifact metadata/event intent; outer execution owns audit/idempotency/commit.
// createProjectFileRevision Требует подготовленный сервером вход без тела и ledger, повторяет
// свежую авторизацию до блокировки head и OCC, проверяет исходную ревизию и точные чистые
// антивирусные привязки объекта. Вставляет ревизию и дескрипторы, продвигает одну строку head и
// принимает ledger. Возвращает свежие метаданные и намерение события; аудит, идемпотентность и
// commit принадлежат внешней команде.
func (repository *Repository) createProjectFileRevision(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProjectFileRevisionInput)
	if !ok || payload.Content != nil || input.Mutation.ExpectedVersion == nil || payload.Prepared == nil || payload.PreparedLedgerID == "" || payload.PreparedRevisionRef == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	// Authority повторяется до lock/OCC; cached plan, locator и replay не grants.
	if err := repository.authorizeCommand(ctx, tx, current, input); err != nil {
		return commandOutcome{}, err
	}
	item, err := repository.artifactRevisionReadHead(ctx, tx, current, payload.ArtifactRef)
	if err != nil {
		return commandOutcome{}, err
	}
	var artifactID, projectID string
	var version int64
	if err := tx.QueryRow(ctx, queryArtifactRevisionCreateLock, current.organizationID, payload.ArtifactRef).Scan(&artifactID, &projectID, &version); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion || item.Version != version || item.CurrentRevisionRef != payload.SourceRevisionRef {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	p := payload.Prepared
	if p.Digest != payload.Digest || p.SizeBytes != payload.SizeBytes || p.MediaType != payload.MediaType || p.ScanState != "CLEAN" {
		return commandOutcome{}, errs.ErrConflict
	}
	scanJSON, err := artifactAntivirusJSON(p.Antivirus)
	if err != nil || p.Antivirus.Infected || p.Antivirus.Digest != p.Digest || p.Antivirus.SizeBytes != p.SizeBytes {
		return commandOutcome{}, errs.ErrConflict
	}
	receiptRef, err := newRef("obj")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var revisionID string
	if err := tx.QueryRow(ctx, queryArtifactRevisionCreate, artifactID, payload.PreparedRevisionRef, item.Revision+1, item.FileName, p.MediaType, p.SizeBytes, p.Digest,
		item.Source, p.ScanState, receiptRef, p.PreviewState, current.actorID).Scan(&revisionID); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactRevisionCreateContent, revisionID, p.ObjectKey, p.ObjectVersion, p.ObjectETag, p.Digest, p.SizeBytes, scanJSON); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryArtifactRevisionAdvanceHead, artifactID, revisionID, version)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err := repository.adoptPreparedContentTx(ctx, tx, current, payload.PreparedLedgerID, revisionID); err != nil {
		return commandOutcome{}, err
	}
	updated, err := repository.artifactRevisionReadHead(ctx, tx, current, payload.ArtifactRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Artifact: &updated}, projectID: projectID, projectRef: updated.ProjectRef, resourceKind: "ARTIFACT", resourceRef: updated.Ref,
		summary: "i18n:ARTIFACT_REVISION_CREATED", platformEvent: "ARTIFACT_CHANGED"}, nil
}
